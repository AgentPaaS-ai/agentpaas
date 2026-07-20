package hashutil

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestSHA256Hex(t *testing.T) {
	t.Parallel()
	// empty
	empty := SHA256Hex(nil)
	wantEmpty := hex.EncodeToString(sha256.New().Sum(nil))
	if empty != wantEmpty {
		t.Fatalf("empty digest = %q, want %q", empty, wantEmpty)
	}
	if empty != SHA256Hex([]byte{}) {
		t.Fatal("nil and empty slice must match")
	}

	got := SHA256Hex([]byte("hello"))
	sum := sha256.Sum256([]byte("hello"))
	want := hex.EncodeToString(sum[:])
	if got != want {
		t.Fatalf("SHA256Hex(hello) = %q, want %q", got, want)
	}
	if got != strings.ToLower(got) {
		t.Fatal("digest must be lowercase hex")
	}
	if len(got) != 64 {
		t.Fatalf("len = %d, want 64", len(got))
	}
}

func TestSHA256HexString(t *testing.T) {
	t.Parallel()
	if SHA256HexString("hello") != SHA256Hex([]byte("hello")) {
		t.Fatal("SHA256HexString must match SHA256Hex of bytes")
	}
	if SHA256HexString("") != SHA256Hex(nil) {
		t.Fatal("empty string digest mismatch")
	}
}

func TestSHA256Hex_Deterministic(t *testing.T) {
	t.Parallel()
	data := []byte("agentpaas-hash-stability")
	a := SHA256Hex(data)
	b := SHA256Hex(data)
	if a != b {
		t.Fatalf("not deterministic: %q vs %q", a, b)
	}
	// mutation of input after call must not affect prior result (hash is copy)
	data[0] = 'X'
	if SHA256Hex([]byte("agentpaas-hash-stability")) != a {
		t.Fatal("digest changed after unrelated mutation of different slice")
	}
}
