package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/cloudclient"
	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
	"github.com/AgentPaaS-ai/agentpaas/internal/secrets"
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
	cmd.AddCommand(newCloudUsageCmd())
	cmd.AddCommand(newCloudInvokeTokenCmd())
	cmd.AddCommand(newCloudInvokeCmd())
	cmd.AddCommand(newCloudResultCmd())
	cmd.AddCommand(newCloudLogsCmd())
	cmd.AddCommand(newCloudLogoutCmd())
	cmd.AddCommand(newCloudPushCmd())
	cmd.AddCommand(newCloudImagesCmd())
	cmd.AddCommand(newCloudRegistryCmd())
	cmd.AddCommand(newCloudDeployCmd())
	cmd.AddCommand(newCloudDeploymentsCmd())
	cmd.AddCommand(newCloudUndeployCmd())
	cmd.AddCommand(newCloudSecretsCmd())
	cmd.AddCommand(newCloudRunCmd())
	cmd.AddCommand(newCloudStatusCmd())
	cmd.AddCommand(newCloudCancelCmd())

	// All cloud descendants return errors through one renderer so API reason
	// codes and semantic exit codes stay consistent across verbs.
	wrapCloudCommandErrors(cmd)

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

// newCloudUsageCmd creates the `agentpaas cloud usage` command.
func newCloudUsageCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "usage",
		Short: "Show cloud usage and plan limits",
		Long: `Display current AgentPaaS Cloud usage, plan limits, and the
metering formula.

Requires a valid login. Use 'agentpaas cloud login' first.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			token, err := resolveToken(cmd)
			if err != nil {
				if strings.Contains(err.Error(), "not logged in") {
					return printNotLoggedIn(cmd)
				}
				return fmt.Errorf("cloud usage: %w", err)
			}

			client := cloudclient.NewCloudClient(resolveAPIURL())
			resp, err := client.GetUsage(cmd.Context(), token)
			if err != nil {
				if strings.Contains(err.Error(), "not authenticated") {
					return printNotLoggedIn(cmd)
				}
				return fmt.Errorf("cloud usage: %w", err)
			}

			if jsonOutput(cmd) {
				return printTextOrJSON(true, resp, nil)
			}

			out := cmd.OutOrStdout()
			_, _ = fmt.Fprintf(out, "Tier: %s\n", resp.Tier)
			_, _ = fmt.Fprintf(out, "Concurrency active: %d/%d\n", resp.ConcurrencyActive, resp.ConcurrencyLimit)
			_, _ = fmt.Fprintf(out, "Agents used: %d/%d\n", resp.AgentsUsed, resp.AgentLimit)
			_, _ = fmt.Fprintf(out, "CPU minutes used: %g/%d\n", resp.CPUMinutesUsed, resp.CPUMinuteLimit)
			if resp.CPUMinutesRemaining == nil {
				_, _ = fmt.Fprintln(out, "CPU minutes remaining: unlimited")
			} else {
				_, _ = fmt.Fprintf(out, "CPU minutes remaining: %g\n", *resp.CPUMinutesRemaining)
			}
			if resp.DaysRemaining == nil {
				_, _ = fmt.Fprintln(out, "Trial days remaining: no trial expiry")
			} else {
				_, _ = fmt.Fprintf(out, "Trial days remaining: %d\n", *resp.DaysRemaining)
			}
			_, _ = fmt.Fprintf(out, "Meter formula: %s\n", resp.Meter.Formula)
			return nil
		},
	}
}

// newCloudInvokeTokenCmd creates the `agentpaas cloud invoke-token` command.
func newCloudInvokeTokenCmd() *cobra.Command {
	var storeOutput bool

	cmd := &cobra.Command{
		Use:   "invoke-token <deployment_id>",
		Short: "Mint a deployment invoke token",
		Long: `Mint a token that can invoke a deployment without exposing the
tenant API token to the caller.

The invoke token is displayed once. Store it securely and use it with
'agentpaas cloud invoke'. Requires a valid cloud login.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			tenantToken, err := resolveToken(cmd)
			if err != nil {
				if strings.Contains(err.Error(), "not logged in") {
					return printNotLoggedIn(cmd)
				}
				return fmt.Errorf("cloud invoke-token: %w", err)
			}

			client := cloudclient.NewCloudClient(resolveAPIURL())
			resp, err := client.MintInvokeToken(cmd.Context(), tenantToken, args[0])
			if err != nil {
				if strings.Contains(err.Error(), "not authenticated") {
					return printNotLoggedIn(cmd)
				}
				return fmt.Errorf("cloud invoke-token: %w", err)
			}

			invokeStore, err := newCloudInvokeTokenStore(cmd)
			if err != nil {
				return fmt.Errorf("cloud invoke-token: store token: %w", err)
			}
			if err := invokeStore.Set(cmd.Context(), args[0], resp.InvokeToken); err != nil {
				return fmt.Errorf("cloud invoke-token: store token: %w", err)
			}

			if storeOutput {
				stored := struct {
					DeploymentID      string `json:"deployment_id"`
					InvokeTokenPrefix string `json:"invoke_token_prefix"`
					Message           string `json:"message"`
				}{
					DeploymentID:      resp.DeploymentID,
					InvokeTokenPrefix: resp.InvokeTokenPrefix,
					Message:           "Invoke token stored securely.",
				}
				if jsonOutput(cmd) {
					return printTextOrJSON(true, stored, nil)
				}
				out := cmd.OutOrStdout()
				_, _ = fmt.Fprintf(out, "Invoke token stored for %s.\n", stored.DeploymentID)
				_, _ = fmt.Fprintf(out, "Prefix: %s\n", stored.InvokeTokenPrefix)
				return nil
			}

			if jsonOutput(cmd) {
				return printTextOrJSON(true, resp, nil)
			}

			out := cmd.OutOrStdout()
			_, _ = fmt.Fprintf(out, "Invoke token: %s\n", resp.InvokeToken)
			_, _ = fmt.Fprintf(out, "Prefix: %s\n", resp.InvokeTokenPrefix)
			_, _ = fmt.Fprintln(out, "Warning: store this token securely; it will only be shown once.")
			return nil
		},
	}
	cmd.Flags().BoolVar(&storeOutput, "store", false, "Store the token and print only its prefix")
	return cmd
}

