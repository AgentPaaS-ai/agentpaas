package cli

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	controlv1 "github.com/AgentPaaS-ai/agentpaas/api/control/v1"
	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
	"github.com/AgentPaaS-ai/agentpaas/internal/install"
	"github.com/AgentPaaS-ai/agentpaas/internal/operator"
	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
	"github.com/AgentPaaS-ai/agentpaas/internal/secrets"
	"github.com/AgentPaaS-ai/agentpaas/internal/strutil"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// newPackCmd creates the `agent pack` command.
func newPackCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pack [project-dir]",
		Short: "Build an agent image from a project directory",
		Long: `Build a container image from an agent project via the control daemon.

Requires a running daemon. project-dir defaults to the current directory.
Refuses wildcard egress policies unless --allow-wildcard is set.`,
		Example: `  agentpaas pack ./my-agent
  agentpaas pack . --name weather --version 1.0.0
  agentpaas pack ./my-agent --allow-wildcard`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			projectDir := "."
			if len(args) > 0 {
				projectDir = args[0]
			}
			absPath, err := resolveCLIProjectPath(projectDir)
			if err != nil {
				return fmt.Errorf("new pack cmd: %w", err)
			}
			projectDir = absPath

			// BUG 9 fix: warn about wildcard egress policies before packing.
			{
				policyPath := filepath.Join(projectDir, "policy.yaml")
				if data, err := os.ReadFile(policyPath); err == nil {
					if hasWildcardEgress(data) {
						allowWildcard, _ := cmd.Flags().GetBool("allow-wildcard") // cobra flag default on missing
						if !allowWildcard {
							fmt.Fprintf(os.Stderr,
								"WARNING: policy.yaml contains wildcard egress (domain: '*'). "+
									"This allows the agent to access ANY HTTPS domain. "+
									"Specify exact domains for production agents. "+
									"Use --allow-wildcard to suppress this warning.\n")
							return fmt.Errorf("refusing to pack with wildcard egress policy (use --allow-wildcard to override)")
						}
						fmt.Fprintf(os.Stderr,
							"WARNING: policy.yaml contains wildcard egress (domain: '*'). "+
								"This allows the agent to access ANY HTTPS domain.\n")
					}
				}
			}

			agentName, _ := cmd.Flags().GetString("name")       // cobra flag default on missing
			agentVersion, _ := cmd.Flags().GetString("version") // cobra flag default on missing

			sock, err := socketPath(cmd)
			if err != nil {
				return fmt.Errorf("new pack cmd: %w", err)
			}
			client, conn, err := ConnectToDaemon(sock)
			if err != nil {
				return fmt.Errorf("new pack cmd: %w", err)
			}
			defer func() { _ = conn.Close() }() // best-effort close

			ctx, cancel := contextWithTimeout(5 * time.Minute)
			defer cancel()

			resp, err := client.Pack(ctx, &controlv1.PackRequest{
				AgentProjectPath: projectDir,
				AgentName:        agentName,
				AgentVersion:     agentVersion,
			})
			if err != nil {
				return fmt.Errorf("pack failed: %w", err)
			}

			result := struct {
				ImageDigest string `json:"image_digest"`
				BuildLog    string `json:"build_log,omitempty"`
			}{
				ImageDigest: resp.GetImageDigest(),
				BuildLog:    resp.GetBuildLog(),
			}
			return printTextOrJSON(jsonOutput(cmd), result, func(v interface{}) string {
				r := v.(struct {
					ImageDigest string `json:"image_digest"`
					BuildLog    string `json:"build_log,omitempty"`
				})
				return fmt.Sprintf("Image: %s\nDigest: %s", r.ImageDigest, r.ImageDigest)
			})
		},
	}
	cmd.Flags().String("name", "", "Override agent name from agent.yaml for this pack")
	cmd.Flags().String("version", "", "Override agent version from agent.yaml for this pack")
	cmd.Flags().Bool("allow-wildcard", false, "Allow packing when policy.yaml has domain: '*' egress (prints warning)")
	return cmd
}

// resolveRunTarget resolves a user-provided target (project path, image
// digest, or agent name) to a deployed agent name that the daemon's Run
// handler can accept.
//
//   - If target contains a path separator or starts with "." or "/", treat
//     it as a project directory — read agent.yaml to get the agent name.
//   - If target starts with "sha256:", scan deployed agents for a matching
//     image digest.
//   - Otherwise, treat it as the deployed agent name directly.
func resolveRunTarget(cmd *cobra.Command, client controlv1.ControlServiceClient, target string) (string, error) {
	if target == "" {
		return "", fmt.Errorf("agent name or project path is required")
	}

	// Case 1: project path (contains / or starts with . or /)
	if strings.Contains(target, "/") || strings.HasPrefix(target, ".") || strings.HasPrefix(target, "~") {
		absPath, err := filepath.Abs(target)
		if err != nil {
			return "", fmt.Errorf("resolve path %q: %w", target, err)
		}
		agentYAML, err := pack.LoadAgentYAML(absPath)
		if err != nil {
			return "", fmt.Errorf("read agent.yaml from %s: %w", absPath, err)
		}
		if agentYAML == nil {
			return "", fmt.Errorf("no agent.yaml found in %s — run 'agentpaas init' first", absPath)
		}
		if agentYAML.Name == "" {
			return "", fmt.Errorf("agent.yaml in %s has no 'name' field", absPath)
		}
		// A project-path run must use the exact source that was packed. The
		// daemon verifies deployed artifacts, but it cannot see the caller's
		// project directory, so perform this comparison at the CLI boundary.
		if homeDir, homeErr := getAgentpaasHome(cmd); homeErr == nil {
			if lock, lockErr := pack.LoadDeployedLock(homeDir, agentYAML.Name); lockErr == nil {
				ignore, ignoreErr := pack.LoadIgnore(absPath)
				if ignoreErr != nil {
					return "", fmt.Errorf("load .agentpaasignore from %s: %w", absPath, ignoreErr)
				}
				currentDigest, digestErr := pack.ComputeBuildInputDigest(absPath, ignore)
				if digestErr != nil {
					return "", fmt.Errorf("compute source digest for %s: %w", absPath, digestErr)
				}
				if currentDigest != lock.BuildInputDigest {
					return "", fmt.Errorf("source changed since pack; repack before running")
				}
			}
		}
		return agentYAML.Name, nil
	}

	// Case 2: image digest (starts with sha256:)
	if strings.HasPrefix(target, "sha256:") {
		// Scan deployed agents for a matching image digest
		homeDir, err := getAgentpaasHome(cmd)
		if err != nil {
			return "", fmt.Errorf("resolve agentpaas home: %w", err)
		}
		agentsDir := filepath.Join(homeDir, "state", "agents")
		entries, err := os.ReadDir(agentsDir)
		if err != nil {
			return "", fmt.Errorf("no deployed agents found — run 'agentpaas pack' first")
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			agentName := entry.Name()
			deployed, err := pack.LoadDeployedAgent(homeDir, agentName)
			if err != nil {
				continue // skip unreadable entries
			}
			if deployed.ImageDigest == target {
				return agentName, nil
			}
		}
		return "", fmt.Errorf("no deployed agent with image digest %s — run 'agentpaas pack' first", target)
	}

	// Case 1b: bare directory name (no /, ., or ~) that exists on disk
	if info, err := os.Stat(target); err == nil && info.IsDir() {
		absPath, err := filepath.Abs(target)
		if err != nil {
			return "", fmt.Errorf("resolve path %q: %w", target, err)
		}
		agentYAML, err := pack.LoadAgentYAML(absPath)
		if err != nil {
			return "", fmt.Errorf("read agent.yaml from %s: %w", absPath, err)
		}
		if agentYAML == nil {
			return "", fmt.Errorf("no agent.yaml found in %s — run 'agentpaas init' first", absPath)
		}
		if agentYAML.Name == "" {
			return "", fmt.Errorf("agent.yaml in %s has no 'name' field", absPath)
		}
		return agentYAML.Name, nil
	}

	// Case 3: agent name / name@pub8 / alias (installed resolution; Phase 1 bare name unchanged).
	resolved, err := resolveCLIAgentRef(cmd, target)
	if err != nil {
		return "", fmt.Errorf("resolve run target: %w", err)
	}
	return resolved.DaemonKey, nil
}

// getAgentpaasHome resolves the AgentPaaS home directory from the --home flag
// or AGENTPAAS_HOME env var, falling back to ~/.agentpaas.
func getAgentpaasHome(cmd *cobra.Command) (string, error) {
	homeFlag, _ := cmd.Flags().GetString("home") // cobra flag default on missing
	if homeFlag != "" {
		return homeFlag, nil
	}
	envHome := os.Getenv("AGENTPAAS_HOME")
	if envHome != "" {
		return envHome, nil
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get agentpaas home: %w", err)
	}
	return filepath.Join(homeDir, ".agentpaas"), nil
}

