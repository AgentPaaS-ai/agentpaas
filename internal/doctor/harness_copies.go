package doctor

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// harnessBinaryName is the primary harness binary name searched for in PATH.
const harnessBinaryName = "agentpaas-harness-linux"

// harnessAltNames are the alternative harness binary names considered when
// scanning PATH (the non-linux suffixed build).
var harnessAltNames = []string{"agentpaas-harness"}

// collectHarnessCandidatePaths returns every candidate path that
// resolveHarnessBinary (in internal/daemon) would consider, in resolution
// order:
//  1. filepath.Dir(os.Executable())/agentpaas-harness-linux
//  2. filepath.Dir(os.Executable())/../bin/agentpaas-harness-linux
//  3. filepath.Dir(os.Executable())/agentpaas-harness
//  4. every directory in PATH containing agentpaas-harness-linux
//
// Only paths that exist as a regular file (or symlink to one) are returned.
// If os.Executable errors, only the PATH scan (item 4) is performed.
func collectHarnessCandidatePaths() []string {
	seen := make(map[string]bool)
	var paths []string

	addIfExists := func(p string) {
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		// Use Lstat to avoid following symlinks into stat errors on
		// broken links; a symlink that resolves to a file is fine.
		info, err := os.Lstat(p)
		if err != nil {
			return
		}
		if info.IsDir() {
			return
		}
		paths = append(paths, p)
	}

	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exePath)
		addIfExists(filepath.Join(exeDir, harnessBinaryName))
		addIfExists(filepath.Join(exeDir, "..", "bin", harnessBinaryName))
		for _, alt := range harnessAltNames {
			addIfExists(filepath.Join(exeDir, alt))
		}
	}

	// Walk PATH for agentpaas-harness-linux and the alt names.
	names := append([]string{harnessBinaryName}, harnessAltNames...)
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" {
			continue
		}
		for _, name := range names {
			addIfExists(filepath.Join(dir, name))
		}
	}

	return paths
}

// harnessCopyInfo records the path, sha256 hash, and modification time of a
// found harness binary copy.
type harnessCopyInfo struct {
	path   string
	hash   string
	mtime  time.Time
	hasMT  bool
}

// hashAndStat returns the sha256 (full hex) and mtime of the file at path.
// If the file cannot be opened or stat'd, hash is "" and hasMT is false.
func hashAndStat(path string) (string, time.Time, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", time.Time{}, false
	}
	defer func() { _ = f.Close() }() // best-effort close

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", time.Time{}, false
	}
	sum := hex.EncodeToString(h.Sum(nil))

	info, err := os.Stat(path)
	if err != nil {
		return sum, time.Time{}, false
	}
	return sum, info.ModTime(), true
}

// compareHarnessCopies inspects a list of candidate harness binary paths and
// returns a doctor status + message. If two or more existing copies have
// DIFFERENT sha256 hashes, the status is "warning" and the message lists each
// path with the first 8 hex chars of its hash and its mtime, followed by a
// sentence explaining that pack uses the first copy in resolution order.
// If zero or one copies exist, or all copies are byte-identical, the status is
// "ok". The function never returns "error" (it is informational only).
func compareHarnessCopies(paths []string) (string, string) {
	var copies []harnessCopyInfo
	for _, p := range paths {
		hash, mt, hasMT := hashAndStat(p)
		if hash == "" {
			// File disappeared between Lstat and Open — skip it.
			continue
		}
		copies = append(copies, harnessCopyInfo{
			path:  p,
			hash:  hash,
			mtime: mt,
			hasMT: hasMT,
		})
	}

	if len(copies) < 2 {
		var msg string
		if len(copies) == 0 {
			msg = "no agentpaas-harness-linux copies found"
		} else {
			msg = fmt.Sprintf("single harness copy at %s", copies[0].path)
		}
		return "ok", msg
	}

	// Check for divergence: do all hashes match?
	first := copies[0].hash
	allSame := true
	for _, c := range copies[1:] {
		if c.hash != first {
			allSame = false
			break
		}
	}
	if allSame {
		return "ok", fmt.Sprintf("%d harness copies found, all byte-identical (sha256 %s…)", len(copies), first[:8])
	}

	// Divergence — build the warning message.
	var lines []string
	for _, c := range copies {
		mtimeStr := "mtime unknown"
		if c.hasMT {
			mtimeStr = c.mtime.Format(time.RFC3339)
		}
		lines = append(lines, fmt.Sprintf("  %s (sha256 %s, mtime %s)", c.path, c.hash[:8], mtimeStr))
	}
	msg := strings.Join(lines, "\n") + "\nmultiple differing harness binaries found; pack uses the first in resolution order"
	return "warning", msg
}

// CheckHarnessCopies is a doctor informational check that detects whether
// multiple divergent agentpaas-harness-linux binaries exist on the machine.
//
// It collects every candidate path that resolveHarnessBinary would consider
// (daemon-executable-relative candidates plus every PATH directory containing
// the harness binary), computes the sha256 of each, and warns if two or more
// copies differ. This catches the defect where a stale root-owned copy at
// /usr/local/bin shadows the current one at /opt/homebrew/bin and gets
// silently embedded into agent images by `agentpaas pack`.
//
// The check status is at most "warning"; it never fails the doctor. It must
// not panic if os.Executable errors (it falls back to a PATH-only scan).
func CheckHarnessCopies() CheckResult {
	name := "harness_copies"
	paths := collectHarnessCandidatePaths()
	status, msg := compareHarnessCopies(paths)
	return CheckResult{
		Name:    name,
		Status:  status,
		Message: msg,
	}
}

