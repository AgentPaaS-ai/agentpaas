package install

import (
	"strings"
	"testing"
)

func TestResolveAgentRef_Adversary_AmbiguousDoesNotPickOne(t *testing.T) {
	state := t.TempDir()
	writeInstalledManifestFixture(t, state, "weather", "a1b2c3d4", "")
	writeInstalledManifestFixture(t, state, "weather", "bbbbbbbb", "")
	_, err := ResolveAgentRef(ResolveRefOpts{StateRoot: state, Input: "weather"})
	if err == nil || !strings.Contains(err.Error(), "candidates:") {
		t.Fatalf("want candidate list error, got %v", err)
	}
}

func TestResolveAgentRef_Adversary_AliasCollisionInstall(t *testing.T) {
	state := t.TempDir()
	writeInstalledManifestFixture(t, state, "weather", "a1b2c3d4", "maria")
	if err := CheckAliasUnique(state, "maria", "weather@bbbbbbbb"); err == nil {
		t.Fatal("expected collision")
	}
}

func TestResolveAgentRef_Adversary_WrongPub8NoFallback(t *testing.T) {
	state := t.TempDir()
	writeInstalledManifestFixture(t, state, "weather", "a1b2c3d4", "")
	_, err := ResolveAgentRef(ResolveRefOpts{StateRoot: state, Input: "weather@deadbeef"})
	if err == nil || !strings.Contains(err.Error(), "no installed agent") {
		t.Fatalf("want not installed error, got %v", err)
	}
}

func TestValidateReferenceInput_RejectsTraversal(t *testing.T) {
	if err := ValidateReferenceInput("../weather"); err == nil {
		t.Fatal("expected rejection")
	}
	if err := ValidateAlias("maria;rm -rf"); err == nil {
		t.Fatal("expected rejection")
	}
}

func TestResolveAgentRef_Adversary_CronFiresDistinctRefs(t *testing.T) {
	state := t.TempDir()
	refA := writeInstalledManifestFixture(t, state, "weather", "a1b2c3d4", "maria")
	refB := writeInstalledManifestFixture(t, state, "weather", "bbbbbbbb", "alex")
	resA, err := ResolveAgentRef(ResolveRefOpts{StateRoot: state, Input: refA})
	if err != nil || resA.DaemonKey != refA {
		t.Fatalf("A: %v %+v", err, resA)
	}
	resB, err := ResolveAgentRef(ResolveRefOpts{StateRoot: state, Input: refB})
	if err != nil || resB.DaemonKey != refB {
		t.Fatalf("B: %v %+v", err, resB)
	}
}