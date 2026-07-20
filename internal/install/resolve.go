package install

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/AgentPaaS-ai/agentpaas/internal/naming"
)

// AgentRefLabel is the Docker label key for installed agent references.
const AgentRefLabel = "agentpaas.agent-ref"

// ResolveRefOpts configures shared agent reference resolution.
type ResolveRefOpts struct {
	StateRoot string
	Input     string
	// Infof prints non-fatal resolution hints (e.g. ambiguous-soon) to stderr.
	Infof func(format string, args ...any)
}

// ResolvedAgent is the outcome of ResolveAgentRef.
type ResolvedAgent struct {
	// DaemonKey is the agent identity passed to the daemon Run/Cron/Trigger APIs.
	DaemonKey string
	// Ref is the canonical installed ref (name@pub8) when installed; otherwise DaemonKey.
	Ref string
	// Display is the D7 publisher string: name@pub8 (alias) or name@pub8.
	Display string
	// Installed is true when resolution targeted materialized shared install state.
	Installed bool
}

// ResolveAgentRef resolves a user reference (name@pub8, alias, or bare name).
func ResolveAgentRef(opts ResolveRefOpts) (*ResolvedAgent, error) {
	input := strings.TrimSpace(opts.Input)
	if input == "" {
		return nil, fmt.Errorf("empty agent reference")
	}
	if err := ValidateReferenceInput(input); err != nil {
		return nil, err
	}
	if opts.StateRoot == "" {
		return nil, fmt.Errorf("install: empty StateRoot")
	}

	if strings.Contains(input, "@") {
		name, pub8, err := naming.ParseAgentRef(input)
		if err != nil {
			return nil, err
		}
		dir, err := findInstalledDirByRef(opts.StateRoot, name, pub8)
		if err != nil {
			return nil, err
		}
		if dir == "" {
			return nil, fmt.Errorf("no installed agent for reference %q", name+"@"+pub8)
		}
		m, err := loadInstalledManifest(dir)
		if err != nil {
			return nil, err
		}
		ref := name + "@" + strings.ToLower(pub8)
		return &ResolvedAgent{
			DaemonKey: ref,
			Ref:       ref,
			Display:   FormatAgentDisplay(ref, m.Alias),
			Installed: true,
		}, nil
	}

	if aliasRes, err := resolveByAlias(opts.StateRoot, input); err != nil {
		return nil, err
	} else if aliasRes != nil {
		return aliasRes, nil
	}

	if isPhase1LocalAgent(opts.StateRoot, input) {
		return &ResolvedAgent{
			DaemonKey: input,
			Ref:       input,
			Display:   input,
		}, nil
	}

	matches, err := listInstalledByAgentName(opts.StateRoot, input)
	if err != nil {
		return nil, err
	}
	switch len(matches) {
	case 0:
		return &ResolvedAgent{
			DaemonKey: input,
			Ref:       input,
			Display:   input,
		}, nil
	case 1:
		if opts.Infof != nil {
			opts.Infof("resolved bare name %q to installed agent %s (ambiguous soon: prefer name@pub8 or alias)\n",
				input, FormatAgentDisplay(matches[0].Ref, matches[0].Alias))
		}
		return &ResolvedAgent{
			DaemonKey: matches[0].Ref,
			Ref:       matches[0].Ref,
			Display:   FormatAgentDisplay(matches[0].Ref, matches[0].Alias),
			Installed: true,
		}, nil
	default:
		return nil, formatAmbiguousBareNameError(input, matches)
	}
}

func resolveByAlias(stateRoot, alias string) (*ResolvedAgent, error) {
	if err := ValidateAlias(alias); err != nil {
		return nil, err
	}
	list, err := ListInstalledAgents(stateRoot)
	if err != nil {
		return nil, err
	}
	var matches []InstalledAgentEntry
	for _, e := range list {
		if e.Alias == alias {
			matches = append(matches, e)
		}
	}
	if len(matches) == 0 {
		return nil, nil
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("ambiguous alias %q matches %d installed agents", alias, len(matches))
	}
	e := matches[0]
	return &ResolvedAgent{
		DaemonKey: e.Ref,
		Ref:       e.Ref,
		Display:   FormatAgentDisplay(e.Ref, e.Alias),
		Installed: true,
	}, nil
}

func listInstalledByAgentName(stateRoot, bareName string) ([]InstalledAgentEntry, error) {
	if err := naming.ValidateName(bareName); err != nil {
		return nil, err
	}
	list, err := ListInstalledAgents(stateRoot)
	if err != nil {
		return nil, err
	}
	var out []InstalledAgentEntry
	for _, e := range list {
		name, _, ok := ParseInstalledAgentDir(e.Ref) // intentionally ignored (reviewed)
		if !ok {
			continue
		}
		if name == bareName {
			out = append(out, e)
		}
	}
	return out, nil
}

func isPhase1LocalAgent(stateRoot, bareName string) bool {
	if !IsPhase1LocalAgentDir(bareName) {
		return false
	}
	dir := filepath.Join(stateRoot, installedAgentsDirName, bareName)
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return false
	}
	// Phase-1 pack layout includes agent.lock at the agent root.
	lockPath := filepath.Join(dir, installedLockName)
	if info, err := os.Stat(lockPath); err != nil || info.IsDir() {
		return false
	}
	return true
}

func formatAmbiguousBareNameError(bare string, matches []InstalledAgentEntry) error {
	var b strings.Builder
	for i, m := range matches {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(FormatAgentDisplay(m.Ref, m.Alias))
	}
	return fmt.Errorf("agent name %q is ambiguous; candidates: %s. Use name@pub8 or alias", bare, b.String())
}

// FormatAgentDisplay renders D7 text: name@pub8 (alias) or name@pub8.
func FormatAgentDisplay(ref, alias string) string {
	if alias == "" {
		return ref
	}
	return ref + " (" + alias + ")"
}

// DisplayForDaemonKey returns the D7 display string for a daemon agent key.
func DisplayForDaemonKey(stateRoot, daemonKey string) string {
	if _, _, ok := ParseInstalledAgentDir(daemonKey); !ok { // intentionally ignored (reviewed)
		return daemonKey
	}
	name, pub8, err := naming.ParseAgentRef(daemonKey)
	if err != nil {
		return daemonKey
	}
	dir, err := findInstalledDirByRef(stateRoot, name, pub8)
	if err != nil || dir == "" {
		return daemonKey
	}
	m, err := loadInstalledManifest(dir)
	if err != nil {
		return daemonKey
	}
	return FormatAgentDisplay(daemonKey, m.Alias)
}

func loadInstalledManifest(dir string) (InstallManifest, error) {
	raw, err := os.ReadFile(filepath.Join(dir, installedManifestName))
	if err != nil {
		return InstallManifest{}, fmt.Errorf("read install manifest: %w", err)
	}
	var m InstallManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return InstallManifest{}, err
	}
	return m, nil
}