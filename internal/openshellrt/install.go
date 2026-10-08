package openshellrt

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type asset struct {
	name   string
	sha256 string
	binary string
}

func pinnedAssets() (cli, gateway asset, err error) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		return asset{}, asset{}, fmt.Errorf("openshell %s is pinned for darwin/arm64 (this host is %s/%s)", PinVersion, runtime.GOOS, runtime.GOARCH)
	}
	base := "https://github.com/NVIDIA/OpenShell/releases/download/" + PinVersion + "/"
	cli = asset{
		name:   base + "openshell-aarch64-apple-darwin.tar.gz",
		sha256: "b5713ffcfc7e974404b914c7566da1a128bbafe3f7afd46c3fbf442239c644b4",
		binary: "openshell",
	}
	gateway = asset{
		name:   base + "openshell-gateway-aarch64-apple-darwin.tar.gz",
		sha256: "00672235e3778c5d868d011502e612e90802a274056fb15c20b5e41ad4b4115f",
		binary: "openshell-gateway",
	}
	return cli, gateway, nil
}

// EnsureInstalled downloads the pinned official N-1 binaries into dir when
// they are missing or the wrong version. It verifies the release checksum
// before extracting. It does not fork OpenShell.
func EnsureInstalled(dir string) error {
	cli, gateway, err := pinnedAssets()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	if err := ensureOne(dir, cli); err != nil {
		return err
	}
	if err := ensureOne(dir, gateway); err != nil {
		return err
	}
	return nil
}

func ensureOne(dir string, a asset) error {
	dest := filepath.Join(dir, a.binary)
	if st, err := os.Stat(dest); err == nil && !st.IsDir() && st.Size() > 0 {
		if ver, err := binaryVersion(dest); err == nil && strings.Contains(ver, strings.TrimPrefix(PinVersion, "v")) {
			return nil
		}
	}
	tmp, err := os.CreateTemp(dir, a.binary+".tar.gz.*")
	if err != nil {
		return fmt.Errorf("temp download: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(a.name)
	if err != nil {
		_ = tmp.Close()
		return fmt.Errorf("download %s: %w", a.name, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		_ = tmp.Close()
		return fmt.Errorf("download %s: HTTP %d", a.name, resp.StatusCode)
	}
	h := sha256.New()
	if _, err := io.Copy(tmp, io.TeeReader(resp.Body, h)); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("download %s: %w", a.name, err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	sum := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(sum, a.sha256) {
		return fmt.Errorf("checksum mismatch for %s", a.binary)
	}
	return extractBinary(tmpName, a.binary, dest)
}

func extractBinary(tarPath, want, dest string) error {
	f, err := os.Open(tarPath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("gzip %s: %w", tarPath, err)
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar %s: %w", tarPath, err)
		}
		if filepath.Base(hdr.Name) != want || hdr.FileInfo().IsDir() {
			continue
		}
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, tr); err != nil {
			_ = out.Close()
			return err
		}
		if err := out.Close(); err != nil {
			return err
		}
		return os.Chmod(dest, 0o755)
	}
	return fmt.Errorf("archive %s does not contain %s", tarPath, want)
}
