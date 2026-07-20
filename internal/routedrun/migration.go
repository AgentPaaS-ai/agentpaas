package routedrun

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Migration transforms persisted state from FromVersion to ToVersion.
// Apply must be idempotent: applying twice to already-migrated bytes is a no-op
// or returns the same ToVersion bytes.
type Migration struct {
	FromVersion string
	ToVersion   string
	Apply       func(state []byte) ([]byte, error)
}

// MigrationRegistry is an ordered registry of supported schema migrations.
// Unknown or newer versions fail closed before any mutation.
type MigrationRegistry struct {
	current    string
	supported  map[string]bool // versions that may appear on disk (current + migration froms/tos)
	migrations []Migration     // ordered chain
}

// NewMigrationRegistry builds a registry for the given current schema version.
// Migrations must form a single forward chain without gaps or cycles.
func NewMigrationRegistry(current string, migrations []Migration) (*MigrationRegistry, error) {
	if current == "" {
		return nil, fmt.Errorf("routedrun: migration registry requires current version")
	}
	r := &MigrationRegistry{
		current:   current,
		supported: map[string]bool{current: true},
	}
	seenFrom := make(map[string]bool)
	for i, m := range migrations {
		if m.FromVersion == "" || m.ToVersion == "" {
			return nil, fmt.Errorf("routedrun: migration[%d] missing from/to version", i)
		}
		if m.Apply == nil {
			return nil, fmt.Errorf("routedrun: migration[%d] %s->%s has nil Apply", i, m.FromVersion, m.ToVersion)
		}
		if seenFrom[m.FromVersion] {
			return nil, fmt.Errorf("routedrun: duplicate migration from %s", m.FromVersion)
		}
		seenFrom[m.FromVersion] = true
		r.supported[m.FromVersion] = true
		r.supported[m.ToVersion] = true
		r.migrations = append(r.migrations, m)
	}
	// Validate chain continuity when migrations are present.
	if len(r.migrations) > 0 {
		// Build adjacency.
		next := make(map[string]string, len(r.migrations))
		for _, m := range r.migrations {
			next[m.FromVersion] = m.ToVersion
		}
		// Every non-current ToVersion should be a FromVersion of another step
		// or equal to current; every FromVersion should eventually reach current.
		for _, m := range r.migrations {
			if err := reachability(m.FromVersion, current, next); err != nil {
				return nil, err
			}
		}
	}
	return r, nil
}

func reachability(from, target string, next map[string]string) error {
	seen := map[string]bool{}
	cur := from
	for cur != target {
		if seen[cur] {
			return fmt.Errorf("routedrun: migration cycle involving %s", cur)
		}
		seen[cur] = true
		n, ok := next[cur]
		if !ok {
			return fmt.Errorf("routedrun: migration chain from %s does not reach %s", from, target)
		}
		cur = n
	}
	return nil
}

// Current returns the registry's current schema version.
func (r *MigrationRegistry) Current() string {
	if r == nil {
		return CurrentSchemaVersion
	}
	return r.current
}

// IsSupported reports whether version is current or an explicitly registered older version.
func (r *MigrationRegistry) IsSupported(version string) bool {
	if r == nil {
		return version == CurrentSchemaVersion
	}
	return r.supported[version]
}

// NeedsMigration reports whether version is older than current and can be migrated.
func (r *MigrationRegistry) NeedsMigration(version string) bool {
	if r == nil {
		return false
	}
	if version == r.current {
		return false
	}
	return r.IsSupported(version)
}

// Migrate applies the ordered chain from version to current (or to target if set).
// Unknown/newer versions fail closed without calling Apply.
func (r *MigrationRegistry) Migrate(fromVersion string, state []byte) (toVersion string, out []byte, err error) {
	if r == nil {
		if fromVersion != CurrentSchemaVersion {
			return "", nil, fmt.Errorf("%w: %s", ErrUnknownSchemaVersion, fromVersion)
		}
		return fromVersion, state, nil
	}
	if fromVersion == "" {
		return "", nil, fmt.Errorf("%w: empty version", ErrUnknownSchemaVersion)
	}
	if fromVersion == r.current {
		return fromVersion, state, nil
	}
	if !r.IsSupported(fromVersion) {
		return "", nil, fmt.Errorf("%w: %s", ErrUnknownSchemaVersion, fromVersion)
	}

	// Index migrations by from version.
	byFrom := make(map[string]Migration, len(r.migrations))
	for _, m := range r.migrations {
		byFrom[m.FromVersion] = m
	}

	curVer := fromVersion
	curState := state
	for curVer != r.current {
		m, ok := byFrom[curVer]
		if !ok {
			return "", nil, fmt.Errorf("routedrun: no migration step from %s toward %s", curVer, r.current)
		}
		next, err := m.Apply(curState)
		if err != nil {
			return "", nil, fmt.Errorf("routedrun: migration %s->%s: %w", m.FromVersion, m.ToVersion, err)
		}
		curState = next
		curVer = m.ToVersion
	}
	return curVer, curState, nil
}