// newRunCmd creates the `agent run` command.
func newRunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run [image-or-project]",
		Short: "Start a new agent run",
		Long: `Start a new agent run through the control daemon.

Target may be a project path, deployed agent name, name@pub8, alias, or
sha256: image digest. Omitting the target is only valid with deployment
invocation flags. Requires a running daemon and a prior pack/install.`,
		Example: `  agentpaas run ./my-agent
  agentpaas run weather
  agentpaas run weather@a1b2c3d4
  agentpaas run list
  agentpaas run --deployment-ref prod --input '{"q":"hi"}'`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := ""
			if len(args) > 0 {
				target = args[0]
			}

			// B26 continuation / control flags — fail closed via daemon.
			continueRunID, _ := cmd.Flags().GetString("continue")         // cobra flag default on missing
			action, _ := cmd.Flags().GetString("action")                  // cobra flag default on missing
			attemptLease, _ := cmd.Flags().GetDuration("attempt-lease")   // cobra flag default on missing
			deploymentRef, _ := cmd.Flags().GetString("deployment-ref")   // cobra flag default on missing
			inputFlag, _ := cmd.Flags().GetString("input")                // cobra flag default on missing
			idempotencyKey, _ := cmd.Flags().GetString("idempotency-key") // cobra flag default on missing
			generatedKey := false

			// Deployment invocation path: when --deployment-ref or --input is set,
			// use InvokeDeployment RPC (API requires idempotency key).
			if deploymentRef != "" || inputFlag != "" {
				if deploymentRef == "" {
					// Treat positional arg as deployment ref when using --input.
					deploymentRef = target
				}
				if deploymentRef == "" {
					return fmt.Errorf("deployment ref is required for deployment invocation (use --deployment-ref or positional arg)")
				}
				if idempotencyKey == "" {
					// CLI generates a key when omitted; API requires one.
					id, err := newCLIIdempotencyKey()
					if err != nil {
						return fmt.Errorf("new run cmd: %w", err)
					}
					idempotencyKey = id
					generatedKey = true
					fmt.Printf("Generated idempotency key: %s\n", idempotencyKey)
				}
				inputBytes, err := readInputFlag(inputFlag)
				if err != nil {
					return fmt.Errorf("new run cmd: %w", err)
				}
				sock, err := socketPath(cmd)
				if err != nil {
					return fmt.Errorf("new run cmd: %w", err)
				}
				client, conn, err := ConnectToDaemon(sock)
				if err != nil {
					return fmt.Errorf("new run cmd: %w", err)
				}
				defer func() { _ = conn.Close() }() // best-effort close
				ctx, cancel := contextWithTimeout(30 * time.Second)
				defer cancel()
				resp, err := client.InvokeDeployment(ctx, &controlv1.InvokeDeploymentRequest{
					DeploymentRef:  deploymentRef,
					InputJson:      inputBytes,
					IdempotencyKey: idempotencyKey,
					CallerIdentity: "cli",
				})
				if err != nil {
					return fmt.Errorf("invoke deployment failed: %w", err)
				}
				if e := resp.GetError(); e != nil {
					return fmt.Errorf("%s: %s", e.GetCodeName(), e.GetMessage())
				}
				result := struct {
					Outcome        string `json:"outcome"`
					InvocationID   string `json:"invocation_id,omitempty"`
					RunID          string `json:"run_id,omitempty"`
					IdempotencyKey string `json:"idempotency_key,omitempty"`
				}{
					Outcome:        resp.GetOutcomeName(),
					InvocationID:   resp.GetInvocationId(),
					RunID:          resp.GetRunId(),
					IdempotencyKey: idempotencyKey,
				}
				_ = generatedKey // key material returned via other channel / displayed above
				return printTextOrJSON(jsonOutput(cmd), result, func(v interface{}) string {
					r := v.(struct {
						Outcome        string `json:"outcome"`
						InvocationID   string `json:"invocation_id,omitempty"`
						RunID          string `json:"run_id,omitempty"`
						IdempotencyKey string `json:"idempotency_key,omitempty"`
					})
					return fmt.Sprintf("Invoke outcome: %s (run %s)", r.Outcome, r.RunID)
				})
			}

			sock, err := socketPath(cmd)
			if err != nil {
				return fmt.Errorf("new run cmd: %w", err)
			}
			client, conn, err := ConnectToDaemon(sock)
			if err != nil {
				return fmt.Errorf("new run cmd: %w", err)
			}
			defer func() { _ = conn.Close() }() // best-effort close

			// Resolve the target to a deployed agent name.
			agentName, err := resolveRunTarget(cmd, client, target)
			if err != nil {
				return fmt.Errorf("new run cmd: %w", err)
			}
			displayAgent := agentName
			if homeDir, herr := getAgentpaasHome(cmd); herr == nil {
				displayAgent = install.DisplayForDaemonKey(filepath.Join(homeDir, "state"), agentName)
			}

			ctx, cancel := contextWithTimeout(90 * time.Second)
			defer cancel()

			runReq := &controlv1.RunRequest{
				AgentName: agentName,
			}
			if continueRunID != "" {
				runReq.ContinueRunId = continueRunID
			}
			if action != "" {
				runReq.RecoveryAction = action
			}
			if attemptLease > 0 {
				runReq.RequestedAttemptLeaseMs = attemptLease.Milliseconds()
			}
			if idempotencyKey != "" {
				runReq.IdempotencyKey = idempotencyKey
			}

			resp, err := client.Run(ctx, runReq)
			if err != nil {
				return fmt.Errorf("run failed: %w", err)
			}

			result := struct {
				RunID     string `json:"run_id"`
				Agent     string `json:"agent,omitempty"`
				AttemptID string `json:"attempt_id,omitempty"`
				Status    string `json:"status,omitempty"`
			}{
				RunID:     resp.GetRunId(),
				Agent:     displayAgent,
				AttemptID: resp.GetAttemptId(),
				Status:    resp.GetStatus(),
			}
			return printTextOrJSON(jsonOutput(cmd), result, func(v interface{}) string {
				r := v.(struct {
					RunID     string `json:"run_id"`
					Agent     string `json:"agent,omitempty"`
					AttemptID string `json:"attempt_id,omitempty"`
					Status    string `json:"status,omitempty"`
				})
				// Preserve legacy primary output line.
				if r.Agent != "" {
					return fmt.Sprintf("Run started: %s (agent %s)", r.RunID, r.Agent)
				}
				return fmt.Sprintf("Run started: %s", r.RunID)
			})
		},
	}
	cmd.Flags().String("continue", "", "Continue a prior run by run ID (reserved; not fully enabled yet)")
	cmd.Flags().String("action", "", "Recovery action: more_time, capability_up, or larger_context (reserved)")
	cmd.Flags().Duration("attempt-lease", 0, "Requested attempt lease duration (reserved; e.g. 30m)")
	cmd.Flags().String("input", "", "Deployment input as JSON string or @path/to/file.json")
	cmd.Flags().String("idempotency-key", "", "Idempotency key for deployment invoke (auto-generated when omitted)")
	cmd.Flags().String("deployment-ref", "", "Deployment alias or exact deployment ID to invoke")

	cmd.AddCommand(newListRunsCmd())
	cmd.AddCommand(newRunCancelCmd())
	cmd.AddCommand(newRunPauseCmd())
	cmd.AddCommand(newRunResumeCmd())
	cmd.AddCommand(newRunRestartCmd())
	cmd.AddCommand(newRunExtendCmd())
	return cmd
}

func newRunCancelCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "cancel <run-id>",
		Short:   "Cancel a run/workflow (not fully enabled yet)",
		Long:    `Request cancellation of a run or workflow. Surface is reserved; daemon may reject.`,
		Example: `  agentpaas run cancel run-01HXYZ...`,
		Args:    cobra.ExactArgs(1),
		RunE:    runControlNotEnabled("cancel"),
	}
}

func newRunPauseCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "pause <run-id>",
		Short:   "Pause a run/workflow (not fully enabled yet)",
		Long:    `Request pause of a run or workflow. Surface is reserved; daemon may reject.`,
		Example: `  agentpaas run pause run-01HXYZ...`,
		Args:    cobra.ExactArgs(1),
		RunE:    runControlNotEnabled("pause"),
	}
}

func newRunResumeCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "resume <run-id>",
		Short:   "Resume a run/workflow (not fully enabled yet)",
		Long:    `Request resume of a paused run or workflow. Surface is reserved; daemon may reject.`,
		Example: `  agentpaas run resume run-01HXYZ...`,
		Args:    cobra.ExactArgs(1),
		RunE:    runControlNotEnabled("resume"),
	}
}

func newRunRestartCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "restart <run-id>",
		Short:   "Restart a run/workflow (not fully enabled yet)",
		Long:    `Request restart of a run or workflow. Surface is reserved; daemon may reject.`,
		Example: `  agentpaas run restart run-01HXYZ...`,
		Args:    cobra.ExactArgs(1),
		RunE:    runControlNotEnabled("restart"),
	}
}

func newRunExtendCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "extend <run-id>",
		Short: "Amend run limits (not fully enabled yet)",
		Long: `Amend absolute run limits (max active time, max LLM spend).

Requires --reason and --idempotency-key. Surface is reserved; daemon may reject.`,
		Example: `  agentpaas run extend run-01HXYZ... --reason "need more time" \
    --idempotency-key ext-1 --max-active-time 2h`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			reason, _ := cmd.Flags().GetString("reason") // cobra flag default on missing
			if strings.TrimSpace(reason) == "" {
				return fmt.Errorf("--reason is required")
			}
			key, _ := cmd.Flags().GetString("idempotency-key") // cobra flag default on missing
			if key == "" {
				return fmt.Errorf("--idempotency-key is required for extend (API contract)")
			}
			maxActive, _ := cmd.Flags().GetDuration("max-active-time") // cobra flag default on missing
			maxSpend, _ := cmd.Flags().GetString("max-llm-spend-usd")  // cobra flag default on missing
			_ = maxActive                                              // reserved CLI flags not yet wired to RPC
			_ = maxSpend                                               // reserved CLI flags not yet wired to RPC

			sock, err := socketPath(cmd)
			if err != nil {
				return fmt.Errorf("new run extend cmd: %w", err)
			}
			client, conn, err := ConnectToDaemon(sock)
			if err != nil {
				return fmt.Errorf("new run extend cmd: %w", err)
			}
			defer func() { _ = conn.Close() }() // best-effort close
			ctx, cancel := contextWithTimeout(15 * time.Second)
			defer cancel()

			// Treat run-id as workflow_id for the amend-limits RPC skeleton.
			resp, err := client.AmendLimits(ctx, &controlv1.AmendLimitsRequest{
				WorkflowId:             args[0],
				NewMaxActiveDurationMs: maxActive.Milliseconds(),
				NewMaxLlmSpendDecimal:  maxSpend,
				Reason:                 reason,
				IdempotencyKey:         key,
				ActorIdentity:          "cli",
			})
			if err != nil {
				return fmt.Errorf("extend failed: %w", err)
			}
			if e := resp.GetError(); e != nil {
				return fmt.Errorf("%s: %s", e.GetCodeName(), e.GetMessage())
			}
			return fmt.Errorf("extend unexpectedly succeeded (not enabled in B26)")
		},
	}
	cmd.Flags().Duration("max-active-time", 0, "New absolute maximum active duration (e.g. 2h)")
	cmd.Flags().String("max-llm-spend-usd", "", "New absolute maximum LLM spend as a decimal USD string")
	cmd.Flags().Bool("extend-current-attempt", false, "Also extend the current attempt lease (reserved)")
	cmd.Flags().String("reason", "", "Human-readable reason for the limit amendment (required)")
	cmd.Flags().String("idempotency-key", "", "Idempotency key for safe retries (required)")
	return cmd
}

