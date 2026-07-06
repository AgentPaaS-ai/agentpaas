package bundle

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"time"
)

// TamperManifestCreatedAt copies src to dst after advancing manifest created_at by
// one second, invalidating the manifest signature. For integration tests only.
func TamperManifestCreatedAt(src, dst string) error {
	entries, err := readBundleTarEntries(src)
	if err != nil {
		return err
	}
	found := false
	for i := range entries {
		if entries[i].name != ManifestPath {
			continue
		}
		var m Manifest
		if err := json.Unmarshal(entries[i].body, &m); err != nil {
			return fmt.Errorf("unmarshal manifest: %w", err)
		}
		m.CreatedAt = m.CreatedAt.Add(time.Second)
		out, err := json.Marshal(&m)
		if err != nil {
			return fmt.Errorf("marshal manifest: %w", err)
		}
		entries[i].body = out
		entries[i].hdr.Size = int64(len(out))
		found = true
		break
	}
	if !found {
		return fmt.Errorf("manifest entry not found")
	}
	return writeBundleTarFilePath(dst, entries)
}

type tarEntry struct {
	name string
	hdr  *tar.Header
	body []byte
}

func readBundleTarEntries(path string) ([]tarEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read bundle: %w", err)
	}
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("gzip: %w", err)
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	var entries []tarEntry
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("tar next: %w", err)
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("read entry: %w", err)
		}
		entries = append(entries, tarEntry{name: hdr.Name, hdr: hdr, body: body})
	}
	return entries, nil
}

func writeBundleTarFilePath(path string, entries []tarEntry) error {
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })
	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)
	for _, e := range entries {
		hdr := *e.hdr
		hdr.Name = e.name
		if err := tw.WriteHeader(&hdr); err != nil {
			return fmt.Errorf("header: %w", err)
		}
		if len(e.body) > 0 {
			if _, err := tw.Write(e.body); err != nil {
				return fmt.Errorf("body: %w", err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		return fmt.Errorf("close tar: %w", err)
	}
	var gzBuf bytes.Buffer
	if err := writeDeterministicGzip(&gzBuf, tarBuf.Bytes()); err != nil {
		return fmt.Errorf("gzip: %w", err)
	}
	if err := os.WriteFile(path, gzBuf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write file: %w", err)
	}
	return nil
}