// MigrateFile migrates a single JSON file on disk with atomic replacement and
// recoverable backup. Interruption leaves either the original or the fully
// migrated file (plus backup until commit marker is written).
//
// Layout beside path:
//
//	path.bak.<from>
//	path.migrate.tmp
//	path.migrate.commit (written after rename; then backups may be removed)
func (r *MigrationRegistry) MigrateFile(path string) error {
	if r == nil {
		return nil
	}
	if err := rejectSymlinkPath(path); err != nil {
		return err
	}
	data, err := readFileStrict(path, maxStateFileBytes)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	ver, err := extractSchemaVersion(data)
	if err != nil {
		return err
	}
	if ver == r.current {
		return nil
	}
	if !r.IsSupported(ver) {
		return fmt.Errorf("%w: %s in %s", ErrUnknownSchemaVersion, ver, path)
	}

	toVer, migrated, err := r.Migrate(ver, data)
	if err != nil {
		return err
	}
	if toVer != r.current {
		return fmt.Errorf("routedrun: migration of %s stopped at %s", path, toVer)
	}

	dir := filepath.Dir(path)
	backup := path + ".bak." + sanitizeVersionForPath(ver)
	// Retain recoverable backup until commit.
	if err := atomicWriteFile(backup, data, filePerm); err != nil {
		return fmt.Errorf("routedrun: write migration backup: %w", err)
	}
	if err := atomicWriteFile(path, migrated, filePerm); err != nil {
		// Attempt rollback from backup.
		if rb, rerr := os.ReadFile(backup); rerr == nil {
			_ = atomicWriteFile(path, rb, filePerm) // intentionally ignored (reviewed)
		}
		return fmt.Errorf("routedrun: write migrated file: %w", err)
	}
	// Commit marker: presence means migration of this file is durable.
	commitPath := path + ".migrate.commit"
	marker := []byte(fmt.Sprintf(`{"schema_version":%q,"from":%q,"to":%q}`+"\n", r.current, ver, toVer))
	if err := atomicWriteFile(commitPath, marker, filePerm); err != nil {
		return fmt.Errorf("routedrun: write migration commit: %w", err)
	}
	// Safe to drop backup and commit marker after success (retain optional; remove for cleanliness).
	_ = os.Remove(backup) // best-effort remove
	_ = os.Remove(commitPath) // best-effort remove
	_ = fsyncDir(dir) // intentionally ignored (reviewed)
	return nil
}

// MigrateTree walks a store root and migrates every JSON state file under it.
// Fails closed on the first unknown/newer version without mutating further files
// once an error is observed; already-migrated files remain migrated.
func (r *MigrationRegistry) MigrateTree(root string) error {
	if r == nil {
		return nil
	}
	if err := rejectSymlinkPath(root); err != nil {
		return err
	}
	var paths []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: %s", ErrSymlinkRejected, path)
		}
		if info.IsDir() {
			return nil
		}
		name := info.Name()
		if strings.HasPrefix(name, ".tmp-") || strings.HasSuffix(name, ".migrate.commit") {
			return nil
		}
		if strings.Contains(name, ".bak.") {
			return nil
		}
		if !strings.HasSuffix(name, ".json") && !strings.HasSuffix(name, ".jsonl") {
			return nil
		}
		// JSONL ledgers: only migrate envelope-bearing single-object files for now.
		if strings.HasSuffix(name, ".jsonl") {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return err
	}
	sort.Strings(paths)
	for _, p := range paths {
		if err := r.MigrateFile(p); err != nil {
			return err
		}
	}
	return nil
}

func extractSchemaVersion(data []byte) (string, error) {
	var probe struct {
		SchemaVersion string `json:"schema_version"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return "", fmt.Errorf("routedrun: parse schema_version: %w", err)
	}
	if probe.SchemaVersion == "" {
		return "", fmt.Errorf("routedrun: missing schema_version")
	}
	return probe.SchemaVersion, nil
}

func sanitizeVersionForPath(v string) string {
	v = strings.ReplaceAll(v, "/", "_")
	v = strings.ReplaceAll(v, "\\", "_")
	v = strings.ReplaceAll(v, "..", "_")
	return v
}

// DefaultMigrationRegistry returns a registry for CurrentSchemaVersion with no
// older migrations registered yet (v0.3.0 is the starting schema).
func DefaultMigrationRegistry() *MigrationRegistry {
	r, err := NewMigrationRegistry(CurrentSchemaVersion, nil)
	if err != nil {
		// CurrentSchemaVersion is non-empty; should never fail.
		panic(err)
	}
	return r
}