// newCloudInvokeCmd creates the `agentpaas cloud invoke` command.
func newCloudInvokeCmd() *cobra.Command {
	var body string
	var bodyFile string
	var token string
	var wait bool
	var waitTimeout time.Duration

	cmd := &cobra.Command{
		Use:   "invoke <deployment_id>",
		Short: "Invoke a cloud deployment",
		Long: `Invoke a deployment with a deployment invoke token.

Use --token or AGENTPAAS_CLOUD_INVOKE_TOKEN for the invoke token. This
command never uses the tenant cloud login token for the invoke request.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			invokeToken, err := resolveCloudInvokeToken(cmd, token, args[0])
			if err != nil {
				return err
			}

			requestBody, err := readCloudInvokeBody(cmd, body, bodyFile)
			if err != nil {
				return err
			}

			client := cloudclient.NewCloudClient(resolveAPIURL())
			resp, err := client.InvokeDeployment(cmd.Context(), invokeToken, args[0], requestBody)
			if err != nil {
				if message, ok := cloudAlreadyRunningMessage(err); ok {
					return errors.New(message)
				}
				return fmt.Errorf("cloud invoke: %w", err)
			}

			if wait {
				if waitTimeout <= 0 {
					return fmt.Errorf("cloud invoke: --wait-timeout must be greater than zero")
				}
				finalResult, waitErr := waitForCloudInvoke(cmd, client, resp, waitTimeout)
				if waitErr != nil {
					return waitErr
				}
				if jsonOutput(cmd) {
					return printTextOrJSON(true, finalResult, nil)
				}
				return printCloudRunResult(cmd, finalResult)
			}

			if jsonOutput(cmd) {
				return printTextOrJSON(true, resp, nil)
			}

			out := cmd.OutOrStdout()
			_, _ = fmt.Fprintf(out, "Run ID: %s\n", resp.RunID)
			_, _ = fmt.Fprintf(out, "Status: %s\n", resp.Status)
			if resp.FinalOutput != nil && *resp.FinalOutput != "" {
				_, _ = fmt.Fprintln(out, "Final output:")
				_, _ = fmt.Fprintln(out, *resp.FinalOutput)
			}
			if resp.Error != nil {
				_, _ = fmt.Fprintf(out, "Error: %s\n", *resp.Error)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&body, "body", "{}", "JSON request body")
	cmd.Flags().StringVar(&bodyFile, "body-file", "", "Read JSON request body from a file, or - for stdin")
	cmd.Flags().StringVar(&token, "token", "", "Deployment invoke token")
	cmd.Flags().BoolVar(&wait, "wait", false, "Wait for a terminal run result")
	cmd.Flags().DurationVar(&waitTimeout, "wait-timeout", 10*time.Minute, "Maximum time to wait when --wait is set")
	return cmd
}

var cloudInvokePollInterval = time.Second

func waitForCloudInvoke(cmd *cobra.Command, client *cloudclient.CloudClient, invoked *cloudclient.InvokeDeploymentResult, timeout time.Duration) (*cloudclient.RunResult, error) {
	if invoked.RunID == "" {
		return nil, fmt.Errorf("cloud invoke: response has no run_id")
	}
	if cloudRunTerminal(invoked.Status) && invoked.FinalOutput != nil {
		return invokeResultAsRunResult(invoked), nil
	}

	tenantToken, err := resolveToken(cmd)
	if err != nil {
		return nil, fmt.Errorf("cloud invoke --wait: resolve cloud token: %w", err)
	}
	waitCtx, cancel := context.WithTimeout(cmd.Context(), timeout)
	defer cancel()

	for {
		run, err := client.GetRun(waitCtx, tenantToken, invoked.RunID)
		if err != nil {
			return nil, fmt.Errorf("cloud invoke --wait: poll status: %w", err)
		}
		if cloudRunTerminal(run.Status) {
			result, err := client.GetRunResult(waitCtx, tenantToken, invoked.RunID)
			if err != nil {
				return nil, fmt.Errorf("cloud invoke --wait: fetch result: %w", err)
			}
			return result, nil
		}

		timer := time.NewTimer(cloudInvokePollInterval)
		select {
		case <-waitCtx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil, fmt.Errorf("cloud invoke --wait: timed out after %s", timeout)
		case <-timer.C:
		}
	}
}

func cloudRunTerminal(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed", "succeeded", "failed", "stopped", "cancelled", "canceled", "error", "finished":
		return true
	default:
		return false
	}
}

func invokeResultAsRunResult(invoked *cloudclient.InvokeDeploymentResult) *cloudclient.RunResult {
	finalOutput := json.RawMessage("null")
	if invoked.FinalOutput != nil {
		value := strings.TrimSpace(*invoked.FinalOutput)
		if json.Valid([]byte(value)) {
			finalOutput = json.RawMessage(value)
		} else if encoded, err := json.Marshal(*invoked.FinalOutput); err == nil {
			finalOutput = json.RawMessage(encoded)
		}
	}
	return &cloudclient.RunResult{
		RunID:       invoked.RunID,
		Status:      invoked.Status,
		Error:       invoked.Error,
		FinalOutput: finalOutput,
		Artifacts:   []cloudclient.RunArtifact{},
	}
}

func cloudAlreadyRunningMessage(err error) (string, bool) {
	var statusErr *cloudclient.HTTPStatusError
	if !errors.As(err, &statusErr) || statusErr.StatusCode != http.StatusTooManyRequests {
		return "", false
	}
	if statusErr.ErrorCode != "already_running" && statusErr.Reason != "already_running" {
		return "", false
	}

	retryAfter := statusErr.RetryAfterSec
	if retryAfter <= 0 {
		retryAfter = 30
	}
	return fmt.Sprintf("agent already running; retry in %ds", retryAfter), true
}

func resolveCloudInvokeToken(cmd *cobra.Command, flagToken, deploymentID string) (string, error) {
	if flagToken != "" {
		return flagToken, nil
	}
	if envToken := os.Getenv("AGENTPAAS_CLOUD_INVOKE_TOKEN"); envToken != "" {
		return envToken, nil
	}

	store, err := newCloudInvokeTokenStore(cmd)
	if err != nil {
		return "", fmt.Errorf("cloud invoke: resolve stored invoke token: %w", err)
	}
	invokeToken, err := store.Get(cmd.Context(), deploymentID)
	if err != nil {
		if cloudclient.IsInvokeTokenNotFoundErr(err) {
			return "", fmt.Errorf("cloud invoke: missing invoke token for deployment %q; provide --token or set AGENTPAAS_CLOUD_INVOKE_TOKEN", deploymentID)
		}
		return "", fmt.Errorf("cloud invoke: resolve stored invoke token: %w", err)
	}
	return invokeToken, nil
}

func readCloudInvokeBody(cmd *cobra.Command, body, bodyFile string) (json.RawMessage, error) {
	if bodyFile != "" && cmd.Flags().Changed("body") {
		return nil, fmt.Errorf("cloud invoke: --body and --body-file are mutually exclusive")
	}

	var data []byte
	if bodyFile == "-" {
		var err error
		data, err = io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return nil, fmt.Errorf("cloud invoke: read body from stdin: %w", err)
		}
	} else if bodyFile != "" {
		var err error
		data, err = readCloudInvokeBodyFile(bodyFile)
		if err != nil {
			return nil, fmt.Errorf("cloud invoke: %w", err)
		}
	} else {
		data = []byte(body)
	}

	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		trimmed = "{}"
	}
	if !json.Valid([]byte(trimmed)) {
		return nil, fmt.Errorf("cloud invoke: invalid JSON body")
	}
	return json.RawMessage(trimmed), nil
}

func readCloudInvokeBodyFile(path string) ([]byte, error) {
	if strings.ContainsRune(path, '\x00') {
		return nil, fmt.Errorf("body file path contains a null byte")
	}
	for _, component := range strings.Split(filepath.ToSlash(path), "/") {
		if component == ".." {
			return nil, fmt.Errorf("body file path must not contain '..'")
		}
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve body file path: %w", err)
	}
	for current := absPath; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			return nil, fmt.Errorf("inspect body file path: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 && !isCloudInvokeSystemSymlink(current) {
			return nil, fmt.Errorf("body file path contains a symlink: %s", current)
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read body file: %w", err)
	}
	return data, nil
}

func isCloudInvokeSystemSymlink(path string) bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	return path == "/var" || path == "/tmp"
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

			if jsonOutput(cmd) {
				return printTextOrJSON(true, struct {
					Message string `json:"message"`
				}{Message: "Logged out."}, nil)
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Logged out.")
			return nil
		},
	}
}

const cloudDockerSaveTimeout = 10 * time.Second

type dockerSaveProcess struct {
	stdout io.ReadCloser
	cmd    *exec.Cmd
	cancel context.CancelFunc
}

func (p *dockerSaveProcess) Read(buf []byte) (int, error) {
	return p.stdout.Read(buf)
}

func (p *dockerSaveProcess) Close() error {
	closeErr := p.stdout.Close()
	waitErr := p.cmd.Wait()
	p.cancel()
	if closeErr != nil {
		return closeErr
	}
	return waitErr
}

// dockerSaveImage streams a local image as a Docker save tar. Tests override
// this hook so they can provide deterministic tar bytes without Docker.
var dockerSaveImage = func(ctx context.Context, imageRef string) (io.ReadCloser, error) {
	saveCtx, cancel := context.WithTimeout(ctx, cloudDockerSaveTimeout)
	cmd := exec.CommandContext(saveCtx, "docker", "save", imageRef)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("docker save %s: create stdout pipe: %w", imageRef, err)
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("docker save %s: start: %w", imageRef, err)
	}
	return &dockerSaveProcess{stdout: stdout, cmd: cmd, cancel: cancel}, nil
}

// dockerImageInspect returns the image ID (digest with sha256: prefix) for a
// local Docker image. Tests override this var.
var dockerImageInspect = func(ctx context.Context, ref string) (string, error) {
	dockerCtx, cancel := context.WithTimeout(ctx, cloudDockerSaveTimeout)
	defer cancel()
	cmd := exec.CommandContext(dockerCtx, "docker", "image", "inspect", ref, "--format", "{{.ID}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker image inspect %s: %w (output: %s)", ref, err, string(out))
	}
	id := strings.TrimSpace(string(out))
	if id == "" {
		return "", fmt.Errorf("docker image inspect %s: empty output", ref)
	}
	return id, nil
}

// dockerTag creates a new tag for a local Docker image. Tests override this var.
var dockerTag = func(ctx context.Context, src, dst string) error {
	dockerCtx, cancel := context.WithTimeout(ctx, cloudDockerSaveTimeout)
	defer cancel()
	cmd := exec.CommandContext(dockerCtx, "docker", "tag", src, dst)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker tag %s %s: %w (output: %s)", src, dst, err, string(out))
	}
	return nil
}

// resolveLocalImageRef determines the local Docker image reference to save.
// It prefers the agentpaas/<name>:<version> tag if it exists and its
// digest matches the lock. Otherwise falls back to the digest image, tagging it
// if necessary. An explicit --image override bypasses all resolution.
func resolveLocalImageRef(ctx context.Context, lock *pack.AgentLock, imageOverride string) (string, error) {
	// Explicit override takes precedence.
	if imageOverride != "" {
		return imageOverride, nil
	}

	// Normalize the lock digest to have sha256: prefix.
	lockDigest := normalizeSHA256Prefix(lock.ImageDigest)

	// Preferred ref: agentpaas/<name>:<version>
	preferredRef := fmt.Sprintf("agentpaas/%s:%s", lock.AgentName, lock.AgentVersion)

	// Check if the preferred tag exists and its digest matches.
	if localDigest, err := dockerImageInspect(ctx, preferredRef); err == nil {
		if normalizeSHA256Prefix(localDigest) == lockDigest {
			return preferredRef, nil
		}
	}

	// Fallback: check if the digest image exists locally.
	if _, err := dockerImageInspect(ctx, lockDigest); err != nil {
		return "", fmt.Errorf("local image for digest %s not found — pack --target linux/amd64 first", lockDigest)
	}

	// Tag the digest image with the preferred ref and return it.
	if err := dockerTag(ctx, lockDigest, preferredRef); err != nil {
		return "", fmt.Errorf("failed to tag image %s → %s: %w", lockDigest, preferredRef, err)
	}
	return preferredRef, nil
}

// normalizeSHA256Prefix ensures the digest has a sha256: prefix.
func normalizeSHA256Prefix(digest string) string {
	if strings.HasPrefix(digest, "sha256:") {
		return digest
	}
	return "sha256:" + digest
}

func uploadCloudChunkWithRetry(ctx context.Context, client *cloudclient.CloudClient, token, uploadID string, index int, chunk []byte) error {
	const maxAttempts = 3
	var err error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		err = client.UploadImageChunk(ctx, token, uploadID, index, chunk)
		if err == nil {
			return nil
		}
		if strings.Contains(err.Error(), "not authenticated") || !cloudclient.IsRetryableError(err) || attempt == maxAttempts {
			return err
		}
	}
	return err
}

func uploadCloudImage(ctx context.Context, client *cloudclient.CloudClient, token, imageRef string, startReq cloudclient.UploadImageStartRequest) (*cloudclient.AdmitImageResponse, error) {
	startResp, err := client.UploadImageStart(ctx, token, startReq)
	if err != nil {
		return nil, err
	}
	if startResp.UploadID == "" {
		return nil, fmt.Errorf("upload-start returned an empty upload_id")
	}
	if startResp.ChunkSizeBytes <= 0 {
		return nil, fmt.Errorf("upload-start returned an invalid chunk_size_bytes: %d", startResp.ChunkSizeBytes)
	}

	saved, err := dockerSaveImage(ctx, imageRef)
	if err != nil {
		return nil, err
	}
	if saved == nil {
		return nil, fmt.Errorf("docker save returned a nil stream")
	}
	closed := false
	defer func() {
		if !closed {
			_ = saved.Close()
		}
	}()

	reader := bufio.NewReaderSize(saved, startResp.ChunkSizeBytes)
	chunk := make([]byte, startResp.ChunkSizeBytes)
	for index := 1; ; index++ {
		n, readErr := io.ReadFull(reader, chunk)
		if readErr != nil && readErr != io.EOF && readErr != io.ErrUnexpectedEOF {
			return nil, fmt.Errorf("read docker save stream: %w", readErr)
		}
		if n > 0 {
			if err := uploadCloudChunkWithRetry(ctx, client, token, startResp.UploadID, index, chunk[:n]); err != nil {
				return nil, err
			}
		}
		if readErr != nil {
			break
		}
	}

	if err := saved.Close(); err != nil {
		closed = true
		return nil, fmt.Errorf("finish docker save: %w", err)
	}
	closed = true

	return client.UploadImageComplete(ctx, token, startResp.UploadID)
}

// newCloudPushCmd creates the `agentpaas cloud push` command.
func newCloudPushCmd() *cobra.Command {
	var (
		lockPath     string
		digest       string
		platform     string
		registryRef  string
		skipRegistry bool
		imageRef     string
	)

	cmd := &cobra.Command{
		Use:   "push",
		Short: "Push a packed agent image to AgentPaaS Cloud",
		Long: `Upload a locally built agent image to AgentPaaS Cloud and admit it for deployment. The image must have been packed with --target linux/amd64.

This command reads the agent.lock file, verifies its signature, and
streams a Docker save archive to the cloud API using your tenant token.
The control plane verifies the lockfile signature and admits the image for
deployment.

Admit rejects unsigned locks.

If --skip-registry is set, only the admission request is sent (no image
upload). Use --registry-ref to admit an image that is already available in
the cloud registry.`,
		Example: `  # Push and admit
  agentpaas cloud push --lock agent.lock

  # Push with admission only (no registry push)
  agentpaas cloud push --lock agent.lock --skip-registry

  # Admit an image already available in the cloud registry
  agentpaas cloud push --lock agent.lock --registry-ref registry.example.com/...

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

			apiURL := resolveAPIURL()
			client := cloudclient.NewCloudClient(apiURL)

			// Marshal the verified lock back into the signed agent_lock object
			// sent to the cloud API.
			lockJSON, err := json.Marshal(lock)
			if err != nil {
				return fmt.Errorf("cloud push: marshal lock JSON: %w", err)
			}
			var lockMap interface{}
			if err := json.Unmarshal(lockJSON, &lockMap); err != nil {
				return fmt.Errorf("cloud push: parse lock JSON: %w", err)
			}

			var resp *cloudclient.AdmitImageResponse
			if skipRegistry || registryRef != "" {
				admReq := cloudclient.AdmitImageRequest{
					ImageDigest: resolvedDigest,
					Platform:    resolvedPlatform,
					RegistryRef: registryRef,
					AgentLock:   lockMap,
				}
				resp, err = client.AdmitImage(cmd.Context(), token, admReq)
			} else {
				localRef, resolveErr := resolveLocalImageRef(cmd.Context(), lock, imageRef)
				if resolveErr != nil {
					return fmt.Errorf("cloud push: %w", resolveErr)
				}
				startReq := cloudclient.UploadImageStartRequest{
					ImageDigest: resolvedDigest,
					Platform:    resolvedPlatform,
					AgentLock:   lockMap,
				}
				resp, err = uploadCloudImage(cmd.Context(), client, token, localRef, startReq)
			}
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
			outputRegistryRef := registryRef
			if outputRegistryRef == "" {
				outputRegistryRef = resp.RegistryRef
			}
			if outputRegistryRef != "" {
				fmt.Printf("  Registry: %s\n", outputRegistryRef)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&lockPath, "lock", "", "Path to agent.lock JSON (required)")
	cmd.Flags().StringVar(&digest, "digest", "", "Override image digest (default: from lock.image_digest)")
	cmd.Flags().StringVar(&platform, "platform", "", "Target platform (default: from lock.platform or linux/amd64)")
	cmd.Flags().StringVar(&registryRef, "registry-ref", "", "Cloud registry reference (optional)")
	cmd.Flags().BoolVar(&skipRegistry, "skip-registry", false, "Skip image upload; admission only")
	cmd.Flags().StringVar(&imageRef, "image", "", "Override local image reference (optional)")

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

// newCloudRegistryCmd creates the `agentpaas cloud registry` command.
func newCloudRegistryCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "registry",
		Aliases: []string{"list"},
		Short:   "List tenant assets and the platform MCP catalog",
		Long: `List assets owned by the authenticated tenant together with the
platform-provided MCP server catalog.

Requires a valid login. Use 'agentpaas cloud login' first.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			token, err := resolveToken(cmd)
			if err != nil {
				if strings.Contains(err.Error(), "not logged in") {
					return printNotLoggedIn(cmd)
				}
				return fmt.Errorf("cloud registry: %w", err)
			}

			client := cloudclient.NewCloudClient(resolveAPIURL())
			registry, err := client.GetRegistry(cmd.Context(), token)
			if err != nil {
				if strings.Contains(err.Error(), "not authenticated") {
					return printNotLoggedIn(cmd)
				}
				return fmt.Errorf("cloud registry: %w", err)
			}

			if jsonOutput(cmd) {
				return printTextOrJSON(true, registry, nil)
			}

			out := cmd.OutOrStdout()
			_, _ = fmt.Fprintf(out, "Tenant assets (%d):\n", len(registry.TenantAssets))
			if len(registry.TenantAssets) == 0 {
				_, _ = fmt.Fprintln(out, "  (none)")
			}
			for _, asset := range registry.TenantAssets {
				_, _ = fmt.Fprintf(out, "  %s", asset.Name)
				if asset.Kind != "" {
					_, _ = fmt.Fprintf(out, " [%s]", asset.Kind)
				}
				if asset.Version != "" {
					_, _ = fmt.Fprintf(out, " @%s", asset.Version)
				}
				if asset.Status != "" {
					_, _ = fmt.Fprintf(out, " (%s)", asset.Status)
				}
				_, _ = fmt.Fprintln(out)
			}

			_, _ = fmt.Fprintf(out, "Platform MCP catalog (%d):\n", len(registry.Platform.MCPCatalog))
			if len(registry.Platform.MCPCatalog) == 0 {
				_, _ = fmt.Fprintln(out, "  (none)")
			}
			for _, entry := range registry.Platform.MCPCatalog {
				_, _ = fmt.Fprintf(out, "  %s", entry.Name)
				if entry.Version != "" {
					_, _ = fmt.Fprintf(out, " @%s", entry.Version)
				}
				if entry.Description != "" {
					_, _ = fmt.Fprintf(out, " — %s", entry.Description)
				}
				_, _ = fmt.Fprintln(out)
			}
			return nil
		},
	}
}

// newCloudSecretsCmd creates the `agentpaas cloud secrets` command.
func newCloudSecretsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "secrets",
		Aliases: []string{"secret"},
		Short:   "Push and list cloud secrets (labels only, never values)",
		Long: `Sync local keychain secrets to the AgentPaaS Cloud.

Secrets are pushed by name only; values are transmitted over TLS but
never displayed by the CLI. Requires a valid cloud login.`,
		Example: `  agentpaas cloud secrets push my-api-key
  agentpaas cloud secrets list`,
	}

	cmd.AddCommand(newCloudSecretsPushCmd())
	cmd.AddCommand(newCloudSecretsListCmd())
	cmd.AddCommand(newCloudSecretsBindCmd())
	cmd.AddCommand(newCloudSecretsBindingsCmd())

	return cmd
}

// newCloudSecretsPushCmd creates the `agentpaas cloud secrets push` command.
func newCloudSecretsPushCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "push <name> [name...]",
		Short: "Push local keychain secrets to the cloud (labels only, never prints value)",
		Long: `Push one or more local keychain secrets to AgentPaaS Cloud.

Each named secret is read from the local macOS Keychain and sent to the
cloud API over TLS. Names are printed on success, but values are never
displayed. Requires a valid cloud login.`,
		Example: `  agentpaas cloud secrets push openai-key
  agentpaas cloud secrets push openai-key anthropic-key`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Resolve token (must be logged in).
			token, err := resolveToken(cmd)
			if err != nil {
				if strings.Contains(err.Error(), "not logged in") {
					return printNotLoggedIn(cmd)
				}
				return fmt.Errorf("cloud secrets push: %w", err)
			}

			apiURL := resolveAPIURL()
			client := cloudclient.NewCloudClient(apiURL)

			// Get the local secret store.
			store, err := secretStoreFactory(cmd)
			if err != nil {
				return fmt.Errorf("cloud secrets push: %w", err)
			}

			pushed := make([]string, 0, len(args))
			for _, name := range args {
				if err := secrets.ValidateSecretName(name); err != nil {
					return fmt.Errorf("cloud secrets push: %w", err)
				}
				value, err := store.Get(cmd.Context(), name)
				if err != nil {
					return fmt.Errorf("cloud secrets push: get local secret %q: %w", name, err)
				}
				if err := client.PutSecret(cmd.Context(), token, name, string(value)); err != nil {
					return fmt.Errorf("cloud secrets push: push %q: %w", name, err)
				}
				pushed = append(pushed, name)
			}

			if jsonOutput(cmd) {
				return printTextOrJSON(true, struct {
					Pushed []string `json:"pushed"`
				}{Pushed: pushed}, nil)
			}
			for _, name := range pushed {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "pushed: %s\n", name)
			}
			return nil
		},
	}
}

// newCloudSecretsListCmd creates the `agentpaas cloud secrets list` command.
func newCloudSecretsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List cloud secret labels (names only, never values)",
		Long: `List secret labels stored in AgentPaaS Cloud.

Returns names and timestamps only. Secret values are never returned
or displayed. Requires a valid cloud login.`,
		Example: `  agentpaas cloud secrets list
  agentpaas cloud secrets list --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			token, err := resolveToken(cmd)
			if err != nil {
				if strings.Contains(err.Error(), "not logged in") {
					return printNotLoggedIn(cmd)
				}
				return fmt.Errorf("cloud secrets list: %w", err)
			}

			apiURL := resolveAPIURL()
			client := cloudclient.NewCloudClient(apiURL)

			labels, err := client.ListSecrets(cmd.Context(), token)
			if err != nil {
				if strings.Contains(err.Error(), "not authenticated") {
					return printNotLoggedIn(cmd)
				}
				return fmt.Errorf("cloud secrets list: %w", err)
			}

			if jsonOutput(cmd) {
				return printTextOrJSON(true, labels, nil)
			}

			if len(labels) == 0 {
				fmt.Println("No cloud secrets. Push one with 'agentpaas cloud secrets push'.")
				return nil
			}

			fmt.Printf("Cloud secrets (%d):\n", len(labels))
			for _, s := range labels {
				fmt.Println("  " + s.Name)
			}
			return nil
		},
	}
}

