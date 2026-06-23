package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/parvezsyed/agentpaas/internal/pack"
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
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			projectDir := "."
			if len(args) > 0 {
				projectDir = args[0]
			}

			fromCode, _ := cmd.Flags().GetBool("from-code")
			noninteractive, _ := cmd.Flags().GetBool("noninteractive")
			if fromCode && !noninteractive {
				return errors.New("--from-code requires --noninteractive in P1")
			}
			if fromCode || noninteractive {
				resolvedDir, err := validateInitProjectPath(projectDir)
				if err != nil {
					return err
				}
				projectDir = resolvedDir
			}

			runtimeFlag, _ := cmd.Flags().GetString("runtime")
			runtime, err := initRuntime(projectDir, runtimeFlag)
			if err != nil {
				return err
			}

			if fromCode {
				if err := pack.InitFromCode(projectDir, runtime); err != nil {
					return err
				}
			} else {
				if err := pack.InitScaffold(projectDir, runtime); err != nil {
					return err
				}
			}
			if noninteractive {
				if err := pack.InitPolicy(projectDir); err != nil {
					return err
				}
			}

			jsonOutput, _ := cmd.Root().PersistentFlags().GetBool("json")
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
	cmd.Flags().String("runtime", "", "Agent runtime: python, langgraph, or crewai (default: auto-detect or python)")
	cmd.Flags().Bool("from-code", false, "Reconcile agent.yaml from existing source files")
	cmd.Flags().Bool("noninteractive", false, "Initialize without prompts using a default-deny policy")

	return cmd
}

func validateInitProjectPath(projectDir string) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get current directory: %w", err)
	}
	absProjectDir, err := filepath.Abs(projectDir)
	if err != nil {
		return "", fmt.Errorf("resolve project directory: %w", err)
	}
	rel, err := filepath.Rel(cwd, absProjectDir)
	if err != nil {
		return "", fmt.Errorf("resolve project path relative to current directory: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("project path must be under current directory")
	}

	return absProjectDir, nil
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

		return pack.RuntimeUnknown, err
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
