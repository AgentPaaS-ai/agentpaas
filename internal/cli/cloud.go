package cli

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/cloudclient"
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