// newCloudSecretsBindCmd binds a vault secret onto a deployment for inject at invoke.
func newCloudSecretsBindCmd() *cobra.Command {
	var injectAs, headerName, hostPattern string
	cmd := &cobra.Command{
		Use:   "bind <deployment_id> <secret_name>",
		Short: "Bind a cloud secret to a deployment (inject at invoke)",
		Long: `Attach a previously pushed cloud secret to a deployment.

LLM agents need this after deploy — tenant vault push alone is not enough.
Example for weather + OpenRouter:

  agentpaas cloud secrets bind dep_… openrouter-key --as bearer --host openrouter.ai

Bindings replace the full set for the named secret when used with --replace-all
false (default merges by rewriting the whole list as: existing minus same name,
plus this binding). Use --only to set exactly one binding.`,
		Example: `  agentpaas cloud secrets bind dep_abc openrouter-key --as bearer --host openrouter.ai`,
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			deploymentID := args[0]
			secretName := args[1]
			if err := secrets.ValidateSecretName(secretName); err != nil {
				return fmt.Errorf("cloud secrets bind: %w", err)
			}
			token, err := resolveToken(cmd)
			if err != nil {
				if strings.Contains(err.Error(), "not logged in") {
					return printNotLoggedIn(cmd)
				}
				return fmt.Errorf("cloud secrets bind: %w", err)
			}
			only, _ := cmd.Flags().GetBool("only")
			client := cloudclient.NewCloudClient(resolveAPIURL())

			b := cloudclient.DeploymentSecretBinding{
				SecretName: secretName,
				InjectAs:   injectAs,
			}
			if headerName != "" {
				h := headerName
				b.HeaderName = &h
			}
			if hostPattern != "" {
				h := hostPattern
				b.HostPattern = &h
			}

			var bindings []cloudclient.DeploymentSecretBinding
			if only {
				bindings = []cloudclient.DeploymentSecretBinding{b}
			} else {
				existing, err := client.ListDeploymentSecrets(cmd.Context(), token, deploymentID)
				if err != nil && !strings.Contains(err.Error(), "not_found") {
					// If list fails empty is OK for first bind — try with just this binding.
					if !strings.Contains(err.Error(), "not authenticated") {
						existing = nil
					} else {
						return printNotLoggedIn(cmd)
					}
				}
				for _, e := range existing {
					if e.SecretName == secretName {
						continue
					}
					bindings = append(bindings, e)
				}
				bindings = append(bindings, b)
			}

			if err := client.SetDeploymentSecrets(cmd.Context(), token, deploymentID, bindings); err != nil {
				if strings.Contains(err.Error(), "not authenticated") {
					return printNotLoggedIn(cmd)
				}
				return fmt.Errorf("cloud secrets bind: %w", err)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "bound: %s → %s (inject=%s host=%s)\n",
				secretName, deploymentID, injectAs, hostPattern)
			return nil
		},
	}
	cmd.Flags().StringVar(&injectAs, "as", "bearer", "Injection mode: bearer, header, or none")
	cmd.Flags().StringVar(&headerName, "header", "", "Header name when --as=header")
	cmd.Flags().StringVar(&hostPattern, "host", "", "Host pattern for credential scope (e.g. openrouter.ai)")
	cmd.Flags().Bool("only", false, "Replace all bindings with only this one")
	return cmd
}

