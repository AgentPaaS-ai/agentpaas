package bundle

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func gzipTarBundle(t *testing.T, entries []tarEntry) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "malicious.agentpaas")
	writeBundleTarFile(t, path, entries)
	return path
}

func minimalValidMetadataEntries(t *testing.T) (manifestJSON, lockJSON, policyYAML, sbomJSON []byte) {
	t.Helper()
	fix := writeTestBundle(t, false)
	for _, e := range readBundleTar(t, fix.Path) {
		switch e.name {
		case ManifestPath:
			manifestJSON = append([]byte(nil), e.body...)
		case AgentLockPath:
			lockJSON = append([]byte(nil), e.body...)
		case PolicyPath:
			policyYAML = append([]byte(nil), e.body...)
		case SBOMPath:
			sbomJSON = append([]byte(nil), e.body...)
		}
	}
	return
}

func TestZipSlipOpenRejects(t *testing.T) {
	manifestJSON, lockJSON, policyYAML, sbomJSON := minimalValidMetadataEntries(t)
	mtime := testSourceDateEpoch()

	base := []tarEntry{
		{name: ManifestPath, hdr: regHeader(ManifestPath, int64(len(manifestJSON)), mtime), body: manifestJSON},
		{name: AgentLockPath, hdr: regHeader(AgentLockPath, int64(len(lockJSON)), mtime), body: lockJSON},
		{name: PolicyPath, hdr: regHeader(PolicyPath, int64(len(policyYAML)), mtime), body: policyYAML},
		{name: SBOMPath, hdr: regHeader(SBOMPath, int64(len(sbomJSON)), mtime), body: sbomJSON},
		{name: SourcePrefix, hdr: dirHeader(SourcePrefix, mtime), body: nil},
	}

	cases := []struct {
		name    string
		entries []tarEntry
		wantErr string
	}{
		{
			name: "parent path",
			entries: append(append([]tarEntry{}, base...), tarEntry{
				name: "../x",
				hdr:  regHeader("../x", 1, mtime),
				body: []byte("x"),
			}),
			wantErr: "path must not contain '..'",
		},
		{
			name: "absolute path",
			entries: append(append([]tarEntry{}, base...), tarEntry{
				name: "/etc/x",
				hdr:  regHeader("/etc/x", 1, mtime),
				body: []byte("x"),
			}),
			wantErr: "absolute path",
		},
		{
			name: "symlink",
			entries: append(append([]tarEntry{}, base...), tarEntry{
				name: "source/link",
				hdr: &tar.Header{
					Name:     "source/link",
					Typeflag: tar.TypeSymlink,
					Linkname: "/etc/passwd",
					Format:   tar.FormatUSTAR,
					ModTime:  mtime,
				},
			}),
			wantErr: "symlinks are not allowed",
		},
		{
			name: "hardlink",
			entries: append(append([]tarEntry{}, base...), tarEntry{
				name: "source/hlink",
				hdr: &tar.Header{
					Name:     "source/hlink",
					Typeflag: tar.TypeLink,
					Linkname: "source/main.py",
					Format:   tar.FormatUSTAR,
					ModTime:  mtime,
				},
			}),
			wantErr: "hardlinks are not allowed",
		},
		{
			name: "duplicate entry",
			entries: func() []tarEntry {
				dup := append([]tarEntry{}, base...)
				dup = append(dup, tarEntry{name: "source/a.txt", hdr: regHeader("source/a.txt", 1, mtime), body: []byte("a")})
				dup = append(dup, tarEntry{name: "source/a.txt", hdr: regHeader("source/a.txt", 1, mtime), body: []byte("b")})
				return dup
			}(),
			wantErr: "duplicate path",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := gzipTarBundle(t, tc.entries)
			_, err := Open(path)
			if err == nil {
				t.Fatal("expected Open error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestZipSlipMaxEntries(t *testing.T) {
	manifestJSON, lockJSON, policyYAML, sbomJSON := minimalValidMetadataEntries(t)
	mtime := testSourceDateEpoch()
	entries := []tarEntry{
		{name: ManifestPath, hdr: regHeader(ManifestPath, int64(len(manifestJSON)), mtime), body: manifestJSON},
		{name: AgentLockPath, hdr: regHeader(AgentLockPath, int64(len(lockJSON)), mtime), body: lockJSON},
		{name: PolicyPath, hdr: regHeader(PolicyPath, int64(len(policyYAML)), mtime), body: policyYAML},
		{name: SBOMPath, hdr: regHeader(SBOMPath, int64(len(sbomJSON)), mtime), body: sbomJSON},
		{name: SourcePrefix, hdr: dirHeader(SourcePrefix, mtime)},
	}
	for i := 0; i <= MaxEntries; i++ {
		name := "source/f-" + strings.Repeat("a", 4) + "-" + itoa(i) + ".txt"
		entries = append(entries, tarEntry{name: name, hdr: regHeader(name, 1, mtime), body: []byte("x")})
	}
	path := gzipTarBundle(t, entries)
	_, err := Open(path)
	if err == nil {
		t.Fatal("expected cap error")
	}
	var capErr *ErrCapExceeded
	if !asCapExceeded(err, &capErr) || capErr.Cap != "entries" {
		t.Fatalf("want entries cap error, got %v", err)
	}
}

func TestZipSlipExpansionBomb(t *testing.T) {
	manifestJSON, lockJSON, policyYAML, sbomJSON := minimalValidMetadataEntries(t)
	mtime := testSourceDateEpoch()
	bombSize := int64(3 * 1024 * 1024 * 1024)
	entries := []tarEntry{
		{name: ManifestPath, hdr: regHeader(ManifestPath, int64(len(manifestJSON)), mtime), body: manifestJSON},
		{name: AgentLockPath, hdr: regHeader(AgentLockPath, int64(len(lockJSON)), mtime), body: lockJSON},
		{name: PolicyPath, hdr: regHeader(PolicyPath, int64(len(policyYAML)), mtime), body: policyYAML},
		{name: SBOMPath, hdr: regHeader(SBOMPath, int64(len(sbomJSON)), mtime), body: sbomJSON},
		{name: SourcePrefix, hdr: dirHeader(SourcePrefix, mtime)},
		{name: "source/bomb.bin", hdr: regHeader("source/bomb.bin", 1, mtime), body: []byte{0}},
	}
	path := writeBundleWithPatchedTarSize(t, entries, "source/bomb.bin", bombSize)
	_, err := Open(path)
	if err == nil {
		t.Fatal("expected cap error for expansion bomb")
	}
	var capErr *ErrCapExceeded
	if !asCapExceeded(err, &capErr) {
		t.Fatalf("want ErrCapExceeded, got %v", err)
	}
	if capErr.Cap != "total uncompressed" && capErr.Cap != "single file" {
		t.Fatalf("unexpected cap %q", capErr.Cap)
	}
}

func writeBundleWithPatchedTarSize(t *testing.T, baseEntries []tarEntry, entryName string, fakeSize int64) string {
	t.Helper()
	tarPath := filepath.Join(t.TempDir(), "inner.tar")
	writeRawTarFile(t, tarPath, baseEntries)
	tarBytes, err := os.ReadFile(tarPath)
	if err != nil {
		t.Fatalf("read tar: %v", err)
	}
	patched, err := patchTarEntrySize(tarBytes, entryName, fakeSize)
	if err != nil {
		t.Fatalf("patch tar: %v", err)
	}
	outPath := filepath.Join(t.TempDir(), "bomb.agentpaas")
	var gzBuf bytes.Buffer
	if err := writeDeterministicGzip(&gzBuf, patched); err != nil {
		t.Fatalf("gzip: %v", err)
	}
	if err := os.WriteFile(outPath, gzBuf.Bytes(), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return outPath
}

func patchTarEntrySize(tarData []byte, entryName string, fakeSize int64) ([]byte, error) {
	if len(tarData)%512 != 0 {
		return nil, fmt.Errorf("invalid tar length")
	}
	sizeField := fmt.Sprintf("%011o", fakeSize)
	out := append([]byte(nil), tarData...)
	for offset := 0; offset+512 <= len(out); offset += 512 {
		name := strings.TrimRight(string(out[offset:offset+100]), "\x00")
		if name != entryName {
			continue
		}
		copy(out[offset+124:offset+124+11], sizeField)
		copy(out[offset+148:offset+156], "        ")
		var sum uint
		for i := 0; i < 512; i++ {
			b := out[offset+i]
			if i >= 148 && i < 156 {
				b = ' '
			}
			sum += uint(b)
		}
		copy(out[offset+148:offset+156], fmt.Sprintf("%06o\x00 ", int(sum)))
		return out, nil
	}
	return nil, fmt.Errorf("entry %q not found", entryName)
}

func regHeader(name string, size int64, mtime time.Time) *tar.Header {
	return &tar.Header{
		Name:     name,
		Mode:     0o644,
		Size:     size,
		Typeflag: tar.TypeReg,
		Format:   tar.FormatUSTAR,
		ModTime:  mtime,
	}
}

func dirHeader(name string, mtime time.Time) *tar.Header {
	return &tar.Header{
		Name:     name,
		Mode:     0o755,
		Typeflag: tar.TypeDir,
		Format:   tar.FormatUSTAR,
		ModTime:  mtime,
	}
}

func itoa(i int) string {
	return strconv.Itoa(i)
}

func asCapExceeded(err error, target **ErrCapExceeded) bool {
	if err == nil {
		return false
	}
	if e, ok := err.(*ErrCapExceeded); ok {
		*target = e
		return true
	}
	return false
}

// Ensure malicious gzip still parses (sanity for custom writer).
func TestRawGzipTarWriter(t *testing.T) {
	var buf bytes.Buffer
	gw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	gw.Header = gzip.Header{ModTime: time.Time{}, OS: 0xff}
	tw := tar.NewWriter(gw)
	hdr := &tar.Header{Name: "manifest.json", Size: 2, Mode: 0o644, Typeflag: tar.TypeReg}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("header: %v", err)
	}
	if _, err := tw.Write([]byte("{}")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	path := filepath.Join(t.TempDir(), "tiny.agentpaas")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	_, err = Open(path)
	if err == nil {
		t.Fatal("expected missing metadata error")
	}
	_ = io.Discard
}