package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/cloudclient"
	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
	"github.com/spf13/cobra"
)

const (
	loginCallbackTimeout = 5 * time.Minute
)

var (
	// cloudTokenStoreFactory is the factory for creating a TokenStore. Tests
	// override this to inject a fake store.
	cloudTokenStoreFactory = func() (cloudclient.TokenStore, error) {
		return cloudclient.NewKeychainTokenStore()
	}

	// openBrowser is a hook to open a URL in the system browser. Tests
	// override this to prevent opening real browsers.
	openBrowser = func(url string) error {
		switch runtime.GOOS {
		case "darwin":
			return exec.Command("open", url).Start()
		case "linux":
			return exec.Command("xdg-open", url).Start()
		default:
			return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
		}
	}
)

// resolveToken returns the cloud API token. Order:
// 1. AGENTPAAS_CLOUD_API_TOKEN env var
// 2. Token store (Keychain)
func resolveToken(cmd *cobra.Command) (string, error) {
	if tok := os.Getenv("AGENTPAAS_CLOUD_API_TOKEN"); tok != "" {
		return tok, nil
	}

	store, err := cloudTokenStoreFactory()
	if err != nil {
		return "", fmt.Errorf("resolve token: %w", err)
	}

	tok, err := store.Get(cmd.Context())
	if err != nil {
		if cloudclient.IsTokenNotFoundErr(err) {
			return "", fmt.Errorf("not logged in")
		}
		return "", fmt.Errorf("resolve token: %w", err)
	}
	return tok, nil
}

// resolveAPIURL returns the cloud API base URL. Order:
// 1. AGENTPAAS_CLOUD_API_URL env var
// 2. Default
func resolveAPIURL() string {
	if u := os.Getenv("AGENTPAAS_CLOUD_API_URL"); u != "" {
		return u
	}
	return cloudclient.DefaultCloudAPIURL
}

// newCloudCmd creates the `agentpaas cloud` command.
func newCloudCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cloud",
		Short: "Manage AgentPaaS Cloud account",
		Long: `Log in, log out, and view your AgentPaaS Cloud account.

The cloud login command opens a browser for authentication.
Use 'agentpaas cloud whoami' to verify your session.`,
	}

	cmd.AddCommand(newCloudLoginCmd())
	cmd.AddCommand(newCloudWhoamiCmd())
	cmd.AddCommand(newCloudLogoutCmd())
	cmd.AddCommand(newCloudPushCmd())
	cmd.AddCommand(newCloudImagesCmd())
	cmd.AddCommand(newCloudDeployCmd())
	cmd.AddCommand(newCloudDeploymentsCmd())

	return cmd
}

// newCloudLoginCmd creates the `agentpaas cloud login` command.
func newCloudLoginCmd() *cobra.Command {
	var tokenStdin bool
	var tokenFlag string

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Log in to AgentPaaS Cloud",
		Long: `Authenticate with AgentPaaS Cloud.

By default, opens a browser for OAuth-style login. The token is stored
securely in your macOS Keychain.

CI environments can use --token-stdin to read a token from stdin, or set
the AGENTPAAS_CLOUD_API_TOKEN environment variable.`,
		Example: `  # Interactive browser login (default)
  agentpaas cloud login

  # CI login via environment variable
  export AGENTPAAS_CLOUD_API_TOKEN=apc_...

  # CI login via stdin
  echo "apc_..." | agentpaas cloud login --token-stdin`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := cloudTokenStoreFactory()
			if err != nil {
				return fmt.Errorf("cloud login: %w", err)
			}

			// --token-stdin path
			if tokenStdin {
				data, err := io.ReadAll(cmd.InOrStdin())
				if err != nil {
					return fmt.Errorf("cloud login: read stdin: %w", err)
				}
				tok := strings.TrimSpace(string(data))
				if tok == "" {
					return fmt.Errorf("cloud login: empty token from stdin")
				}
				return storeAndConfirmLogin(cmd, store, tok)
			}

			// --token flag path (CI only, hidden from help)
			if tokenFlag != "" {
				return storeAndConfirmLogin(cmd, store, tokenFlag)
			}

			// Browser login flow.
			apiURL := resolveAPIURL()
			client := cloudclient.NewCloudClient(apiURL)

			return runBrowserLoginFlow(cmd, store, client)
		},
	}

	cmd.Flags().BoolVar(&tokenStdin, "token-stdin", false, "Read API token from stdin (for CI)")
	cmd.Flags().StringVar(&tokenFlag, "token", "", "API token value (for CI)")
	// Mark --token as hidden from primary help.
	_ = cmd.Flags().MarkHidden("token")

	return cmd
}