func runControlNotEnabled(command string) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		sock, err := socketPath(cmd)
		if err != nil {
			return fmt.Errorf("run control not enabled: %w", err)
		}
		client, conn, err := ConnectToDaemon(sock)
		if err != nil {
			return fmt.Errorf("run control not enabled: %w", err)
		}
		defer func() { _ = conn.Close() }() // best-effort close
		ctx, cancel := contextWithTimeout(15 * time.Second)
		defer cancel()

		key := "cli-control-" + command + "-" + args[0]
		var desired controlv1.ControlCommand
		switch command {
		case "cancel":
			// Prefer CancelWorkflow for cancel.
			resp, err := client.CancelWorkflow(ctx, &controlv1.CancelWorkflowRequest{
				WorkflowId:     args[0],
				Reason:         "cli cancel",
				ActorIdentity:  "cli",
				IdempotencyKey: key,
			})
			if err != nil {
				return fmt.Errorf("%s failed: %w", command, err)
			}
			if e := resp.GetError(); e != nil {
				return fmt.Errorf("%s: %s", e.GetCodeName(), e.GetMessage())
			}
			return fmt.Errorf("%s unexpectedly succeeded (not enabled in B26)", command)
		case "pause":
			desired = controlv1.ControlCommand_CONTROL_COMMAND_PAUSE
		case "resume":
			desired = controlv1.ControlCommand_CONTROL_COMMAND_RESUME
		case "restart":
			resp, err := client.RestartWorkflow(ctx, &controlv1.RestartWorkflowRequest{
				SourceWorkflowId: args[0],
				ActorIdentity:    "cli",
				IdempotencyKey:   key,
			})
			if err != nil {
				return fmt.Errorf("%s failed: %w", command, err)
			}
			if e := resp.GetError(); e != nil {
				return fmt.Errorf("%s: %s", e.GetCodeName(), e.GetMessage())
			}
			return fmt.Errorf("%s unexpectedly succeeded (not enabled in B26)", command)
		default:
			desired = controlv1.ControlCommand_CONTROL_COMMAND_UNSPECIFIED
		}
		resp, err := client.SetWorkflowDesiredState(ctx, &controlv1.SetWorkflowDesiredStateRequest{
			WorkflowId:     args[0],
			DesiredCommand: desired,
			ActorIdentity:  "cli",
			IdempotencyKey: key,
		})
		if err != nil {
			return fmt.Errorf("%s failed: %w", command, err)
		}
		if e := resp.GetError(); e != nil {
			return fmt.Errorf("%s: %s", e.GetCodeName(), e.GetMessage())
		}
		return fmt.Errorf("%s unexpectedly succeeded (not enabled in B26)", command)
	}
}

func readInputFlag(input string) ([]byte, error) {
	if input == "" {
		return []byte("{}"), nil
	}
	if strings.HasPrefix(input, "@") {
		path := strings.TrimPrefix(input, "@")
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read input file %s: %w", path, err)
		}
		return data, nil
	}
	return []byte(input), nil
}

func newCLIIdempotencyKey() (string, error) {
	// Use routedrun-compatible inv- prefix via crypto/rand hex.
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate idempotency key: %w", err)
	}
	return "inv-" + hex.EncodeToString(buf), nil
}

// newListRunsCmd creates the `agent run list` command.
func newListRunsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all active and recent agent runs",
		Long: `List active and recent runs known to the daemon.

Shows run ID, agent, and status. Use global --json for scripts.`,
		Example: `  agentpaas run list
  agentpaas run list --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			sock, err := socketPath(cmd)
			if err != nil {
				return fmt.Errorf("new list runs cmd: %w", err)
			}
			client, conn, err := ConnectToDaemon(sock)
			if err != nil {
				return fmt.Errorf("new list runs cmd: %w", err)
			}
			defer func() { _ = conn.Close() }() // best-effort close

			ctx, cancel := contextWithTimeout(15 * time.Second)
			defer cancel()

			resp, err := client.ListRuns(ctx, &controlv1.ListRunsRequest{})
			if err != nil {
				return fmt.Errorf("list runs failed: %w", err)
			}
			stateRoot := ""
			if homeDir, herr := getAgentpaasHome(cmd); herr == nil {
				stateRoot = filepath.Join(homeDir, "state")
			}

			return printTextOrJSON(jsonOutput(cmd), resp, func(v interface{}) string {
				r := v.(*controlv1.ListRunsResponse)
				if len(r.GetRuns()) == 0 {
					return "No recent runs.\n"
				}
				out := fmt.Sprintf("Recent runs (%d):\n", len(r.GetRuns()))
				for _, run := range r.GetRuns() {
					agentLabel := run.GetAgentName()
					if stateRoot != "" {
						agentLabel = install.DisplayForDaemonKey(stateRoot, run.GetAgentName())
					}
					out += fmt.Sprintf("  %s  %s  [%s]\n", run.GetRunId(), agentLabel, run.GetStatus())
				}
				return out
			})
		},
	}
}

// newStopCmd creates the `agent stop` command.
func newStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop <run-id>",
		Short: "Terminate a running agent",
		Long: `Stop a running agent by run ID via the control daemon.

Sends a stop request; the daemon terminates the container/process.`,
		Example: `  agentpaas stop run-01HXYZ...
  agentpaas stop run-01HXYZ... --json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			runID := args[0]
			sock, err := socketPath(cmd)
			if err != nil {
				return fmt.Errorf("new stop cmd: %w", err)
			}
			client, conn, err := ConnectToDaemon(sock)
			if err != nil {
				return fmt.Errorf("new stop cmd: %w", err)
			}
			defer func() { _ = conn.Close() }() // best-effort close

			ctx, cancel := contextWithTimeout(10 * time.Second)
			defer cancel()

			_, err = client.Stop(ctx, &controlv1.StopRequest{RunId: runID})
			if err != nil {
				return fmt.Errorf("stop failed: %w", err)
			}

			result := struct {
				Stopped         bool   `json:"stopped"`
				RunID           string `json:"run_id"`
				RequiresConfirm bool   `json:"requires_confirm"`
			}{Stopped: true, RunID: runID, RequiresConfirm: false}
			return printTextOrJSON(jsonOutput(cmd), result, func(v interface{}) string {
				r := v.(struct {
					Stopped         bool   `json:"stopped"`
					RunID           string `json:"run_id"`
					RequiresConfirm bool   `json:"requires_confirm"`
				})
				return fmt.Sprintf("Stopped run: %s", r.RunID)
			})
		},
	}
}

func newConfirmCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "confirm <confirmation-id>",
		Short: "Approve or decline a pending trust-boundary change",
		Long: `Resolve a pending trust-boundary confirmation from policy proposals.

Exactly one of --approve or --decline is required. List pending IDs with
'agentpaas confirmations'.`,
		Example: `  agentpaas confirmations
  agentpaas confirm conf-01HXYZ... --approve
  agentpaas confirm conf-01HXYZ... --decline`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			approve, _ := cmd.Flags().GetBool("approve") // cobra flag default on missing
			decline, _ := cmd.Flags().GetBool("decline") // cobra flag default on missing
			if approve == decline {
				return fmt.Errorf("exactly one of --approve or --decline is required")
			}

			sock, err := socketPath(cmd)
			if err != nil {
				return fmt.Errorf("new confirm cmd: %w", err)
			}
			client, conn, err := ConnectToDaemon(sock)
			if err != nil {
				return fmt.Errorf("new confirm cmd: %w", err)
			}
			defer func() { _ = conn.Close() }() // best-effort close

			decision := "decline"
			if approve {
				decision = "approve"
			}
			ctx, cancel := contextWithTimeout(10 * time.Second)
			defer cancel()
			resp, err := client.NextAction(ctx, &controlv1.NextActionRequest{
				Context: "confirm-change:" + decision + ":" + args[0],
			})
			if err != nil {
				return fmt.Errorf("confirm change failed: %w", err)
			}
			result := struct {
				ConfirmationID string `json:"confirmation_id"`
				Decision       string `json:"decision"`
				NextAction     string `json:"next_action"`
				Rationale      string `json:"rationale"`
			}{
				ConfirmationID: args[0],
				Decision:       decision + "d",
				NextAction:     resp.GetNextAction(),
				Rationale:      resp.GetRationale(),
			}
			return printTextOrJSON(jsonOutput(cmd), result, func(v interface{}) string {
				r := v.(struct {
					ConfirmationID string `json:"confirmation_id"`
					Decision       string `json:"decision"`
					NextAction     string `json:"next_action"`
					Rationale      string `json:"rationale"`
				})
				return fmt.Sprintf("%s: %s\nNext: %s", r.ConfirmationID, r.Decision, r.NextAction)
			})
		},
	}
	cmd.Flags().Bool("approve", false, "Approve the proposed trust-boundary change (mutually exclusive with --decline)")
	cmd.Flags().Bool("decline", false, "Decline the proposed trust-boundary change (mutually exclusive with --approve)")
	return cmd
}

type pendingConfirmationOutput struct {
	ID            string                 `json:"id"`
	CreatedAt     time.Time              `json:"created_at"`
	ExpiresAt     time.Time              `json:"expires_at"`
	ChangeType    string                 `json:"change_type"`
	RiskLevel     string                 `json:"risk_level"`
	Rationale     string                 `json:"rationale"`
	AffectedDests []string               `json:"affected_destinations,omitempty"`
	CredentialIDs []string               `json:"credential_ids,omitempty"`
	EvidenceRefs  []operator.EvidenceRef `json:"evidence_refs,omitempty"`
	ProposedPatch string                 `json:"proposed_patch,omitempty"`
	Status        string                 `json:"status"`
}

func newConfirmationsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "confirmations",
		Short: "List pending trust-boundary confirmations",
		Long: `List pending trust-boundary confirmations awaiting operator decision.

Use 'agentpaas confirm <id> --approve|--decline' to resolve each item.`,
		Example: `  agentpaas confirmations
  agentpaas confirmations --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			sock, err := socketPath(cmd)
			if err != nil {
				return fmt.Errorf("new confirmations cmd: %w", err)
			}
			client, conn, err := ConnectToDaemon(sock)
			if err != nil {
				return fmt.Errorf("new confirmations cmd: %w", err)
			}
			defer func() { _ = conn.Close() }() // best-effort close

			ctx, cancel := contextWithTimeout(10 * time.Second)
			defer cancel()
			resp, err := client.NextAction(ctx, &controlv1.NextActionRequest{Context: "confirmations:list"})
			if err != nil {
				return fmt.Errorf("list confirmations failed: %w", err)
			}
			var confirmations []pendingConfirmationOutput
			if err := json.Unmarshal([]byte(resp.GetParams()["confirmations_json"]), &confirmations); err != nil {
				return fmt.Errorf("decode confirmations: %w", err)
			}
			return printTextOrJSON(jsonOutput(cmd), confirmations, func(v interface{}) string {
				items := v.([]pendingConfirmationOutput)
				if len(items) == 0 {
					return "No pending confirmations."
				}
				var b strings.Builder
				for _, item := range items {
					fmt.Fprintf(
						&b,
						"%s\t%s\t%s\t%s\n",
						item.ID,
						item.ChangeType,
						item.RiskLevel,
						item.Rationale,
					)
				}
				return strings.TrimSuffix(b.String(), "\n")
			})
		},
	}
}

// newLogsCmd creates the `agent logs` command.
func newLogsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logs <run-id>",
		Short: "Follow or query agent logs",
		Long: `Stream or fetch log entries for a run from the control daemon.

By default prints the last --tail entries. Use -f/--follow to stream.
Command-local --json emits a single document with an entries array;
global --json emits one JSON object per line while streaming.`,
		Example: `  agentpaas logs run-01HXYZ...
  agentpaas logs run-01HXYZ... --tail 50
  agentpaas logs run-01HXYZ... -f
  agentpaas logs run-01HXYZ... --json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			runID := args[0]
			follow, _ := cmd.Flags().GetBool("follow") // cobra flag default on missing
			tail, _ := cmd.Flags().GetInt32("tail")    // cobra flag default on missing
			logsJSON, _ := cmd.Flags().GetBool("json") // cobra flag default on missing

			sock, err := socketPath(cmd)
			if err != nil {
				return fmt.Errorf("new logs cmd: %w", err)
			}
			client, conn, err := ConnectToDaemon(sock)
			if err != nil {
				return fmt.Errorf("new logs cmd: %w", err)
			}
			defer func() { _ = conn.Close() }() // best-effort close

			ctx, cancel := contextWithTimeout(60 * time.Second)
			defer cancel()

			stream, err := client.Logs(ctx, &controlv1.LogsRequest{
				RunId:  runID,
				Follow: follow,
				Tail:   tail,
			})
			if err != nil {
				return fmt.Errorf("logs failed: %w", err)
			}

			jsonOut := jsonOutput(cmd) && !logsJSON
			var entries []map[string]interface{}
			for {
				entry, err := stream.Recv()
				if err == io.EOF {
					break
				}
				if err != nil {
					return fmt.Errorf("log stream error: %w", err)
				}
				fields := entry.GetFields()
				entryMap := map[string]interface{}{
					"timestamp": entry.GetTimestamp().AsTime().Format(time.RFC3339Nano),
					"level":     entry.GetLevel(),
					"message":   entry.GetMessage(),
				}
				if logsJSON {
					if fields != nil {
						entryMap["fields"] = fields
					}
					entries = append(entries, entryMap)
				} else if jsonOut {
					entryMap["run_id"] = entry.GetRunId()
					entryMap["fields"] = fields
					data, _ := json.Marshal(entryMap) // best-effort JSON for display
					fmt.Println(string(data))
				} else {
					ts := ""
					if entry.GetTimestamp() != nil {
						ts = entry.GetTimestamp().AsTime().Format(time.RFC3339)
					}
					fmt.Printf("[%s] %s %s\n", ts, entry.GetLevel(), entry.GetMessage())
				}
			}
			if logsJSON {
				data, err := json.Marshal(map[string]interface{}{
					"run_id":  runID,
					"entries": entries,
				})
				if err != nil {
					return fmt.Errorf("json marshal error: %w", err)
				}
				fmt.Println(string(data))
			}
			return nil
		},
	}
	cmd.Flags().BoolP("follow", "f", false, "Follow log output until the stream ends or is interrupted")
	cmd.Flags().Int32("tail", 100, "Number of historical log entries to return before following (default 100)")
	cmd.Flags().Bool("json", false, "Emit one JSON document {run_id, entries} instead of line-oriented output")
	return cmd
}

// newPolicyCmd creates the `agent policy` command.
func newPolicyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "policy",
		Short: "Manage agent policies",
		Long: `Manage agent network/credential policies (policy.yaml).

init scaffolds templates; apply validates/applies YAML; show reads the
project policy; explain/propose help debug denials.`,
		Example: `  agentpaas policy init ./my-agent --template deny-all
  agentpaas policy show ./my-agent
  agentpaas policy apply ./my-agent/policy.yaml --dry-run
  agentpaas policy explain api.example.com`,
	}
	cmd.AddCommand(newPolicyApplyCmd())
	cmd.AddCommand(newPolicyShowCmd())
	cmd.AddCommand(newPolicyExplainCmd())
	cmd.AddCommand(newPolicyProposeCmd())
	cmd.AddCommand(newPolicyInitCmd())
	return cmd
}

func newPolicyApplyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apply <policy-file>",
		Short: "Apply or validate a policy file",
		Long: `Apply a policy.yaml through the daemon, or validate with --dry-run.

Requires a running daemon. Prints the resulting policy digest and rule count.`,
		Example: `  agentpaas policy apply ./policy.yaml
  agentpaas policy apply ./policy.yaml --dry-run
  agentpaas policy apply ./policy.yaml --json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			policyFile := args[0]
			data, err := os.ReadFile(policyFile)
			if err != nil {
				return fmt.Errorf("read policy file: %w", err)
			}
			dryRun, _ := cmd.Flags().GetBool("dry-run") // cobra flag default on missing

			sock, err := socketPath(cmd)
			if err != nil {
				return fmt.Errorf("new policy apply cmd: %w", err)
			}
			client, conn, err := ConnectToDaemon(sock)
			if err != nil {
				return fmt.Errorf("new policy apply cmd: %w", err)
			}
			defer func() { _ = conn.Close() }() // best-effort close

			ctx, cancel := contextWithTimeout(15 * time.Second)
			defer cancel()

			resp, err := client.PolicyApply(ctx, &controlv1.PolicyApplyRequest{
				PolicyYaml: string(data),
				DryRun:     dryRun,
			})
			if err != nil {
				return fmt.Errorf("policy apply failed: %w", err)
			}

			result := struct {
				PolicyDigest string   `json:"policy_digest"`
				RulesApplied int32    `json:"rules_applied"`
				Warnings     []string `json:"warnings,omitempty"`
				DryRun       bool     `json:"dry_run"`
			}{
				PolicyDigest: resp.GetPolicyDigest(),
				RulesApplied: resp.GetRulesApplied(),
				Warnings:     resp.GetWarnings(),
				DryRun:       dryRun,
			}
			return printTextOrJSON(jsonOutput(cmd), result, func(v interface{}) string {
				r := v.(struct {
					PolicyDigest string   `json:"policy_digest"`
					RulesApplied int32    `json:"rules_applied"`
					Warnings     []string `json:"warnings,omitempty"`
					DryRun       bool     `json:"dry_run"`
				})
				out := fmt.Sprintf("Policy: %s (%d rules)", r.PolicyDigest, r.RulesApplied)
				if r.DryRun {
					out += " [dry-run]"
				}
				return out
			})
		},
	}
	cmd.Flags().Bool("dry-run", false, "Validate and report the policy digest without applying it")
	return cmd
}

func newPolicyShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show [run-id]",
		Short: "Show the active policy for a run or project",
		Long: `Show policy.yaml for a project directory, or (future) the policy for a run ID.

When the argument is omitted, uses the current directory. Arguments starting
with run- query by run ID (not yet fully implemented).`,
		Example: `  agentpaas policy show
  agentpaas policy show ./my-agent
  agentpaas policy show ./my-agent --json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := ""
			if len(args) > 0 {
				target = args[0]
			}

			// If target looks like a run_id (starts with "run-"), query daemon
			if strings.HasPrefix(target, "run-") {
				// Existing daemon query path (keep for future when policy store exists)
				// For now, return not-yet-implemented for run-based queries
				result := struct {
					SchemaVersion string `json:"schema_version"`
					RunID         string `json:"run_id,omitempty"`
					Message       string `json:"message"`
				}{
					SchemaVersion: operator.SchemaVersion,
					RunID:         target,
					Message:       "policy show by run-id is not yet implemented; use a project directory instead",
				}
				return printTextOrJSON(jsonOutput(cmd), result, func(v interface{}) string {
					return result.Message
				})
			}

			// Treat target as a project directory — read policy.yaml
			projectDir := target
			if projectDir == "" {
				projectDir = "."
			}
			policyPath := filepath.Join(projectDir, "policy.yaml")

			data, err := os.ReadFile(policyPath)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return fmt.Errorf("policy.yaml not found in %s; run 'agentpaas policy init %s' to create one", projectDir, projectDir)
				}
				return fmt.Errorf("read policy: %w", err)
			}

			result := struct {
				SchemaVersion string `json:"schema_version"`
				ProjectDir    string `json:"project_dir"`
				Policy        string `json:"policy"`
			}{
				SchemaVersion: operator.SchemaVersion,
				ProjectDir:    projectDir,
				Policy:        string(data),
			}
			return printTextOrJSON(jsonOutput(cmd), result, func(v interface{}) string {
				return string(data)
			})
		},
	}
}

func newPolicyExplainCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "explain <run-id|destination>",
		Short: "Explain why a destination was denied by policy",
		Long: `Ask the daemon why a destination (or run) was denied by policy.

Requires a running daemon. Prefer a hostname/URL destination string.`,
		Example: `  agentpaas policy explain api.openai.com
  agentpaas policy explain https://api.example.com/v1`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := args[0]
			sock, err := socketPath(cmd)
			if err != nil {
				return fmt.Errorf("new policy explain cmd: %w", err)
			}
			client, conn, err := ConnectToDaemon(sock)
			if err != nil {
				return fmt.Errorf("new policy explain cmd: %w", err)
			}
			defer func() { _ = conn.Close() }() // best-effort close

			ctx, cancel := contextWithTimeout(10 * time.Second)
			defer cancel()

			resp, err := client.ExplainPolicyDenial(ctx, &controlv1.ExplainPolicyDenialRequest{
				RunId:             target,
				DeniedDestination: target,
			})
			if err != nil {
				return fmt.Errorf("explain denial failed: %w", err)
			}

			// Build operator-schema-shaped JSON output
			result := operator.ExplainPolicyDenialResponse{
				SchemaVersion:  resp.GetSchemaVersion(),
				RunID:          resp.GetRunId(),
				DeniedAction:   resp.GetDeniedAction(),
				BlockingRuleID: resp.GetBlockingRuleId(),
				PolicyDigest:   resp.GetPolicyDigest(),
				Rationale:      resp.GetRationale(),
				NextAction:     operator.NextAction(resp.GetNextAction()),
			}
			return printTextOrJSON(jsonOutput(cmd), result, func(v interface{}) string {
				r := v.(operator.ExplainPolicyDenialResponse)
				return fmt.Sprintf("Denied: %s\nRule: %s\nReason: %s\nNext: %s",
					r.DeniedAction, r.BlockingRuleID, r.Rationale, r.NextAction)
			})
		},
	}
}

func newPolicyProposeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "propose <desired-behavior>",
		Short: "Suggest a policy patch for a desired behavior",
		Long: `Suggest a policy patch that would allow a desired egress/behavior.

Requires a running daemon. High-risk patches may require confirmation.`,
		Example: `  agentpaas policy propose "allow HTTPS to api.openai.com"
  agentpaas policy propose "allow MCP server at localhost:3000"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			behavior := args[0]
			sock, err := socketPath(cmd)
			if err != nil {
				return fmt.Errorf("new policy propose cmd: %w", err)
			}
			client, conn, err := ConnectToDaemon(sock)
			if err != nil {
				return fmt.Errorf("new policy propose cmd: %w", err)
			}
			defer func() { _ = conn.Close() }() // best-effort close

			ctx, cancel := contextWithTimeout(10 * time.Second)
			defer cancel()

			resp, err := client.RecommendPolicyPatch(ctx, &controlv1.RecommendPolicyPatchRequest{
				DesiredBehavior: behavior,
			})
			if err != nil {
				return fmt.Errorf("recommend patch failed: %w", err)
			}

			// Build operator-schema-shaped JSON output
			confirmation := operator.ConfirmationRequirement{
				RequiresConfirmation: resp.GetConfirmation().GetRequiresConfirmation(),
				ConfirmationID:       resp.GetConfirmation().GetConfirmationId(),
				RiskLevel:            operator.RiskLevel(resp.GetConfirmation().GetRiskLevel()),
				Rationale:            resp.GetConfirmation().GetRationale(),
				AffectedDestinations: resp.GetConfirmation().GetAffectedDestinations(),
				CredentialIDs:        resp.GetConfirmation().GetCredentialIds(),
			}
			result := operator.RecommendPolicyPatchResponse{
				SchemaVersion:        resp.GetSchemaVersion(),
				ProposedPatch:        resp.GetProposedPatch(),
				RiskLevel:            operator.RiskLevel(resp.GetRiskLevel()),
				Rationale:            resp.GetRationale(),
				AffectedDestinations: resp.GetAffectedDestinations(),
				CredentialIDs:        resp.GetCredentialIds(),
				Confirmation:         confirmation,
				NextAction:           operator.NextAction(resp.GetNextAction()),
			}
			return printTextOrJSON(jsonOutput(cmd), result, func(v interface{}) string {
				r := v.(operator.RecommendPolicyPatchResponse)
				return fmt.Sprintf("Patch: %s\nRisk: %s\nReason: %s\nConfirm required: %v",
					r.ProposedPatch, r.RiskLevel, r.Rationale, r.Confirmation.RequiresConfirmation)
			})
		},
	}
}

var secretStoreFactory = newDefaultSecretStore

// newSecretCmd creates the `agent secret` command.
func newSecretCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "secret",
		Aliases: []string{"secrets"},
		Short:   "Manage local profile secrets",
		Long: `Manage local secrets in the platform keychain (values never printed).

add/set and rotate read the secret value from stdin (or a TTY prompt).
Use with installed map-credential to wire agents to local credentials.`,
		Example: `  printf '%s' "$OPENAI_API_KEY" | agentpaas secret add openai-key
  agentpaas secret list
  agentpaas secret test openai-key --provider openai
  agentpaas secret remove openai-key`,
	}

	cmd.AddCommand(&cobra.Command{
		Use:     "add <name>",
		Aliases: []string{"set"},
		Short:   "Create or update a secret from stdin",
		Long: `Store a secret value under name in the local keychain.

Reads the value from stdin when piped; prompts on a TTY. Values are never
echoed back by list or show commands.`,
		Example: `  printf '%s' "$OPENAI_API_KEY" | agentpaas secret add openai-key
  agentpaas secret set openai-key < ./key.txt`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if err := secrets.ValidateSecretName(name); err != nil {
				return fmt.Errorf("new secret cmd: %w", err)
			}
			value, err := readSecretValue(cmd)
			if err != nil {
				return fmt.Errorf("new secret cmd: %w", err)
			}
			store, err := secretStoreFactory(cmd)
			if err != nil {
				return fmt.Errorf("new secret cmd: %w", err)
			}
			if err := store.Set(cmd.Context(), name, value); err != nil {
				return fmt.Errorf("new secret cmd: %w", err)
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "secret %q stored\n", name)
			return err
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:     "rotate <name>",
		Short:   "Replace a secret with a new value from stdin (atomic)",
		Long:    `Replace an existing secret value atomically from stdin or a TTY prompt.`,
		Example: `  printf '%s' "$NEW_KEY" | agentpaas secret rotate openai-key`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if err := secrets.ValidateSecretName(name); err != nil {
				return fmt.Errorf("new secret cmd: %w", err)
			}
			value, err := readSecretValue(cmd)
			if err != nil {
				return fmt.Errorf("new secret cmd: %w", err)
			}
			store, err := secretStoreFactory(cmd)
			if err != nil {
				return fmt.Errorf("new secret cmd: %w", err)
			}
			if err := store.Set(cmd.Context(), name, value); err != nil {
				return fmt.Errorf("new secret cmd: %w", err)
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "secret %q rotated\n", name)
			return err
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List secret metadata",
		Long:  `List secret names and timestamps without revealing values.`,
		Example: `  agentpaas secret list
  agentpaas secret list --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := secretStoreFactory(cmd)
			if err != nil {
				return fmt.Errorf("new secret cmd: %w", err)
			}
			meta, err := store.List(cmd.Context())
			if err != nil {
				return fmt.Errorf("new secret cmd: %w", err)
			}
			return writeSecretList(cmd, meta)
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:     "remove <name>",
		Aliases: []string{"rm"},
		Short:   "Remove a secret",
		Long:    `Delete a secret from the local keychain by name.`,
		Example: `  agentpaas secret remove openai-key
  agentpaas secret rm openai-key`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if err := secrets.ValidateSecretName(name); err != nil {
				return fmt.Errorf("new secret cmd: %w", err)
			}
			store, err := secretStoreFactory(cmd)
			if err != nil {
				return fmt.Errorf("new secret cmd: %w", err)
			}
			if err := store.Delete(cmd.Context(), name); err != nil {
				return fmt.Errorf("new secret cmd: %w", err)
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "secret %q removed\n", name)
			return err
		},
	})

	testCmd := &cobra.Command{
		Use:   "test <name>",
		Short: "Validate a credential with a trivial provider API call",
		Long: `Load the secret and make a lightweight authenticated call to the provider.

Provider is auto-detected from the secret name when --provider is omitted
(openai, anthropic, xai, nous, openrouter).`,
		Example: `  agentpaas secret test openai-key
  agentpaas secret test my-key --provider anthropic`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if err := secrets.ValidateSecretName(name); err != nil {
				return fmt.Errorf("new secret cmd: %w", err)
			}
			provider, _ := cmd.Flags().GetString("provider") // cobra flag default on missing
			if provider == "" {
				provider = detectProviderFromName(name)
			}
			store, err := secretStoreFactory(cmd)
			if err != nil {
				return fmt.Errorf("new secret cmd: %w", err)
			}
			value, err := store.Get(cmd.Context(), name)
			if err != nil {
				return fmt.Errorf("secret %q: %w", name, err)
			}
			result := secrets.TestProvider(cmd.Context(), provider, value)
			if result.Status == "ok" {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "secret %q: %s test OK (%s, HTTP %d)\n", name, result.Provider, result.Endpoint, result.HTTPStatus) // best-effort CLI write
			} else {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "secret %q: %s test FAILED: %s\n", name, result.Provider, result.Detail) // best-effort CLI write
				return fmt.Errorf("credential test failed for %q", name)
			}
			return nil
		},
	}
	testCmd.Flags().String("provider", "", "Credential provider: openai, anthropic, xai, nous, or openrouter (auto-detected from name if omitted)")
	cmd.AddCommand(testCmd)

	return cmd
}

func detectProviderFromName(name string) string {
	lower := strings.ToLower(name)
	if strings.Contains(lower, "openrouter") {
		return "openrouter"
	}
	if strings.Contains(lower, "openai") || strings.Contains(lower, "gpt") {
		return "openai"
	}
	if strings.Contains(lower, "anthropic") || strings.Contains(lower, "claude") {
		return "anthropic"
	}
	if strings.Contains(lower, "xai") || strings.Contains(lower, "grok") {
		return "xai"
	}
	if strings.Contains(lower, "nous") || strings.Contains(lower, "deepseek") {
		return "nous"
	}
	return "openrouter"
}

func newDefaultSecretStore(cmd *cobra.Command) (secrets.SecretStore, error) {
	homeDir, err := homeDirPath(cmd)
	if err != nil {
		return nil, fmt.Errorf("new default secret store: %w", err)
	}
	return secrets.NewKeychainStore(secrets.KeychainServiceName(homeDir))
}

