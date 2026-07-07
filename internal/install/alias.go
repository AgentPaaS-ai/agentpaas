package install

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
	"github.com/AgentPaaS-ai/agentpaas/internal/naming"
)

// ValidateReferenceInput rejects hostile reference strings before resolution.
func ValidateReferenceInput(s string) error {
	if strings.ContainsAny(s, "\x00\n\r") {
		return fmt.Errorf("invalid agent reference: control characters")
	}
	if strings.Contains(s, "..") || strings.ContainsAny(s, `/\`) {
		return fmt.Errorf("invalid agent reference: path separators")
	}
	return nil
}

// ValidateAlias checks install alias strings (publisher display names).
func ValidateAlias(alias string) error {
	if err := ValidateReferenceInput(alias); err != nil {
		return err
	}
	if strings.ContainsAny(alias, ";|&$`<>") {
		return fmt.Errorf("invalid alias: shell metacharacters")
	}
	for _, r := range alias {
		if r > unicode.MaxASCII {
			return fmt.Errorf("invalid alias: non-ascii character")
		}
	}
	return naming.ValidateName(alias)
}

// CheckAliasUnique ensures no other installed agent uses alias (excluding excludeRef).
func CheckAliasUnique(stateRoot, alias, excludeRef string) error {
	if alias == "" {
		return nil
	}
	if err := ValidateAlias(alias); err != nil {
		return err
	}
	list, err := ListInstalledAgents(stateRoot)
	if err != nil {
		return err
	}
	for _, e := range list {
		if e.Alias != alias {
			continue
		}
		if excludeRef != "" && e.Ref == excludeRef {
			continue
		}
		return fmt.Errorf("alias %q already used by installed agent %s", alias, e.Ref)
	}
	return nil
}

// SetInstalledAlias updates manifest alias for ref (name@pub8 or alias), with uniqueness check.
func SetInstalledAlias(stateRoot, ref, newAlias string, emitAudit func(eventType string, payload map[string]string)) error {
	newAlias = strings.TrimSpace(newAlias)
	if newAlias != "" {
		if err := ValidateAlias(newAlias); err != nil {
			return err
		}
	}
	resolvedRef, dir, err := resolveInstalledRef(stateRoot, ref)
	if err != nil {
		return err
	}
	if dir == "" {
		return fmt.Errorf("no installed agent for reference %q", ref)
	}
	if err := CheckAliasUnique(stateRoot, newAlias, resolvedRef); err != nil {
		return err
	}
	manifestPath := filepath.Join(dir, installedManifestName)
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("read install manifest: %w", err)
	}
	var m InstallManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return fmt.Errorf("parse install manifest: %w", err)
	}
	old := m.Alias
	m.Alias = newAlias
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(manifestPath, out, 0o600); err != nil {
		return err
	}
	if emitAudit != nil {
		emitAudit(audit.EventTypeInstallAliasChanged, map[string]string{
			"agent_ref": resolvedRef,
			"old_alias": old,
			"new_alias": newAlias,
		})
	}
	return nil
}