// whoamiDisplay is the founder-facing whoami output, intentionally omitting
// SecretsBackend (founders should not see secrets infrastructure details).
type whoamiDisplay struct {
	TenantID         string `json:"tenant_id"`
	Tier             string `json:"tier"`
	ConcurrencyLimit int    `json:"concurrency_limit"`
}

// newCloudWhoamiCmd creates the `agentpaas cloud whoami` command.
func newCloudWhoamiCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Show authenticated cloud user info",
		Long: `Display the currently authenticated cloud user's tenant, tier,
and concurrency limit.

Requires a valid login. Use 'agentpaas cloud login' first.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			token, err := resolveToken(cmd)
			if err != nil {
				return printNotLoggedIn(cmd)
			}

			apiURL := resolveAPIURL()
			client := cloudclient.NewCloudClient(apiURL)

			resp, err := client.Whoami(cmd.Context(), token)
			if err != nil {
				// Check if it's a 401 — token may be expired.
				if strings.Contains(err.Error(), "not authenticated") {
					return printNotLoggedIn(cmd)
				}
				return fmt.Errorf("cloud whoami: %w", err)
			}

			display := whoamiDisplay{
				TenantID:         resp.TenantID,
				Tier:             resp.Tier,
				ConcurrencyLimit: resp.ConcurrencyLimit,
			}

			if jsonOutput(cmd) {
				return printTextOrJSON(true, display, nil)
			}
			fmt.Printf("Tenant: %s\nTier: %s\nConcurrency limit: %d\n",
				display.TenantID, display.Tier, display.ConcurrencyLimit)
			return nil
		},
	}
}

// newCloudLogoutCmd creates the `agentpaas cloud logout` command.
func newCloudLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Log out of AgentPaaS Cloud",
		Long: `Remove the stored cloud API token from the macOS Keychain.

Idempotent — succeeds even if not currently logged in.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := cloudTokenStoreFactory()
			if err != nil {
				return fmt.Errorf("cloud logout: %w", err)
			}

			if err := store.Delete(cmd.Context()); err != nil {
				return fmt.Errorf("cloud logout: %w", err)
			}

			fmt.Println("Logged out.")
			return nil
		},
	}
}

// runWranglerPush is the hook for pushing an image to Cloudflare Container Registry.
// Tests override this var. The default implementation calls wrangler.
var runWranglerPush = func(ctx context.Context, imageRef string) (registryRef string, err error) {
	// Try wrangler first, then npx wrangler.
	cmd := exec.CommandContext(ctx, "wrangler", "containers", "push", imageRef)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Try npx.
		cmd2 := exec.CommandContext(ctx, "npx", "wrangler", "containers", "push", imageRef)
		out2, err2 := cmd2.CombinedOutput()
		if err2 != nil {
			return "", fmt.Errorf("wrangler push failed: %w (output: %s)", err, string(out))
		}
		out = out2
	}
	// Parse the output for a registry.cloudflare.com line.
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "registry.cloudflare.com") {
			return line, nil
		}
	}
	return "", fmt.Errorf("wrangler push succeeded but no registry ref found in output")
}

