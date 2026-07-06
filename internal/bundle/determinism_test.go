package bundle

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteDeterministicGolden(t *testing.T) {
	fix := writeTestBundle(t, false)

	var buf1, buf2 bytes.Buffer
	if _, err := Write(fix.Config, &buf1); err != nil {
		t.Fatalf("Write first: %v", err)
	}
	if _, err := Write(fix.Config, &buf2); err != nil {
		t.Fatalf("Write second: %v", err)
	}
	if !bytes.Equal(buf1.Bytes(), buf2.Bytes()) {
		t.Fatalf("two Write calls differ (%d vs %d bytes)", len(buf1.Bytes()), len(buf2.Bytes()))
	}

	pathA := filepath.Join(t.TempDir(), "a.agentpaas")
	pathB := filepath.Join(t.TempDir(), "b.agentpaas")
	if _, err := WriteToFile(fix.Config, pathA); err != nil {
		t.Fatalf("WriteToFile A: %v", err)
	}
	if _, err := WriteToFile(fix.Config, pathB); err != nil {
		t.Fatalf("WriteToFile B: %v", err)
	}
	dataA, _ := os.ReadFile(pathA)
	dataB, _ := os.ReadFile(pathB)
	if !bytes.Equal(dataA, dataB) {
		t.Fatalf("two WriteToFile calls differ")
	}
	if !bytes.Equal(buf1.Bytes(), dataA) {
		t.Fatalf("Write and WriteToFile differ (%d vs %d bytes)", len(buf1.Bytes()), len(dataA))
	}

	digest := sha256Hex(buf1.Bytes())
	if goldenBundleDigest != "" && digest != goldenBundleDigest {
		t.Fatalf("bundle digest = %s, want pinned golden %s", digest, goldenBundleDigest)
	}
	t.Logf("bundle digest: %s", digest)
}