func readSecretValue(cmd *cobra.Command) ([]byte, error) {
	in := cmd.InOrStdin()
	if isTerminal(in) {
		if _, err := fmt.Fprint(cmd.ErrOrStderr(), "Secret value: "); err != nil {
			return nil, fmt.Errorf("read secret value: %w", err)
		}
		reader := bufio.NewReader(io.LimitReader(in, secrets.MaxSecretValueSize+2))
		value, err := reader.ReadBytes('\n')
		if err != nil && err != io.EOF {
			return nil, fmt.Errorf("read secret value: %w", err)
		}
		if len(value) > 0 && value[len(value)-1] == '\n' {
			value = value[:len(value)-1]
		}
		if len(value) > 0 && value[len(value)-1] == '\r' {
			value = value[:len(value)-1]
		}
		if len(value) > secrets.MaxSecretValueSize {
			return nil, fmt.Errorf("%w: exceeds %d byte limit", secrets.ErrSecretTooLarge, secrets.MaxSecretValueSize)
		}
		return value, nil
	}

	value, err := io.ReadAll(io.LimitReader(in, secrets.MaxSecretValueSize+1))
	if err != nil {
		return nil, fmt.Errorf("read secret value: %w", err)
	}
	if len(value) > secrets.MaxSecretValueSize {
		return nil, fmt.Errorf("%w: exceeds %d byte limit", secrets.ErrSecretTooLarge, secrets.MaxSecretValueSize)
	}
	// Trim trailing newlines/whitespace (piped input often includes a trailing \n)
	value = bytes.TrimRight(value, "\r\n	 ")
	return value, nil
}

func isTerminal(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

type secretListItem struct {
	Name         string    `json:"name"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	LastUsedAt   time.Time `json:"last_used_at"`
	ReferencedBy []string  `json:"referenced_by"`
}

func writeSecretList(cmd *cobra.Command, meta []secrets.SecretMeta) error {
	if jsonOutput(cmd) {
		items := make([]secretListItem, 0, len(meta))
		for _, m := range meta {
			items = append(items, secretListItem{
				Name:         m.Name,
				CreatedAt:    m.CreatedAt,
				UpdatedAt:    m.UpdatedAt,
				LastUsedAt:   m.LastUsedAt,
				ReferencedBy: []string{},
			})
		}
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(items)
	}

	tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "NAME\tCREATED_AT\tUPDATED_AT\tLAST_USED_AT\tREFERENCED_BY"); err != nil {
		return fmt.Errorf("write secret list: %w", err)
	}
	for _, m := range meta {
		if _, err := fmt.Fprintf(
			tw,
			"%s\t%s\t%s\t%s\t-\n",
			m.Name,
			formatSecretTime(m.CreatedAt),
			formatSecretTime(m.UpdatedAt),
			formatSecretTime(m.LastUsedAt),
		); err != nil {
			return err
		}
	}
	return tw.Flush()
}

func formatSecretTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.UTC().Format(time.RFC3339Nano)
}

// newAuditCmd creates the `agent audit` command.
func newAuditCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Query and export audit logs",
		Long: `Query, export, and verify the local audit log hash chain.

query/export talk to the daemon. verify reads local audit files under
<home>/state and does not require a daemon.`,
		Example: `  agentpaas audit query --run-id run-01HXYZ...
  agentpaas audit export -o ./audit.json --format json
  agentpaas audit verify`,
	}
	cmd.AddCommand(newAuditQueryCmd())
	cmd.AddCommand(newAuditExportCmd())
	cmd.AddCommand(newAuditVerifyCmd())
	return cmd
}

func newAuditVerifyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Verify the audit hash chain and checkpoints",
		Long: `Verify integrity of the local audit JSONL and checkpoint files.

Defaults to <home>/state/audit.jsonl and audit.jsonl.checkpoints.
Does not require a running daemon.`,
		Example: `  agentpaas audit verify
  agentpaas audit verify --audit /path/audit.jsonl --checkpoints /path/audit.jsonl.checkpoints`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			auditPath, _ := cmd.Flags().GetString("audit")             // cobra flag default on missing
			checkpointsPath, _ := cmd.Flags().GetString("checkpoints") // cobra flag default on missing
			if auditPath == "" || checkpointsPath == "" {
				homeDir, err := homeDirPath(cmd)
				if err != nil {
					return fmt.Errorf("new audit verify cmd: %w", err)
				}
				stateDir := filepath.Join(homeDir, "state")
				if auditPath == "" {
					auditPath = filepath.Join(stateDir, "audit.jsonl")
				}
				if checkpointsPath == "" {
					checkpointsPath = filepath.Join(stateDir, "audit.jsonl.checkpoints")
				}
			}

			result, err := audit.VerifyAuditChain(auditPath, checkpointsPath, nil)
			if err != nil {
				return fmt.Errorf("audit verification failed: %w", err)
			}
			if jsonOutput(cmd) {
				if err := json.NewEncoder(cmd.OutOrStdout()).Encode(result); err != nil {
					return fmt.Errorf("new audit verify cmd: %w", err)
				}
			} else if len(result.Issues) == 0 {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Audit chain valid: %d records, %d checkpoints\n", result.AuditRecordCount, result.CheckpointCount) // best-effort CLI write
			} else {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Audit chain verification FAILED") // best-effort CLI write
				for _, issue := range result.Issues {
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "- %s\n", issue.Message) // best-effort CLI write
				}
			}
			if len(result.Issues) > 0 {
				return fmt.Errorf("audit chain verification failed: %d issue(s)", len(result.Issues))
			}
			return nil
		},
	}
	cmd.Flags().String("audit", "", "Path to audit JSONL (default: <home>/state/audit.jsonl)")
	cmd.Flags().String("checkpoints", "", "Path to checkpoints JSONL (default: <home>/state/audit.jsonl.checkpoints)")
	return cmd
}

func newAuditQueryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "query",
		Short: "Query audit log entries",
		Long: `Query audit events from the daemon with optional run or agent filters.

Requires a running daemon. --limit is an alias for --page-size.`,
		Example: `  agentpaas audit query
  agentpaas audit query --run-id run-01HXYZ...
  agentpaas audit query --agent-name weather --limit 20`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			runID, _ := cmd.Flags().GetString("run-id")           // cobra flag default on missing
			agentFilter, _ := cmd.Flags().GetString("agent-name") // cobra flag default on missing
			pageSize, _ := cmd.Flags().GetInt32("page-size")      // cobra flag default on missing
			limit, _ := cmd.Flags().GetInt32("limit")             // cobra flag default on missing
			// --limit is an alias for --page-size; use whichever was explicitly set.
			if limit != 50 && pageSize == 50 {
				pageSize = limit
			}
			if agentFilter != "" {
				resolved, err := resolveCLIAgentRef(cmd, agentFilter)
				if err != nil {
					return fmt.Errorf("new audit query cmd: %w", err)
				}
				agentFilter = resolved.DaemonKey
			}

			sock, err := socketPath(cmd)
			if err != nil {
				return fmt.Errorf("new audit query cmd: %w", err)
			}
			client, conn, err := ConnectToDaemon(sock)
			if err != nil {
				return fmt.Errorf("new audit query cmd: %w", err)
			}
			defer func() { _ = conn.Close() }() // best-effort close

			ctx, cancel := contextWithTimeout(15 * time.Second)
			defer cancel()

			resp, err := client.AuditQuery(ctx, &controlv1.AuditQueryRequest{
				RunId:     runID,
				AgentName: agentFilter,
				PageSize:  pageSize,
			})
			if err != nil {
				return fmt.Errorf("audit query failed: %w", err)
			}

			type entryJSON struct {
				EventID   string    `json:"event_id"`
				EventType string    `json:"event_type"`
				RunID     string    `json:"run_id"`
				Timestamp time.Time `json:"timestamp"`
			}
			entries := make([]entryJSON, 0, len(resp.GetEntries()))
			for _, e := range resp.GetEntries() {
				var ts time.Time
				if e.GetTimestamp() != nil {
					ts = e.GetTimestamp().AsTime()
				}
				entries = append(entries, entryJSON{
					EventID:   e.GetEventId(),
					EventType: e.GetEventType().String(),
					RunID:     e.GetRunId(),
					Timestamp: ts,
				})
			}
			result := struct {
				Entries       []entryJSON `json:"entries"`
				TotalCount    int32       `json:"total_count"`
				NextPageToken string      `json:"next_page_token,omitempty"`
			}{
				Entries:       entries,
				TotalCount:    resp.GetTotalCount(),
				NextPageToken: resp.GetNextPageToken(),
			}
			return printTextOrJSON(jsonOutput(cmd), result, func(v interface{}) string {
				r := v.(struct {
					Entries       []entryJSON `json:"entries"`
					TotalCount    int32       `json:"total_count"`
					NextPageToken string      `json:"next_page_token,omitempty"`
				})
				return fmt.Sprintf("%d entries (total: %d)", len(r.Entries), r.TotalCount)
			})
		},
	}
	cmd.Flags().String("run-id", "", "Return only events for this run ID")
	cmd.Flags().String("agent-name", "", "Filter by agent name, name@pub8, or display alias")
	cmd.Flags().Int32("page-size", 50, "Maximum number of results to return (default 50; alias: --limit)")
	cmd.Flags().Int32("limit", 50, "Maximum number of results (alias of --page-size; default 50)")
	return cmd
}

func newAuditExportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export audit log entries",
		Long: `Export audit log entries from the daemon to stdout or a file.

Requires a running daemon. Formats: json (default), csv, ndjson.`,
		Example: `  agentpaas audit export
  agentpaas audit export --format ndjson -o ./audit.ndjson
  agentpaas audit export --format csv -o ./audit.csv`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			format, _ := cmd.Flags().GetString("format") // cobra flag default on missing
			output, _ := cmd.Flags().GetString("output") // cobra flag default on missing

			sock, err := socketPath(cmd)
			if err != nil {
				return fmt.Errorf("new audit export cmd: %w", err)
			}
			client, conn, err := ConnectToDaemon(sock)
			if err != nil {
				return fmt.Errorf("new audit export cmd: %w", err)
			}
			defer func() { _ = conn.Close() }() // best-effort close

			ctx, cancel := contextWithTimeout(60 * time.Second)
			defer cancel()

			resp, err := client.AuditExport(ctx, &controlv1.AuditExportRequest{
				Format: format,
			})
			if err != nil {
				return fmt.Errorf("audit export failed: %w", err)
			}

			if output != "" {
				if err := os.WriteFile(output, resp.GetData(), 0644); err != nil {
					return fmt.Errorf("write export file: %w", err)
				}
				result := struct {
					Output     string `json:"output"`
					EntryCount int32  `json:"entry_count"`
					Format     string `json:"format"`
				}{Output: output, EntryCount: resp.GetEntryCount(), Format: format}
				return printTextOrJSON(jsonOutput(cmd), result, func(v interface{}) string {
					r := v.(struct {
						Output     string `json:"output"`
						EntryCount int32  `json:"entry_count"`
						Format     string `json:"format"`
					})
					return fmt.Sprintf("Exported %d entries to %s (%s)", r.EntryCount, r.Output, r.Format)
				})
			}

			// Write to stdout
			fmt.Print(string(resp.GetData()))
			return nil
		},
	}
	cmd.Flags().String("format", "json", "Export format: json, csv, or ndjson (default json)")
	cmd.Flags().StringP("output", "o", "", "Write export to this file path instead of stdout")
	return cmd
}

// newValidateCmd creates the `agent validate` command.
func newValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate <project-path>",
		Short: "Validate an agent project directory structure",
		Long: `Validate that a project directory is ready to pack/run.

Requires a running daemon. Reports readiness, runtime, and issues.`,
		Example: `  agentpaas validate ./my-agent
  agentpaas validate ./my-agent --json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			projectPath, err := resolveCLIProjectPath(args[0])
			if err != nil {
				return fmt.Errorf("new validate cmd: %w", err)
			}
			sock, err := socketPath(cmd)
			if err != nil {
				return fmt.Errorf("new validate cmd: %w", err)
			}
			client, conn, err := ConnectToDaemon(sock)
			if err != nil {
				return fmt.Errorf("new validate cmd: %w", err)
			}
			defer func() { _ = conn.Close() }() // best-effort close

			ctx, cancel := contextWithTimeout(15 * time.Second)
			defer cancel()

			resp, err := client.ValidateAgentProject(ctx, &controlv1.ValidateAgentProjectRequest{
				ProjectPath: projectPath,
			})
			if err != nil {
				return fmt.Errorf("validate failed: %w", err)
			}

			// Build operator-schema-shaped JSON output
			issues := make([]operator.ValidationIssue, 0, len(resp.GetIssues()))
			for _, iss := range resp.GetIssues() {
				issues = append(issues, operator.ValidationIssue{
					Category:   operator.ErrorCategory(iss.GetCategory()),
					Message:    iss.GetMessage(),
					NextAction: operator.NextAction(iss.GetNextAction()),
				})
			}
			result := operator.ValidateAgentProjectResponse{
				SchemaVersion: resp.GetSchemaVersion(),
				Ready:         resp.GetReady(),
				ProjectDir:    resp.GetProjectDir(),
				Runtime:       resp.GetRuntime(),
				Issues:        issues,
			}
			return printTextOrJSON(jsonOutput(cmd), result, func(v interface{}) string {
				r := v.(operator.ValidateAgentProjectResponse)
				if r.Ready {
					return fmt.Sprintf("Project ready: %s (runtime: %s)", r.ProjectDir, r.Runtime)
				}
				out := fmt.Sprintf("Project NOT ready: %s\n", r.ProjectDir)
				for _, iss := range r.Issues {
					out += fmt.Sprintf("  [%s] %s → %s\n", iss.Category, iss.Message, iss.NextAction)
				}
				return out
			})
		},
	}
}