// newCloudPushCmd creates the `agentpaas cloud push` command.
func newCloudPushCmd() *cobra.Command {
	var (
		lockPath     string
		digest       string
		platform     string
		registryRef  string
		skipRegistry bool
	)

	cmd := &cobra.Command{
		Use:   "push",
		Short: "Push a packed agent image to AgentPaaS Cloud",
		Long: `Admit a signed agent image to the AgentPaaS Cloud control plane.

This command reads the agent.lock file, verifies its signature, and
sends an admission request to the cloud API. The control plane verifies
the lockfile signature and admits the image for deployment.

Cloud requires amd64 packs. Customers never run wrangler themselves —
the CLI wraps it. Admit rejects unsigned locks.

If --skip-registry is set, only the admission request is sent (no
registry push). Otherwise, the CLI attempts to push the image to the
Cloudflare Container Registry via wrangler, using the
CLOUDFLARE_API_TOKEN environment variable.`,
		Example: `  # Push with admission only (no registry push)
  agentpaas cloud push --lock agent.lock --skip-registry

  # Push with registry ref from prior wrangler push
  agentpaas cloud push --lock agent.lock --registry-ref registry.cloudflare.com/...

  # Push with explicit digest override
  agentpaas cloud push --lock agent.lock --digest sha256:...`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// --lock is required. Check before network calls (fail closed).
			if lockPath == "" {
				return fmt.Errorf("cloud push: --lock is required (path to agent.lock)")
			}

			// Read and validate the lockfile before auth (fail closed).
			lock, err := pack.ReadAgentLock(lockPath)
			if err != nil {
				return fmt.Errorf("cloud push: read lock: %w", err)
			}

			// Require lockfile_signature non-empty.
			if lock.LockfileSignature == "" {
				return fmt.Errorf("cloud push: unsigned agent.lock — pack with signing identity")
			}

			// Verify the lockfile signature.
			if err := pack.VerifyLockfileSignature(lock); err != nil {
				return fmt.Errorf("cloud push: lockfile signature verification failed: %w", err)
			}

			// Require image_digest non-empty.
			if lock.ImageDigest == "" {
				return fmt.Errorf("cloud push: agent.lock has no image_digest")
			}

			// Resolve platform.
			resolvedPlatform := platform
			if resolvedPlatform == "" {
				if lock.Platform != "" {
					resolvedPlatform = lock.Platform
				} else {
					resolvedPlatform = "linux/amd64"
				}
			}

			// Require platform == linux/amd64 when targeting cloud.
			if resolvedPlatform != "linux/amd64" {
				return fmt.Errorf("cloud push: platform must be linux/amd64 for cloud, got %q", resolvedPlatform)
			}

			// Resolve token (must be logged in).
			token, err := resolveToken(cmd)
			if err != nil {
				if strings.Contains(err.Error(), "not logged in") {
					return printNotLoggedIn(cmd)
				}
				return fmt.Errorf("cloud push: %w", err)
			}

			// Resolve image digest.
			resolvedDigest := digest
			if resolvedDigest == "" {
				resolvedDigest = lock.ImageDigest
			}

			// Registry step: only if --registry-ref not provided AND --skip-registry is false.
			resolvedRegistryRef := registryRef
			if resolvedRegistryRef == "" && !skipRegistry {
				// Check if CLOUDFLARE_API_TOKEN is set.
				if os.Getenv("CLOUDFLARE_API_TOKEN") == "" {
					return fmt.Errorf("cloud push: CLOUDFLARE_API_TOKEN not set — use --skip-registry or set CLOUDFLARE_API_TOKEN")
				}
				ref, err := runWranglerPush(cmd.Context(), resolvedDigest)
				if err != nil {
					return fmt.Errorf("cloud push: wrangler push failed: %w (use --skip-registry to skip)", err)
				}
				resolvedRegistryRef = ref
			}

			// Build the admit request.
			apiURL := resolveAPIURL()
			client := cloudclient.NewCloudClient(apiURL)

			// Read lock JSON for agent_lock field.
			lockJSON, err := os.ReadFile(lockPath)
			if err != nil {
				return fmt.Errorf("cloud push: read lock file: %w", err)
			}
			var lockMap interface{}
			if err := json.Unmarshal(lockJSON, &lockMap); err != nil {
				return fmt.Errorf("cloud push: parse lock JSON: %w", err)
			}

			admReq := cloudclient.AdmitImageRequest{
				ImageDigest: resolvedDigest,
				Platform:    resolvedPlatform,
				RegistryRef: resolvedRegistryRef,
				AgentLock:   lockMap,
			}

			resp, err := client.AdmitImage(cmd.Context(), token, admReq)
			if err != nil {
				// Check if 401 — token may be expired.
				if strings.Contains(err.Error(), "not authenticated") {
					return printNotLoggedIn(cmd)
				}
				return fmt.Errorf("cloud push: %w", err)
			}

			// Print result.
			if jsonOutput(cmd) {
				return printTextOrJSON(true, resp, nil)
			}
			fmt.Printf("Image admitted: %s\n", resp.ID)
			fmt.Printf("  Digest: %s\n", resp.ImageDigest)
			fmt.Printf("  Status: %s\n", resp.Status)
			if resolvedRegistryRef != "" {
				fmt.Printf("  Registry: %s\n", resolvedRegistryRef)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&lockPath, "lock", "", "Path to agent.lock JSON (required)")
	cmd.Flags().StringVar(&digest, "digest", "", "Override image digest (default: from lock.image_digest)")
	cmd.Flags().StringVar(&platform, "platform", "", "Target platform (default: from lock.platform or linux/amd64)")
	cmd.Flags().StringVar(&registryRef, "registry-ref", "", "Cloudflare Container Registry reference (optional)")
	cmd.Flags().BoolVar(&skipRegistry, "skip-registry", false, "Skip registry push; admission only")

	return cmd
}

// newCloudImagesCmd creates the `agentpaas cloud images` command.
func newCloudImagesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "images",
		Short: "List admitted cloud images",
		Long: `List all images admitted to the AgentPaaS Cloud control plane.

Requires a valid login. Use 'agentpaas cloud login' first.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			token, err := resolveToken(cmd)
			if err != nil {
				if strings.Contains(err.Error(), "not logged in") {
					return printNotLoggedIn(cmd)
				}
				return fmt.Errorf("cloud images: %w", err)
			}

			apiURL := resolveAPIURL()
			client := cloudclient.NewCloudClient(apiURL)

			images, err := client.ListImages(cmd.Context(), token)
			if err != nil {
				if strings.Contains(err.Error(), "not authenticated") {
					return printNotLoggedIn(cmd)
				}
				return fmt.Errorf("cloud images: %w", err)
			}

			if jsonOutput(cmd) {
				return printTextOrJSON(true, images, nil)
			}

			if len(images) == 0 {
				fmt.Println("No images. Push an image with 'agentpaas cloud push'.")
				return nil
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%-20s %-70s %s\n", "ID", "DIGEST", "STATUS")
			for _, img := range images {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%-20s %-70s %s\n", img.ID, img.ImageDigest, img.Status)
			}
			return nil
		},
	}
}

// printNotLoggedIn prints the "not logged in" error message and returns an
// error so the command exits non-zero. For JSON output, prints the error to
// stdout and returns nil (matching the daemon status pattern).
func printNotLoggedIn(cmd *cobra.Command) error {
	errMsg := "not logged in"
	hint := "Run: agentpaas cloud login  (CI: export AGENTPAAS_CLOUD_API_TOKEN=...)"
	if jsonOutput(cmd) {
		return printTextOrJSON(true, JSONError{
			Error:   errMsg,
			Message: "No cloud token found",
			Hint:    hint,
		}, nil)
	}
	return fmt.Errorf("%s. %s", errMsg, hint)
}

// storeAndConfirmLogin stores the token and prints a confirmation.
func storeAndConfirmLogin(cmd *cobra.Command, store cloudclient.TokenStore, token string) error {
	if err := store.Set(cmd.Context(), token); err != nil {
		return fmt.Errorf("cloud login: store token: %w", err)
	}
	fmt.Println("Logged in. Run 'agentpaas cloud whoami' to verify.")
	return nil
}

// runBrowserLoginFlow implements the browser-based OAuth login flow.
func runBrowserLoginFlow(cmd *cobra.Command, store cloudclient.TokenStore, client *cloudclient.CloudClient) error {
	// 1. Start a callback HTTP server on 127.0.0.1:0.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("cloud login: start callback server: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", port)

	// 2. POST to /v1/auth/cli/start.
	authResp, err := client.StartCLIAuth(cmd.Context(), redirectURI)
	if err != nil {
		_ = listener.Close()
		return fmt.Errorf("cloud login: start auth: %w", err)
	}

	// 4. Set up the callback handler.
	type callbackResult struct {
		token string
		err   error
	}
	callbackCh := make(chan callbackResult, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		// Validate state.
		qState := r.URL.Query().Get("state")
		if qState == "" || qState != authResp.State {
			http.Error(w, "Invalid state parameter", http.StatusBadRequest)
			callbackCh <- callbackResult{err: fmt.Errorf("state mismatch: expected %q, got %q", authResp.State, qState)}
			return
		}

		token := r.URL.Query().Get("token")
		if token == "" {
			http.Error(w, "Missing token parameter", http.StatusBadRequest)
			callbackCh <- callbackResult{err: fmt.Errorf("callback missing token parameter")}
			return
		}

		// Success.
		_, _ = fmt.Fprint(w, "Login successful. You may close this window.")
		callbackCh <- callbackResult{token: token}
	})

	server := &http.Server{
		Handler: mux,
	}
	defer func() { _ = server.Close() }()

	// 5. Start the server in a goroutine.
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			if !isServerClosedErr(err) {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Callback server error: %v\n", err)
			}
		}
	}()

	// 6. Open the browser with the approve URL.
	approveURL := cloudclient.ResolveApproveURL(client.BaseURL, authResp.ApproveURL)
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Opening browser for login: %s\n", approveURL)
	if err := openBrowser(approveURL); err != nil {
		_ = server.Close()
		return fmt.Errorf("cloud login: open browser: %w", err)
	}

	// 7. Wait for the callback with a timeout.
	select {
	case result := <-callbackCh:
		if result.err != nil {
			return fmt.Errorf("cloud login: %w", result.err)
		}
		return storeAndConfirmLogin(cmd, store, result.token)
	case <-time.After(loginCallbackTimeout):
		_ = server.Close()
		return fmt.Errorf("cloud login: timed out waiting for browser authentication (waited %v)", loginCallbackTimeout)
	case <-cmd.Context().Done():
		_ = server.Close()
		return fmt.Errorf("cloud login: cancelled")
	}
}

func isServerClosedErr(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "use of closed network connection")
}

// sha256DigestRe validates sha256:<64 hex chars>.
var sha256DigestRe = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// newCloudDeployCmd creates the `agentpaas cloud deploy` command.
func newCloudDeployCmd() *cobra.Command {
	var slotID string

	cmd := &cobra.Command{
		Use:   "deploy <digest>",
		Short: "Deploy an admitted image to AgentPaaS Cloud",
		Long: `Create a deployment from an admitted image digest.

The digest must be in sha256:<hex> format. The image must have been
previously admitted via 'agentpaas cloud push'.

Use --slot-id to pin the deployment to a specific slot; otherwise the
control plane assigns one automatically.`,
		Example: `  # Deploy an admitted image
  agentpaas cloud deploy sha256:abcd1234...

  # Deploy to a specific slot
  agentpaas cloud deploy sha256:abcd1234... --slot-id slot-42`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			digest := args[0]

			// Validate digest format.
			if !sha256DigestRe.MatchString(digest) {
				return fmt.Errorf("cloud deploy: invalid digest format %q — must be sha256:<64 hex chars>", digest)
			}

			// Resolve token.
			token, err := resolveToken(cmd)
			if err != nil {
				if strings.Contains(err.Error(), "not logged in") {
					return printNotLoggedIn(cmd)
				}
				return fmt.Errorf("cloud deploy: %w", err)
			}

			// Build request.
			apiURL := resolveAPIURL()
			client := cloudclient.NewCloudClient(apiURL)

			req := cloudclient.CreateDeploymentRequest{
				ImageDigest: digest,
			}
			if slotID != "" {
				req.SlotID = &slotID
			}

			resp, err := client.CreateDeployment(cmd.Context(), token, req)
			if err != nil {
				if strings.Contains(err.Error(), "not authenticated") {
					return printNotLoggedIn(cmd)
				}
				return fmt.Errorf("cloud deploy: %w", err)
			}

			// Print result.
			if jsonOutput(cmd) {
				return printTextOrJSON(true, resp, nil)
			}
			fmt.Printf("Deployment created: %s\n", resp.ID)
			fmt.Printf("  Digest: %s\n", resp.ImageDigest)
			fmt.Printf("  Status: %s\n", resp.Status)
			if resp.SlotID != nil && *resp.SlotID != "" {
				fmt.Printf("  Slot:   %s\n", *resp.SlotID)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&slotID, "slot-id", "", "Pin deployment to a specific slot")

	return cmd
}

// newCloudDeploymentsCmd creates the `agentpaas cloud deployments` command.
func newCloudDeploymentsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "deployments",
		Aliases: []string{"list"},
		Short:   "List cloud deployments",
		Long: `List all deployments on the AgentPaaS Cloud control plane.

Requires a valid login. Use 'agentpaas cloud login' first.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			token, err := resolveToken(cmd)
			if err != nil {
				if strings.Contains(err.Error(), "not logged in") {
					return printNotLoggedIn(cmd)
				}
				return fmt.Errorf("cloud deployments: %w", err)
			}

			apiURL := resolveAPIURL()
			client := cloudclient.NewCloudClient(apiURL)

			deployments, err := client.ListDeployments(cmd.Context(), token)
			if err != nil {
				if strings.Contains(err.Error(), "not authenticated") {
					return printNotLoggedIn(cmd)
				}
				return fmt.Errorf("cloud deployments: %w", err)
			}

			if jsonOutput(cmd) {
				return printTextOrJSON(true, deployments, nil)
			}

			if len(deployments) == 0 {
				fmt.Println("No deployments. Deploy an image with 'agentpaas cloud deploy'.")
				return nil
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%-20s %-70s %-12s %s\n", "ID", "DIGEST", "STATUS", "SLOT")
			for _, dep := range deployments {
				slot := "-"
				if dep.SlotID != nil && *dep.SlotID != "" {
					slot = *dep.SlotID
				}
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%-20s %-70s %-12s %s\n", dep.ID, dep.ImageDigest, dep.Status, slot)
			}
			return nil
		},
	}

	return cmd
}