// newCloudSecretsBindingsCmd lists deployment secret bindings (metadata only).
func newCloudSecretsBindingsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "bindings <deployment_id>",
		Short: "List secret bindings on a deployment (names only)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			token, err := resolveToken(cmd)
			if err != nil {
				if strings.Contains(err.Error(), "not logged in") {
					return printNotLoggedIn(cmd)
				}
				return fmt.Errorf("cloud secrets bindings: %w", err)
			}
			client := cloudclient.NewCloudClient(resolveAPIURL())
			list, err := client.ListDeploymentSecrets(cmd.Context(), token, args[0])
			if err != nil {
				if strings.Contains(err.Error(), "not authenticated") {
					return printNotLoggedIn(cmd)
				}
				return fmt.Errorf("cloud secrets bindings: %w", err)
			}
			if jsonOutput(cmd) {
				return printTextOrJSON(true, list, nil)
			}
			if len(list) == 0 {
				fmt.Println("No bindings. Bind with: agentpaas cloud secrets bind <dep> <secret> --as bearer --host <host>")
				return nil
			}
			fmt.Printf("Bindings on %s (%d):\n", args[0], len(list))
			for _, b := range list {
				host := ""
				if b.HostPattern != nil {
					host = *b.HostPattern
				}
				fmt.Printf("  %s  inject=%s  host=%s\n", b.SecretName, b.InjectAs, host)
			}
			return nil
		},
	}
}

