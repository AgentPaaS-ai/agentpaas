package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
	"github.com/spf13/cobra"
)

// newInitCmd creates the `agent init` command.
// Usage: agent init [project-dir] [--runtime python|langgraph|crewai]
// If project-dir is omitted, uses current directory.
// If --runtime is omitted, auto-detects from existing files or defaults to python.
func newInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init [project-dir]",
		Short: "Initialize a new agent project",
		Long: `Scaffold a new agent project directory with agent.yaml and runtime stubs.

If project-dir is omitted, uses the current directory. When --runtime is
omitted, auto-detects from existing files or defaults to python.

Use --noninteractive to skip prompts and write a default-deny policy.yaml.
Use --from-code with --noninteractive to reconcile agent.yaml from source.`,
		Example: `  agentpaas init ./my-agent
  agentpaas init ./my-agent --runtime python --noninteractive
  agentpaas init . --from-code --noninteractive
  agentpaas init ./my-agent --runtime langgraph`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			projectDir := "."
			if len(args) > 0 {
				projectDir = args[0]
			}

			fromCode, _ := cmd.Flags().GetBool("from-code")            // cobra flag default on missing
			noninteractive, _ := cmd.Flags().GetBool("noninteractive") // cobra flag default on missing
			if fromCode && !noninteractive {
				return errors.New("--from-code requires --noninteractive in P1")
			}
			if fromCode || noninteractive {
				resolvedDir, err := validateInitProjectPath(projectDir)
				if err != nil {
					return fmt.Errorf("new init cmd: %w", err)
				}
				projectDir = resolvedDir
			}

			runtimeFlag, _ := cmd.Flags().GetString("runtime") // cobra flag default on missing
			runtime, err := initRuntime(projectDir, runtimeFlag)
			if err != nil {
				return fmt.Errorf("new init cmd: %w", err)
			}

			if fromCode {
				if err := pack.InitFromCode(projectDir, runtime); err != nil {
					return fmt.Errorf("new init cmd: %w", err)
				}
			} else {
				if err := pack.InitScaffold(projectDir, runtime); err != nil {
					return fmt.Errorf("new init cmd: %w", err)
				}
			}
			if noninteractive {
				if err := pack.InitPolicy(projectDir); err != nil {
					return fmt.Errorf("new init cmd: %w", err)
				}
			}

			jsonOutput, _ := cmd.Root().PersistentFlags().GetBool("json") // cobra flag default on missing
			if jsonOutput {
				result := struct {
					ProjectDir string           `json:"project_dir"`
					Runtime    pack.RuntimeType `json:"runtime"`
				}{
					ProjectDir: projectDir,
					Runtime:    runtime,
				}
				encoder := json.NewEncoder(cmd.OutOrStdout())
				return encoder.Encode(result)
			}

			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Initialized agent project in %s with runtime %s\n", projectDir, runtime)
			return err
		},
	}
	cmd.Flags().String("runtime", "", "Agent runtime: python, langgraph, or crewai (default: auto-detect from project files, else python)")
	cmd.Flags().Bool("from-code", false, "Reconcile agent.yaml from existing source files (requires --noninteractive)")
	cmd.Flags().Bool("noninteractive", false, "Skip prompts and write a default-deny policy.yaml")

	return cmd
}

func validateInitProjectPath(projectDir string) (string, error) {
	if strings.ContainsRune(projectDir, 0) {
		return "", errors.New("project path contains null byte")
	}
	absProjectDir, err := filepath.Abs(projectDir)
	if err != nil {
		return "", fmt.Errorf("resolve project directory: %w", err)
	}
	if strings.ContainsRune(absProjectDir, 0) {
		return "", errors.New("project path contains null byte")
	}
	if err := rejectInitSymlinkPath(absProjectDir); err != nil {
		return "", fmt.Errorf("validate init project path: %w", err)
	}
	return absProjectDir, nil
}

func rejectInitSymlinkPath(absPath string) error {
	current := absPath
	for {
		parent := filepath.Dir(current)
		info, err := os.Lstat(current)
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 && filepath.Dir(parent) != parent {
				return fmt.Errorf("path component %s is a symlink (potential escape)", current)
			}
		} else if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("inspect path component %s: %w", current, err)
		}

		if parent == current {
			return nil
		}
		current = parent
	}
}

func initRuntime(projectDir string, runtimeFlag string) (pack.RuntimeType, error) {
	if runtimeFlag != "" {
		runtime := cliRuntime(runtimeFlag)
		if runtime == pack.RuntimeUnknown {
			return pack.RuntimeUnknown, fmt.Errorf("unsupported runtime: %s", runtimeFlag)
		}

		return runtime, nil
	}

	result, err := pack.DetectProject(projectDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return pack.RuntimePython, nil
		}

		return pack.RuntimeUnknown, fmt.Errorf("init runtime: %w", err)
	}
	if result.Runtime == pack.RuntimeUnknown {
		return pack.RuntimePython, nil
	}

	return result.Runtime, nil
}

func cliRuntime(s string) pack.RuntimeType {
	switch s {
	case "python", "python3.11", "python3.12":
		return pack.RuntimePython
	case "langgraph":
		return pack.RuntimeLangGraph
	case "crewai":
		return pack.RuntimeCrewAI
	default:
		return pack.RuntimeUnknown
	}
}