// newSummarizeCmd creates the `agent summarize` command.
func newSummarizeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "summarize <run-id>",
		Short: "Generate a summary of a completed run",
		Long: `Summarize a run's status, duration, denials, and invoke response if present.

Requires a running daemon.`,
		Example: `  agentpaas summarize run-01HXYZ...
  agentpaas summarize run-01HXYZ... --json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			runID := args[0]
			sock, err := socketPath(cmd)
			if err != nil {
				return fmt.Errorf("new summarize cmd: %w", err)
			}
			client, conn, err := ConnectToDaemon(sock)
			if err != nil {
				return fmt.Errorf("new summarize cmd: %w", err)
			}
			defer func() { _ = conn.Close() }() // best-effort close

			ctx, cancel := contextWithTimeout(15 * time.Second)
			defer cancel()

			resp, err := client.SummarizeRun(ctx, &controlv1.SummarizeRunRequest{RunId: runID})
			if err != nil {
				return fmt.Errorf("summarize failed: %w", err)
			}

			result := operator.SummarizeRunResponse{
				SchemaVersion: resp.GetSchemaVersion(),
				RunID:         runID,
				Status:        resp.GetStatus(),
				ExitCode:      int(resp.GetExitCode()),
				Summary:       resp.GetSummary(),
				Invocations:   int(resp.GetInvocations()),
				PolicyDenials: int(resp.GetPolicyDenials()),
				ErrorCategory: operator.ErrorCategory(resp.GetErrorCategory()),
			}
			if resp.GetStartedAt() != nil {
				result.StartedAt = resp.GetStartedAt().AsTime()
			}
			if resp.GetFinishedAt() != nil {
				result.FinishedAt = resp.GetFinishedAt().AsTime()
			}
			result.DurationMS = resp.GetDurationMs()

			// Read the persisted invoke response (BUG 11 fix).
			if homeDir, err := homeDirPath(cmd); err == nil {
				respPath := filepath.Join(homeDir, "state", "runs", runID, "invoke-response.json")
				if data, err := os.ReadFile(respPath); err == nil {
					result.InvokeResponse = string(data)
				}
			}

			return printTextOrJSON(jsonOutput(cmd), result, func(v interface{}) string {
				r := v.(operator.SummarizeRunResponse)
				msg := fmt.Sprintf("Run %s: %s (status: %s)", r.RunID, r.Summary, r.Status)
				if r.InvokeResponse != "" {
					msg += "\nInvoke Response:\n" + r.InvokeResponse
				}
				return msg
			})
		},
	}
}

// newExplainFailureCmd creates the `agent explain-failure` command.
func newExplainFailureCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "explain-failure <run-id>",
		Short: "Analyze a failed run and return root cause",
		Long: `Analyze a failed run and return categorized root cause with redacted excerpts.

Requires a running daemon.`,
		Example: `  agentpaas explain-failure run-01HXYZ...
  agentpaas explain-failure run-01HXYZ... --json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			runID := args[0]
			sock, err := socketPath(cmd)
			if err != nil {
				return fmt.Errorf("new explain failure cmd: %w", err)
			}
			client, conn, err := ConnectToDaemon(sock)
			if err != nil {
				return fmt.Errorf("new explain failure cmd: %w", err)
			}
			defer func() { _ = conn.Close() }() // best-effort close

			ctx, cancel := contextWithTimeout(15 * time.Second)
			defer cancel()

			resp, err := client.ExplainFailure(ctx, &controlv1.ExplainFailureRequest{RunId: runID})
			if err != nil {
				return fmt.Errorf("explain-failure failed: %w", err)
			}

			excerpts := make([]operator.RedactedExcerpt, 0, len(resp.GetRedactedExcerpts()))
			for _, ex := range resp.GetRedactedExcerpts() {
				excerpts = append(excerpts, operator.RedactedExcerpt{
					Source:    ex.GetSource(),
					StartLine: int(ex.GetStartLine()),
					EndLine:   int(ex.GetEndLine()),
					Content:   ex.GetContent(),
				})
			}
			result := operator.ExplainFailureResponse{
				SchemaVersion:    resp.GetSchemaVersion(),
				RunID:            runID,
				ErrorCategory:    operator.ErrorCategory(resp.GetErrorCategory()),
				RootCause:        resp.GetRootCause(),
				RedactedExcerpts: excerpts,
				NextAction:       operator.NextAction(resp.GetNextAction()),
			}
			return printTextOrJSON(jsonOutput(cmd), result, func(v interface{}) string {
				r := v.(operator.ExplainFailureResponse)
				if r.ErrorCategory == "" {
					return r.RootCause
				}
				return fmt.Sprintf("Run %s failed [%s]: %s → %s",
					r.RunID, r.ErrorCategory, r.RootCause, r.NextAction)
			})
		},
	}
}

// newExplainDenialCmd creates the `agent explain-denial` command.
func newExplainDenialCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "explain-denial <destination>",
		Short: "Explain why a destination was denied by policy",
		Long: `Explain a policy denial for a destination hostname or URL.

Requires a running daemon. Similar to 'agentpaas policy explain'.`,
		Example: `  agentpaas explain-denial api.openai.com
  agentpaas explain-denial https://example.com/v1`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := args[0]
			sock, err := socketPath(cmd)
			if err != nil {
				return fmt.Errorf("new explain denial cmd: %w", err)
			}
			client, conn, err := ConnectToDaemon(sock)
			if err != nil {
				return fmt.Errorf("new explain denial cmd: %w", err)
			}
			defer func() { _ = conn.Close() }() // best-effort close

			ctx, cancel := contextWithTimeout(10 * time.Second)
			defer cancel()

			resp, err := client.ExplainPolicyDenial(ctx, &controlv1.ExplainPolicyDenialRequest{
				DeniedDestination: target,
			})
			if err != nil {
				return fmt.Errorf("explain-denial failed: %w", err)
			}

			result := operator.ExplainPolicyDenialResponse{
				SchemaVersion:  resp.GetSchemaVersion(),
				RunID:          resp.GetRunId(),
				DeniedAction:   resp.GetDeniedAction(),
				BlockingRuleID: resp.GetBlockingRuleId(),
				PolicyDigest:   resp.GetPolicyDigest(),
				Rationale:      resp.GetRationale(),
				NextAction:     operator.NextAction(resp.GetNextAction()),
			}
			return printTextOrJSON(jsonOutput(cmd), result, func(v interface{}) string {
				r := v.(operator.ExplainPolicyDenialResponse)
				return fmt.Sprintf("Denied: %s\nRule: %s\nReason: %s\nNext: %s",
					r.DeniedAction, r.BlockingRuleID, r.Rationale, r.NextAction)
			})
		},
	}
}