// printNotLoggedIn returns a typed cloud error. The cloud command wrapper
// renders it as JSON when requested and maps it to the auth exit code.
func printNotLoggedIn(cmd *cobra.Command) error {
	return fmt.Errorf("not logged in. Run: agentpaas cloud login  (CI: export AGENTPAAS_CLOUD_API_TOKEN=...)")
}

// storeAndConfirmLogin stores the token and prints a confirmation.
func storeAndConfirmLogin(cmd *cobra.Command, store cloudclient.TokenStore, token string) error {
	if err := store.Set(cmd.Context(), token); err != nil {
		return fmt.Errorf("cloud login: store token: %w", err)
	}
	if jsonOutput(cmd) {
		return printTextOrJSON(true, struct {
			Message string `json:"message"`
		}{Message: "Logged in."}, nil)
	}
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Logged in. Run 'agentpaas cloud whoami' to verify.")
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
	var lockPath string
	var instanceType string
	var deployType string

	cmd := &cobra.Command{
		Use:   "deploy [digest|latest]",
		Short: "Deploy an admitted image to AgentPaaS Cloud",
		Long: `Create a deployment from an admitted image digest.

The digest must be in sha256:<hex> format, or pass "latest" to deploy the
most recently admitted image. With --lock, the digest is read from the
signed agent.lock (same path pack prints).

The image must have been previously admitted via 'agentpaas cloud push'.

Use --slot-id to pin the deployment to a specific slot; otherwise the
control plane assigns one automatically.

Use --instance-type to select a Cloudflare Container preset, from smallest to
largest: lite, basic (default; 1/4 vCPU, 1GiB), standard-1, standard-2,
standard-3, standard-4.

Use --type to deploy an agent or an MCP server. The default is agent.`,
		Example: `  # Deploy an admitted image
  agentpaas cloud deploy sha256:abcd1234...

  # Deploy most recently admitted image with the default basic preset
  agentpaas cloud deploy latest

  # Deploy latest using the standard-2 preset
  agentpaas cloud deploy latest --instance-type standard-2

  # Deploy digest from a pack lock
  agentpaas cloud deploy --lock "$HOME/.agentpaas/state/agents/weather-agent/agent.lock"

  # Deploy to a specific slot
  agentpaas cloud deploy sha256:abcd1234... --slot-id slot-42`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if deployType != "agent" && deployType != "mcp" {
				return fmt.Errorf("cloud deploy: --type must be agent or mcp")
			}
			switch instanceType {
			case "dev":
				return fmt.Errorf("instance_type 'dev' is an alias for 'lite' (256MiB) which is too small for LLM agents — use 'basic' or higher")
			case "lite", "basic", "standard-1", "standard-2", "standard-3", "standard-4":
			default:
				return fmt.Errorf("instance_type must be one of: lite, basic, standard-1, standard-2, standard-3, standard-4")
			}

			// Resolve token.
			token, err := resolveToken(cmd)
			if err != nil {
				if strings.Contains(err.Error(), "not logged in") {
					return printNotLoggedIn(cmd)
				}
				return fmt.Errorf("cloud deploy: %w", err)
			}

			apiURL := resolveAPIURL()
			client := cloudclient.NewCloudClient(apiURL)

			digest := ""
			if len(args) > 0 {
				digest = args[0]
			}

			// Prefer --lock when set.
			if lockPath != "" {
				if !filepath.IsAbs(lockPath) {
					return fmt.Errorf("cloud deploy: --lock path must be absolute: %s", lockPath)
				}
				lock, err := pack.ReadAgentLock(lockPath)
				if err != nil {
					return fmt.Errorf("cloud deploy: read lock: %w", err)
				}
				if lock.ImageDigest == "" {
					return fmt.Errorf("cloud deploy: agent.lock has no image_digest")
				}
				digest = lock.ImageDigest
			}

			if digest == "" {
				return fmt.Errorf("cloud deploy: provide a digest, 'latest', or --lock <absolute agent.lock>")
			}

			if digest == "latest" {
				images, err := client.ListImages(cmd.Context(), token)
				if err != nil {
					if strings.Contains(err.Error(), "not authenticated") {
						return printNotLoggedIn(cmd)
					}
					return fmt.Errorf("cloud deploy: list images for latest: %w", err)
				}
				if len(images) == 0 {
					return fmt.Errorf("cloud deploy: no admitted images — run 'agentpaas cloud push' first")
				}
				// Prefer the most recently admitted; API order is newest-first when available.
				digest = images[0].ImageDigest
				if digest == "" {
					return fmt.Errorf("cloud deploy: latest image has empty digest")
				}
				if !jsonOutput(cmd) {
					fmt.Printf("Using latest admitted image: %s\n", digest)
				}
			}

			// Validate digest format.
			if !sha256DigestRe.MatchString(digest) {
				return fmt.Errorf("cloud deploy: invalid digest format %q — must be sha256:<64 hex chars> or 'latest'", digest)
			}

			// Build request.
			req := cloudclient.CreateDeploymentRequest{
				ImageDigest:  digest,
				Kind:         deployType,
				InstanceType: &instanceType,
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
	cmd.Flags().StringVar(&lockPath, "lock", "", "Absolute path to agent.lock (uses its image_digest)")
	cmd.Flags().StringVar(&instanceType, "instance-type", "basic", "Cloudflare Container preset (default: basic; lite, basic, standard-1, standard-2, standard-3, standard-4)")
	cmd.Flags().StringVar(&deployType, "type", "agent", "Deployment type: agent|mcp (default: agent)")

	return cmd
}

// newCloudDeploymentsCmd creates the `agentpaas cloud deployments` command.
func newCloudDeploymentsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deployments",
		Short: "List cloud deployments",
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

// newCloudUndeployCmd creates the `agentpaas cloud undeploy` command.
func newCloudUndeployCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "undeploy <dep_id>",
		Short: "Undeploy a cloud deployment and free its slot",
		Long: `Undeploy a cloud deployment by ID.

This calls DELETE /v1/deployments/:id and frees the deployment's slot for
reuse.

Requires a valid login. Use 'agentpaas cloud login' first.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			depID := args[0]
			if strings.ContainsAny(depID, "/\\\n\r") {
				return fmt.Errorf("cloud undeploy: invalid deployment id %q: must not contain '/', '\\', newline, or carriage return", depID)
			}

			token, err := resolveToken(cmd)
			if err != nil {
				if strings.Contains(err.Error(), "not logged in") {
					return printNotLoggedIn(cmd)
				}
				return fmt.Errorf("cloud undeploy: %w", err)
			}

			apiURL := resolveAPIURL()
			client := cloudclient.NewCloudClient(apiURL)

			result, err := client.DeleteDeployment(cmd.Context(), token, depID)
			if err != nil {
				if strings.Contains(err.Error(), "not authenticated") {
					return printNotLoggedIn(cmd)
				}
				return fmt.Errorf("cloud undeploy: %w", err)
			}

			if jsonOutput(cmd) {
				return printTextOrJSON(true, result, nil)
			}
			fmt.Printf("Undeployed: %s (slot freed)\n", result.ID)
			fmt.Printf("  Status: %s\n", result.Status)
			return nil
		},
	}
}

// newCloudRunCmd creates the `agentpaas cloud run` command.
func newCloudRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run <deployment_id>",
		Short: "Create a cloud run from a deployment",
		Long: `Create a new run for a deployment on the AgentPaaS Cloud control plane.

Note: cloud run may show status=failed when no container runtime is
available (local development setup). Real Cloudflare Workers runs
will be available in a future release.

Requires a valid login. Use 'agentpaas cloud login' first.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			deploymentID := args[0]

			token, err := resolveToken(cmd)
			if err != nil {
				if strings.Contains(err.Error(), "not logged in") {
					return printNotLoggedIn(cmd)
				}
				return fmt.Errorf("cloud run: %w", err)
			}

			apiURL := resolveAPIURL()
			client := cloudclient.NewCloudClient(apiURL)

			req := cloudclient.CreateRunRequest{
				DeploymentID: deploymentID,
			}

			resp, err := client.CreateRun(cmd.Context(), token, req)
			if err != nil {
				if message, ok := cloudAlreadyRunningMessage(err); ok {
					return errors.New(message)
				}
				if strings.Contains(err.Error(), "not authenticated") {
					return printNotLoggedIn(cmd)
				}
				return fmt.Errorf("cloud run: %w", err)
			}

			if jsonOutput(cmd) {
				return printTextOrJSON(true, resp, nil)
			}
			fmt.Printf("Run created: %s\n", resp.ID)
			fmt.Printf("  Deployment: %s\n", resp.DeploymentID)
			fmt.Printf("  Status:     %s\n", resp.Status)
			if resp.Error != nil && *resp.Error != "" {
				fmt.Printf("  Error:      %s\n", *resp.Error)
			}
			return nil
		},
	}
}

