package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/daemon"
	"github.com/spf13/cobra"
)

const (
	dockerCLITimeout    = 5 * time.Second
	dockerDaemonTimeout = 5 * time.Second
	keychainTimeout     = 3 * time.Second
)

// doctorExitFn is overridden in tests to avoid os.Exit.
var doctorExitFn = func(code int) { os.Exit(code) }

// DoctorCheck is a single diagnostic check result.
type DoctorCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

// DoctorOutput holds the aggregated doctor diagnostics output.
type DoctorOutput struct {
	Checks        []DoctorCheck `json:"checks"`
	OverallStatus string        `json:"overall_status"`
}

// newDoctorCmd creates the `agent doctor` command.
func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Run first-run environment diagnostics",
		Long: `Run system diagnostics to verify agentpaas is configured correctly.

Checks CLI version, Docker, macOS Keychain, the Linux harness binary, and
the agentpaas home directory. Does not require the daemon to be running.

Use --json for structured JSON output.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			homeDir, err := homeDirPath(cmd)
			if err != nil {
				return err
			}

			output := runDoctorChecks(homeDir)
			if err := printTextOrJSON(jsonOutput(cmd), output, func(v interface{}) string {
				return DoctorText(v.(DoctorOutput))
			}); err != nil {
				return err
			}

			if output.hasFailure() {
				doctorExitFn(1)
			}
			return nil
		},
	}
}

func runDoctorChecks(homeDir string) DoctorOutput {
	checks := []DoctorCheck{
		checkVersion(),
		checkDockerCLI(),
		checkDockerDaemon(),
		checkKeychain(),
		checkHarnessLinux(),
		checkHomeDir(homeDir),
	}
	return DoctorOutput{
		Checks:        checks,
		OverallStatus: overallStatus(checks),
	}
}

func checkVersion() DoctorCheck {
	version := daemon.CurrentVersion().CLIVersion
	if version == "" {
		version = "0.0.0-dev"
	}
	return DoctorCheck{Name: "version", Status: "ok", Message: version}
}

func checkDockerCLI() DoctorCheck {
	if _, err := exec.LookPath("docker"); err != nil {
		return DoctorCheck{Name: "docker_cli", Status: "fail", Message: "not found"}
	}

	version, err := runDockerFormat(dockerCLITimeout, "version", "--format", "{{.Server.Version}}")
	if err == nil && strings.TrimSpace(version) != "" {
		return DoctorCheck{Name: "docker_cli", Status: "ok", Message: strings.TrimSpace(version)}
	}

	// CLI is on PATH; confirm the client responds even if the server is down.
	clientVersion, clientErr := runDockerFormat(dockerCLITimeout, "version", "--format", "{{.Client.Version}}")
	if clientErr == nil && strings.TrimSpace(clientVersion) != "" {
		return DoctorCheck{Name: "docker_cli", Status: "ok", Message: strings.TrimSpace(clientVersion)}
	}

	return DoctorCheck{Name: "docker_cli", Status: "fail", Message: "not working"}
}

func checkDockerDaemon() DoctorCheck {
	if _, err := exec.LookPath("docker"); err != nil {
		return DoctorCheck{Name: "docker_daemon", Status: "warn", Message: "not running"}
	}

	version, err := runDockerFormat(dockerDaemonTimeout, "info", "--format", "{{.ServerVersion}}")
	if err != nil || strings.TrimSpace(version) == "" {
		return DoctorCheck{Name: "docker_daemon", Status: "warn", Message: "not running"}
	}
	return DoctorCheck{Name: "docker_daemon", Status: "ok", Message: strings.TrimSpace(version)}
}

func checkKeychain() DoctorCheck {
	if runtime.GOOS != "darwin" {
		return DoctorCheck{Name: "keychain", Status: "ok", Message: "n/a (not macOS)"}
	}

	ctx, cancel := context.WithTimeout(context.Background(), keychainTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, "security", "default-keychain").Output()
	if err != nil {
		msg := "not accessible"
		if ctx.Err() == context.DeadlineExceeded {
			msg = "timed out"
		}
		return DoctorCheck{Name: "keychain", Status: "fail", Message: msg}
	}

	keychain := strings.Trim(strings.TrimSpace(string(out)), `"`)
	if keychain == "" {
		return DoctorCheck{Name: "keychain", Status: "fail", Message: "not accessible"}
	}
	return DoctorCheck{Name: "keychain", Status: "ok", Message: filepath.Base(keychain)}
}

