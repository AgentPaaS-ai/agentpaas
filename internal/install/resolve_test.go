package install

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/runtime"
	"github.com/AgentPaaS-ai/agentpaas/internal/trigger"
)

func writeInstalledManifestFixture(t *testing.T, stateRoot, agentName, pub8, alias string) string {
	t.Helper()
	ref, err := InstalledAgentRefDirName(agentName, pub8)
	if err != nil {
		t.Fatalf("ref: %v", err)
	}
	dir := filepath.Join(stateRoot, installedAgentsDirName, ref)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	m := InstallManifest{
		AgentName:            agentName,
		PublisherFingerprint: strings.ToLower(pub8),
		Alias:                alias,
		AgentVersion:         "1.0.0",
		InstalledAt:          time.Now().UTC(),
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, installedManifestName), raw, 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return ref
}

func writePhase1LocalAgent(t *testing.T, stateRoot, name string) {
	t.Helper()
	dir := filepath.Join(stateRoot, installedAgentsDirName, name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, installedLockName), []byte("{}"), 0o600); err != nil {
		t.Fatalf("write lock: %v", err)
	}
}

func TestResolveAgentRef_ExactRef(t *testing.T) {
	state := t.TempDir()
	ref := writeInstalledManifestFixture(t, state, "weather", "a1b2c3d4", "maria")
	got, err := ResolveAgentRef(ResolveRefOpts{StateRoot: state, Input: ref})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got.DaemonKey != ref || !got.Installed {
		t.Fatalf("got %+v want ref=%s installed", got, ref)
	}
	if got.Display != "weather@a1b2c3d4 (maria)" {
		t.Fatalf("display=%q", got.Display)
	}
}

func TestResolveAgentRef_Alias(t *testing.T) {
	state := t.TempDir()
	ref := writeInstalledManifestFixture(t, state, "weather", "a1b2c3d4", "maria")
	got, err := ResolveAgentRef(ResolveRefOpts{StateRoot: state, Input: "maria"})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got.DaemonKey != ref {
		t.Fatalf("daemon key=%q want %q", got.DaemonKey, ref)
	}
}

func TestResolveAgentRef_BareNameSingleInstalled(t *testing.T) {
	state := t.TempDir()
	ref := writeInstalledManifestFixture(t, state, "weather", "a1b2c3d4", "")
	var info strings.Builder
	got, err := ResolveAgentRef(ResolveRefOpts{
		StateRoot: state,
		Input:     "weather",
		Infof: func(format string, args ...any) { _, _ = fmt.Fprintf(&info, format, args...) },
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got.DaemonKey != ref {
		t.Fatalf("key=%q", got.DaemonKey)
	}
	if !strings.Contains(info.String(), "ambiguous soon") {
		t.Fatalf("expected ambiguous soon info, got %q", info.String())
	}
}

func TestResolveAgentRef_BareNameAmbiguous(t *testing.T) {
	state := t.TempDir()
	writeInstalledManifestFixture(t, state, "weather", "a1b2c3d4", "maria")
	writeInstalledManifestFixture(t, state, "weather", "e5f6a7b8", "alex")
	_, err := ResolveAgentRef(ResolveRefOpts{StateRoot: state, Input: "weather"})
	if err == nil {
		t.Fatal("expected ambiguity error")
	}
	if !strings.Contains(err.Error(), "candidates:") {
		t.Fatalf("err=%v", err)
	}
}

func TestResolveAgentRef_Phase1LocalBareName(t *testing.T) {
	state := t.TempDir()
	writePhase1LocalAgent(t, state, "myagent")
	got, err := ResolveAgentRef(ResolveRefOpts{StateRoot: state, Input: "myagent"})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got.DaemonKey != "myagent" || got.Installed {
		t.Fatalf("got %+v", got)
	}
}

func TestResolveAgentRef_BareNameNoInstallPassthrough(t *testing.T) {
	state := t.TempDir()
	got, err := ResolveAgentRef(ResolveRefOpts{StateRoot: state, Input: "missing"})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got.DaemonKey != "missing" {
		t.Fatalf("got %q", got.DaemonKey)
	}
}

func TestCheckAliasUnique_RejectsCollision(t *testing.T) {
	state := t.TempDir()
	ref1 := writeInstalledManifestFixture(t, state, "weather", "a1b2c3d4", "maria")
	_ = ref1
	if err := CheckAliasUnique(state, "maria", "weather@e5f6a7b8"); err == nil {
		t.Fatal("expected alias collision")
	}
}

func TestSetInstalledAlias_UpdatesManifest(t *testing.T) {
	state := t.TempDir()
	ref := writeInstalledManifestFixture(t, state, "weather", "a1b2c3d4", "")
	if err := SetInstalledAlias(state, ref, "maria", nil); err != nil {
		t.Fatalf("set alias: %v", err)
	}
	got, err := ResolveAgentRef(ResolveRefOpts{StateRoot: state, Input: "maria"})
	if err != nil || got.DaemonKey != ref {
		t.Fatalf("resolve by alias: %v %+v", err, got)
	}
}

func TestCronPersistFullRefAfterReload(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "cron-state.json")
	cs1 := trigger.NewCronScheduler(trigger.CronConfig{StatePath: statePath})
	ctx := t.Context()
	fullRef := "weather@a1b2c3d4"
	id, err := cs1.AddSchedule(ctx, &trigger.CronSchedule{Expr: "*/5 * * * *", AgentName: fullRef})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	cs2 := trigger.NewCronScheduler(trigger.CronConfig{StatePath: statePath})
	schedules := cs2.ListSchedules()
	if len(schedules) != 1 || schedules[0].AgentName != fullRef {
		t.Fatalf("schedules=%+v", schedules)
	}
	if schedules[0].ScheduleID != id {
		t.Fatalf("id mismatch")
	}
}

func TestLabelsWithAgentRef(t *testing.T) {
	labels := runtime.LabelsWithAgentRef(runtime.ResourceTypeAgent, "run-1", "weather@a1b2c3d4")
	if labels[runtime.LabelAgentRef] != "weather@a1b2c3d4" {
		t.Fatalf("labels=%v", labels)
	}
}