// newCloudStatusCmd creates the `agentpaas cloud status` command.
func newCloudStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status [run_id]",
		Short: "Show cloud run status",
		Long: `Show status of cloud runs.

Without arguments, lists all runs. With a run_id, shows details for
that specific run.

Requires a valid login. Use 'agentpaas cloud login' first.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			token, err := resolveToken(cmd)
			if err != nil {
				if strings.Contains(err.Error(), "not logged in") {
					return printNotLoggedIn(cmd)
				}
				return fmt.Errorf("cloud status: %w", err)
			}

			apiURL := resolveAPIURL()
			client := cloudclient.NewCloudClient(apiURL)

			// If a run_id is provided, get that specific run.
			if len(args) == 1 {
				runID := args[0]

				resp, err := client.GetRun(cmd.Context(), token, runID)
				if err != nil {
					if strings.Contains(err.Error(), "not authenticated") {
						return printNotLoggedIn(cmd)
					}
					return fmt.Errorf("cloud status: %w", err)
				}

				if jsonOutput(cmd) {
					return printTextOrJSON(true, resp, nil)
				}
				fmt.Printf("Run:        %s\n", resp.ID)
				fmt.Printf("  Deployment: %s\n", resp.DeploymentID)
				fmt.Printf("  Status:     %s\n", resp.Status)
				if resp.CreatedAt != "" {
					fmt.Printf("  Created:    %s\n", resp.CreatedAt)
				}
				if resp.StartedAt != nil && *resp.StartedAt != "" {
					fmt.Printf("  Started:    %s\n", *resp.StartedAt)
				}
				if resp.FinishedAt != nil && *resp.FinishedAt != "" {
					fmt.Printf("  Finished:   %s\n", *resp.FinishedAt)
				}
				if resp.Error != nil && *resp.Error != "" {
					fmt.Printf("  Error:      %s\n", *resp.Error)
				}
				if resp.SlotID != nil && *resp.SlotID != "" {
					fmt.Printf("  Slot:       %s\n", *resp.SlotID)
				}
				if resp.ContainerID != nil && *resp.ContainerID != "" {
					fmt.Printf("  Container:  %s\n", *resp.ContainerID)
				}
				return nil
			}

			// No run_id: list all runs.
			runs, err := client.ListRuns(cmd.Context(), token)
			if err != nil {
				if strings.Contains(err.Error(), "not authenticated") {
					return printNotLoggedIn(cmd)
				}
				return fmt.Errorf("cloud status: %w", err)
			}

			if jsonOutput(cmd) {
				return printTextOrJSON(true, runs, nil)
			}

			if len(runs) == 0 {
				fmt.Println("No runs. Create one with 'agentpaas cloud run <deployment_id>'.")
				return nil
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%-20s %-20s %-12s %s\n", "ID", "DEPLOYMENT", "STATUS", "ERROR")
			for _, r := range runs {
				errStr := ""
				if r.Error != nil {
					errStr = *r.Error
				}
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%-20s %-20s %-12s %s\n", r.ID, r.DeploymentID, r.Status, errStr)
			}
			return nil
		},
	}
}

// newCloudCancelCmd creates the `agentpaas cloud cancel` command.
func newCloudCancelCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "cancel <run_id>",
		Short: "Cancel a cloud run",
		Long: `Cancel a running or pending cloud run.

Requires a valid login. Use 'agentpaas cloud login' first.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			runID := args[0]

			token, err := resolveToken(cmd)
			if err != nil {
				if strings.Contains(err.Error(), "not logged in") {
					return printNotLoggedIn(cmd)
				}
				return fmt.Errorf("cloud cancel: %w", err)
			}

			apiURL := resolveAPIURL()
			client := cloudclient.NewCloudClient(apiURL)

			resp, err := client.CancelRun(cmd.Context(), token, runID)
			if err != nil {
				if strings.Contains(err.Error(), "not authenticated") {
					return printNotLoggedIn(cmd)
				}
				return fmt.Errorf("cloud cancel: %w", err)
			}

			if jsonOutput(cmd) {
				return printTextOrJSON(true, resp, nil)
			}
			fmt.Printf("Run cancelled: %s\n", resp.ID)
			fmt.Printf("  Deployment: %s\n", resp.DeploymentID)
			fmt.Printf("  Status:     %s\n", resp.Status)
			if resp.Error != nil && *resp.Error != "" {
				fmt.Printf("  Error:      %s\n", *resp.Error)
			}
			return nil
		},
	}
}