func checkHarnessLinux() DoctorCheck {
	if path, ok := findHarnessLinux(); ok {
		return DoctorCheck{Name: "harness_linux", Status: "ok", Message: path}
	}
	return DoctorCheck{Name: "harness_linux", Status: "warn", Message: "not found"}
}

func findHarnessLinux() (string, bool) {
	if path, err := exec.LookPath("agentpaas-harness-linux"); err == nil {
		return path, true
	}

	exe, err := os.Executable()
	if err != nil {
		return "", false
	}
	candidate := filepath.Join(filepath.Dir(exe), "agentpaas-harness-linux")
	if _, err := os.Stat(candidate); err == nil {
		return candidate, true
	}
	return "", false
}

func checkHomeDir(homeDir string) DoctorCheck {
	displayPath := shortenHome(homeDir)

	info, err := os.Stat(homeDir)
	if os.IsNotExist(err) {
		if isWritableDir(filepath.Dir(homeDir)) {
			return DoctorCheck{Name: "home_dir", Status: "ok", Message: displayPath + ", needs creation"}
		}
		return DoctorCheck{Name: "home_dir", Status: "fail", Message: displayPath + ", not writable"}
	}
	if err != nil {
		return DoctorCheck{Name: "home_dir", Status: "fail", Message: displayPath + ", not accessible"}
	}
	if !info.IsDir() {
		return DoctorCheck{Name: "home_dir", Status: "fail", Message: displayPath + ", not a directory"}
	}
	if !isWritableDir(homeDir) {
		return DoctorCheck{Name: "home_dir", Status: "fail", Message: displayPath + ", not writable"}
	}
	return DoctorCheck{Name: "home_dir", Status: "ok", Message: displayPath + ", writable"}
}

func runDockerFormat(timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, "docker", args...).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func isWritableDir(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return false
	}

	testFile := filepath.Join(path, ".agentpaas-doctor-write-test")
	f, err := os.OpenFile(testFile, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
	if err != nil {
		return false
	}
	_ = f.Close()
	_ = os.Remove(testFile)
	return true
}

func shortenHome(path string) string {
	userHome, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == userHome {
		return "~"
	}
	prefix := userHome + string(os.PathSeparator)
	if strings.HasPrefix(path, prefix) {
		return "~" + strings.TrimPrefix(path, userHome)
	}
	return path
}

func overallStatus(checks []DoctorCheck) string {
	hasWarn := false
	for _, c := range checks {
		switch c.Status {
		case "fail":
			return "fail"
		case "warn":
			hasWarn = true
		}
	}
	if hasWarn {
		return "warn"
	}
	return "ok"
}

func (o DoctorOutput) hasFailure() bool {
	for _, c := range o.Checks {
		if c.Status == "fail" {
			return true
		}
	}
	return false
}

func passedCount(checks []DoctorCheck) int {
	n := 0
	for _, c := range checks {
		if c.Status != "fail" {
			n++
		}
	}
	return n
}

// DoctorText returns human-readable doctor output.
func DoctorText(o DoctorOutput) string {
	labels := map[string]string{
		"version":       "Version",
		"docker_cli":    "Docker CLI",
		"docker_daemon": "Docker daemon",
		"keychain":      "macOS Keychain",
		"harness_linux": "Linux harness",
		"home_dir":      "Home directory",
	}

	var b strings.Builder
	b.WriteString("agentpaas doctor\n")
	b.WriteString("===============\n")

	for _, check := range o.Checks {
		label := labels[check.Name]
		if label == "" {
			label = check.Name
		}
		fmt.Fprintf(&b, "%-18s %s (%s)\n", label+":", check.Status, check.Message)
	}

	fmt.Fprintf(&b, "\nOverall: %d/%d checks passed\n", passedCount(o.Checks), len(o.Checks))
	return b.String()
}