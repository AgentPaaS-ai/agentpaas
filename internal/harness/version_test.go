package harness

import "testing"

func TestHarnessVersionDefault(t *testing.T) {
	if HarnessVersion == "" {
		t.Fatal("HarnessVersion must not be empty (default is \"dev\", stamped at build via ldflags)")
	}
}