// newRecommendPatchCmd creates the `agent recommend-patch` command.
func newRecommendPatchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "recommend-patch <desired-behavior>",
		Short: "Suggest a policy patch for a desired behavior",
		Long: `Suggest a policy patch for a desired behavior (top-level alias of policy propose).

Requires a running daemon.`,
		Example: `  agentpaas recommend-patch "allow HTTPS to api.openai.com"`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			behavior := args[0]
			sock, err := socketPath(cmd)
			if err != nil {
				return fmt.Errorf("new recommend patch cmd: %w", err)
			}
			client, conn, err := ConnectToDaemon(sock)
			if err != nil {
				return fmt.Errorf("new recommend patch cmd: %w", err)
			}
			defer func() { _ = conn.Close() }() // best-effort close

			ctx, cancel := contextWithTimeout(10 * time.Second)
			defer cancel()

			resp, err := client.RecommendPolicyPatch(ctx, &controlv1.RecommendPolicyPatchRequest{
				DesiredBehavior: behavior,
			})
			if err != nil {
				return fmt.Errorf("recommend-patch failed: %w", err)
			}

			confirmation := operator.ConfirmationRequirement{
				RequiresConfirmation: resp.GetConfirmation().GetRequiresConfirmation(),
				ConfirmationID:       resp.GetConfirmation().GetConfirmationId(),
				RiskLevel:            operator.RiskLevel(resp.GetConfirmation().GetRiskLevel()),
				Rationale:            resp.GetConfirmation().GetRationale(),
				AffectedDestinations: resp.GetConfirmation().GetAffectedDestinations(),
				CredentialIDs:        resp.GetConfirmation().GetCredentialIds(),
			}
			result := operator.RecommendPolicyPatchResponse{
				SchemaVersion:        resp.GetSchemaVersion(),
				ProposedPatch:        resp.GetProposedPatch(),
				RiskLevel:            operator.RiskLevel(resp.GetRiskLevel()),
				Rationale:            resp.GetRationale(),
				AffectedDestinations: resp.GetAffectedDestinations(),
				CredentialIDs:        resp.GetCredentialIds(),
				Confirmation:         confirmation,
				NextAction:           operator.NextAction(resp.GetNextAction()),
			}
			return printTextOrJSON(jsonOutput(cmd), result, func(v interface{}) string {
				r := v.(operator.RecommendPolicyPatchResponse)
				return fmt.Sprintf("Patch: %s\nRisk: %s\nReason: %s\nConfirm required: %v",
					r.ProposedPatch, r.RiskLevel, r.Rationale, r.Confirmation.RequiresConfirmation)
			})
		},
	}
}

// newTimelineCmd creates the `agent timeline` command.
func newTimelineCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "timeline <run-id>",
		Short: "Show chronological timeline of events for a run",
		Long:  `Show a chronological event timeline for a run from the daemon.`,
		Example: `  agentpaas timeline run-01HXYZ...
  agentpaas timeline run-01HXYZ... --json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			runID := args[0]
			sock, err := socketPath(cmd)
			if err != nil {
				return fmt.Errorf("new timeline cmd: %w", err)
			}
			client, conn, err := ConnectToDaemon(sock)
			if err != nil {
				return fmt.Errorf("new timeline cmd: %w", err)
			}
			defer func() { _ = conn.Close() }() // best-effort close

			ctx, cancel := contextWithTimeout(15 * time.Second)
			defer cancel()

			resp, err := client.GetRunTimeline(ctx, &controlv1.GetRunTimelineRequest{RunId: runID})
			if err != nil {
				return fmt.Errorf("timeline failed: %w", err)
			}

			events := make([]operator.TimelineEvent, 0, len(resp.GetEvents()))
			for _, e := range resp.GetEvents() {
				var ts time.Time
				if e.GetTimestamp() != nil {
					ts = e.GetTimestamp().AsTime()
				}
				events = append(events, operator.TimelineEvent{
					Timestamp: ts,
					EventType: e.GetType(),
					Detail:    e.GetDescription(),
					AuditSeq:  0,
				})
			}
			result := operator.GetRunTimelineResponse{
				SchemaVersion: resp.GetSchemaVersion(),
				RunID:         runID,
				Events:        events,
			}
			return printTextOrJSON(jsonOutput(cmd), result, func(v interface{}) string {
				r := v.(operator.GetRunTimelineResponse)
				out := fmt.Sprintf("Timeline for %s (%d events):\n", r.RunID, len(r.Events))
				for _, e := range r.Events {
					out += fmt.Sprintf("  %s [%s] %s\n", e.Timestamp.Format(time.RFC3339), e.EventType, e.Detail)
				}
				return out
			})
		},
	}
}

// newStatusCmd creates the `agentpaas status [run-id]` command.
// With a run-id, shows that run's status. Without, shows daemon health.
func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status [run-id]",
		Short: "Show daemon status or a specific run's status",
		Long: `With no arguments, shows daemon readiness (same as daemon status).
With a run ID, shows that run's status summary.`,
		Example: `  agentpaas status
  agentpaas status run-01HXYZ...
  agentpaas status --json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				// No run-id: delegate to daemon status
				return runDaemonStatus(cmd)
			}
			runID := args[0]
			sock, err := socketPath(cmd)
			if err != nil {
				return fmt.Errorf("new status cmd: %w", err)
			}
			client, conn, err := ConnectToDaemon(sock)
			if err != nil {
				return fmt.Errorf("new status cmd: %w", err)
			}
			defer func() { _ = conn.Close() }() // best-effort close

			ctx, cancel := contextWithTimeout(15 * time.Second)
			defer cancel()

			resp, err := client.SummarizeRun(ctx, &controlv1.SummarizeRunRequest{RunId: runID})
			if err != nil {
				return fmt.Errorf("status failed: %w", err)
			}

			result := operator.SummarizeRunResponse{
				SchemaVersion: resp.GetSchemaVersion(),
				RunID:         runID,
				Status:        resp.GetStatus(),
				Summary:       resp.GetSummary(),
			}
			return printTextOrJSON(jsonOutput(cmd), result, func(v interface{}) string {
				r := v.(operator.SummarizeRunResponse)
				return fmt.Sprintf("Run %s\n  Status:  %s\n  Summary: %s\n", r.RunID, r.Status, r.Summary)
			})
		},
	}
}

// newNextActionCmd creates the `agent next-action` command.
func newNextActionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "next-action [run-id]",
		Short: "Recommend the next action based on current context",
		Long: `Ask the daemon for the recommended next operator action.

Optional run-id scopes the recommendation to that run.`,
		Example: `  agentpaas next-action
  agentpaas next-action run-01HXYZ...
  agentpaas next-action --json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			runID := ""
			if len(args) > 0 {
				runID = args[0]
			}
			sock, err := socketPath(cmd)
			if err != nil {
				return fmt.Errorf("new next action cmd: %w", err)
			}
			client, conn, err := ConnectToDaemon(sock)
			if err != nil {
				return fmt.Errorf("new next action cmd: %w", err)
			}
			defer func() { _ = conn.Close() }() // best-effort close

			ctx, cancel := contextWithTimeout(10 * time.Second)
			defer cancel()

			resp, err := client.NextAction(ctx, &controlv1.NextActionRequest{
				Context: runID,
			})
			if err != nil {
				return fmt.Errorf("next-action failed: %w", err)
			}

			var confirmation *operator.ConfirmationRequirement
			if resp.GetConfirmation() != nil {
				c := operator.ConfirmationRequirement{
					RequiresConfirmation: resp.GetConfirmation().GetRequiresConfirmation(),
					ConfirmationID:       resp.GetConfirmation().GetConfirmationId(),
					RiskLevel:            operator.RiskLevel(resp.GetConfirmation().GetRiskLevel()),
					Rationale:            resp.GetConfirmation().GetRationale(),
					AffectedDestinations: resp.GetConfirmation().GetAffectedDestinations(),
					CredentialIDs:        resp.GetConfirmation().GetCredentialIds(),
				}
				confirmation = &c
			}
			result := operator.NextActionResponse{
				SchemaVersion: resp.GetSchemaVersion(),
				RunID:         runID,
				NextAction:    operator.NextAction(resp.GetNextAction()),
				Rationale:     resp.GetRationale(),
				Confirmation:  confirmation,
			}
			return printTextOrJSON(jsonOutput(cmd), result, func(v interface{}) string {
				r := v.(operator.NextActionResponse)
				return fmt.Sprintf("Next action: %s\nReason: %s", r.NextAction, r.Rationale)
			})
		},
	}
	return cmd
}

// resolveCLIProjectPath converts a CLI project path argument to an absolute
// path in the client's working directory before sending it to the daemon.
func resolveCLIProjectPath(projectPath string) (string, error) {
	absPath, err := filepath.Abs(projectPath)
	if err != nil {
		return "", fmt.Errorf("cannot resolve project path %q: %w", projectPath, err)
	}
	return absPath, nil
}

// hasWildcardEgress checks if policy.yaml content contains a wildcard domain
// entry (domain: "*" with allow_wildcard: true). This is a simple text scan
// rather than full YAML parsing to keep the CLI fast and avoid importing the
// policy package.
func hasWildcardEgress(data []byte) bool {
	// Look for domain: "*" pattern in the egress section
	lines := string(data)
	for _, line := range splitLines(lines) {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "domain:") && strings.Contains(trimmed, "\"*\"") {
			return true
		}
		if strings.HasPrefix(trimmed, "domain:") && strings.Contains(trimmed, "'*'") {
			return true
		}
		if strings.HasPrefix(trimmed, "domain:") && strings.TrimSpace(strings.TrimPrefix(trimmed, "domain:")) == "*" {
			return true
		}
	}
	return false
}

func splitLines(s string) []string {
	return strutil.SplitLines(s)
}

// contextWithTimeout is a helper that creates a context with the given timeout.
func contextWithTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

// ensure timestamppb import is used (for future timestamp conversion helpers)
var _ = timestamppb.New // keep import used
