package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"
)

const (
	seatWaitFlagKey         = "SEAT_WAIT_WORKFLOW_PRIMITIVE"
	seatWaitFixtureFile     = "test/w27-seat-wait-flag.test.ts"
	seatWaitFixtureTimeout  = 4 * time.Minute
	seatWaitProofLinePrefix = "W27_SEAT_WAIT "
	seatWaitEnvLinePrefix   = "W27_WORKER_ENV "
)

// seatWaitProveMu is taken only after the wrangler file locks.
var seatWaitProveMu sync.Mutex

type seatWaitFixtureFunc func(ctx context.Context, workerDir string) (string, error)

var runSeatWaitFixture seatWaitFixtureFunc = defaultRunSeatWaitFixture

var seatWaitFlagLine = regexp.MustCompile(`(?m)^SEAT_WAIT_WORKFLOW_PRIMITIVE = "([01])"$`)

func seatWaitFixtureArgs() []string {
	return []string{
		"npx", "vitest", "run",
		"--disableConsoleIntercept",
		"--reporter=verbose",
		seatWaitFixtureFile,
	}
}

func defaultRunSeatWaitFixture(ctx context.Context, workerDir string) (string, error) {
	runCtx, cancel := context.WithTimeout(ctx, seatWaitFixtureTimeout)
	defer cancel()
	args := seatWaitFixtureArgs()
	cmd := exec.CommandContext(runCtx, args[0], args[1:]...)
	cmd.Dir = workerDir
	cmd.Env = os.Environ()
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	out := buf.String()
	if runCtx.Err() == context.DeadlineExceeded {
		return out, errors.New("seat-wait fixture timed out")
	}
	if err != nil {
		return out, fmt.Errorf("seat-wait fixture: %w", err)
	}
	return out, nil
}

func newCloudSeatWaitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "seat-wait",
		Short: "Flip the seat-wait workflow flag and re-run its fixture",
		Long: `Flip Worker env SEAT_WAIT_WORKFLOW_PRIMITIVE, re-run the seat-wait fixture, and flip the flag back off.

This does not call wrangler, does not deploy, and does not change concurrency.
The hop inside the fixture does not span seat_wait_min.`,
	}
	cmd.AddCommand(newCloudSeatWaitProveCmd())
	return cmd
}

func newCloudSeatWaitProveCmd() *cobra.Command {
	var workerDir string
	cmd := &cobra.Command{
		Use:   "prove",
		Short: "Flip SEAT_WAIT_WORKFLOW_PRIMITIVE on, re-run the seat-wait fixture, flip it off",
		Long: `Flip Worker env SEAT_WAIT_WORKFLOW_PRIMITIVE to 1, re-run the seat-wait fixture, then set the flag back to 0 and re-run it.

Flag-on and flag-off terminals must match. The flag is left off.
Does not call wrangler.

  agentpaas cloud seat-wait prove --worker-dir /absolute/path/to/cloud`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := proveSeatWaitWorkflowPrimitive(cmd.Context(), workerDir)
			if err != nil {
				return fmt.Errorf("cloud seat-wait prove: %w", err)
			}
			return printTextOrJSON(jsonOutput(cmd), result, func(v interface{}) string {
				return formatSeatWaitProveText(v.(seatWaitProveResult))
			})
		},
	}
	cmd.Flags().StringVar(&workerDir, "worker-dir", "", "Absolute path to the cloud worker directory (required)")
	_ = cmd.MarkFlagRequired("worker-dir")
	return cmd
}

type seatWaitProveResult struct {
	FlagBefore       string               `json:"flag_before"`
	FlagOn           string               `json:"flag_on"`
	FlagAfter        string               `json:"flag_after"`
	TerminalsMatch   bool                 `json:"terminals_match"`
	FlagOnTerminals  seatWaitTerminalView `json:"flag_on_terminals"`
	FlagOffTerminals seatWaitTerminalView `json:"flag_off_terminals"`
	Wrangler         bool                 `json:"wrangler"`
	HopSpannedWait   bool                 `json:"hop_spanned_seat_wait_min"`
}

type seatWaitTerminalView struct {
	Flag          string `json:"flag"`
	ParentError   string `json:"parent_error"`
	ChildError    string `json:"child_error"`
	ChildPromoted bool   `json:"child_promoted"`
	HopElapsedMs  int    `json:"hop_elapsed_ms"`
	SeatWaitMinMs int    `json:"seat_wait_min_ms"`
	RetryOnHop    int    `json:"retry_on_hop"`
	StartOnHop    int    `json:"start_on_hop"`
	WorkerEnvFlag string `json:"worker_env_flag"`
}

type seatWaitProof struct {
	Flag          string  `json:"flag"`
	WorkerEnvFlag string  `json:"worker_env_flag"`
	ParentError   *string `json:"parent_error"`
	ChildError    *string `json:"child_error"`
	ChildPromoted bool    `json:"child_promoted"`
	HopElapsedMs  int     `json:"hop_elapsed_ms"`
	SeatWaitMinMs int     `json:"seat_wait_min_ms"`
	RetryOnHop    int     `json:"retry_on_hop"`
	StartOnHop    int     `json:"start_on_hop"`
}

func formatSeatWaitProveText(r seatWaitProveResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "flag_before %s\n", r.FlagBefore)
	fmt.Fprintf(&b, "flag_on %s\n", r.FlagOn)
	fmt.Fprintf(&b, "flag_after %s\n", r.FlagAfter)
	fmt.Fprintf(&b, "terminals_match %t\n", r.TerminalsMatch)
	fmt.Fprintf(&b, "wrangler %t\n", r.Wrangler)
	fmt.Fprintf(&b, "hop_spanned_seat_wait_min %t\n", r.HopSpannedWait)
	fmt.Fprintf(&b, "flag_on_parent_error %s\n", r.FlagOnTerminals.ParentError)
	fmt.Fprintf(&b, "flag_on_child_error %s\n", r.FlagOnTerminals.ChildError)
	fmt.Fprintf(&b, "flag_on_child_promoted %t\n", r.FlagOnTerminals.ChildPromoted)
	fmt.Fprintf(&b, "flag_on_hop_elapsed_ms %d\n", r.FlagOnTerminals.HopElapsedMs)
	fmt.Fprintf(&b, "flag_on_seat_wait_min_ms %d\n", r.FlagOnTerminals.SeatWaitMinMs)
	fmt.Fprintf(&b, "flag_off_parent_error %s\n", r.FlagOffTerminals.ParentError)
	fmt.Fprintf(&b, "flag_off_child_error %s\n", r.FlagOffTerminals.ChildError)
	fmt.Fprintf(&b, "flag_off_child_promoted %t\n", r.FlagOffTerminals.ChildPromoted)
	fmt.Fprintf(&b, "flag_off_hop_elapsed_ms %d\n", r.FlagOffTerminals.HopElapsedMs)
	fmt.Fprintf(&b, "flag_off_seat_wait_min_ms %d\n", r.FlagOffTerminals.SeatWaitMinMs)
	return strings.TrimRight(b.String(), "\n")
}

type seatWaitLockedFile struct {
	path string
	f    *os.File
	fd   int
}

func proveSeatWaitWorkflowPrimitive(ctx context.Context, workerDir string) (result seatWaitProveResult, err error) {
	dir, err := validateSeatWaitWorkerDir(workerDir)
	if err != nil {
		return seatWaitProveResult{}, err
	}
	paths, err := seatWaitEnvPaths(dir)
	if err != nil {
		return seatWaitProveResult{}, err
	}
	locks, err := lockSeatWaitEnvFiles(paths)
	if err != nil {
		return seatWaitProveResult{}, err
	}
	defer func() {
		if cerr := unlockSeatWaitEnvFiles(locks); cerr != nil && err == nil {
			err = cerr
		}
	}()
	seatWaitProveMu.Lock()
	defer seatWaitProveMu.Unlock()

	before, err := readLockedSeatWaitFlag(locks)
	if err != nil {
		return seatWaitProveResult{}, err
	}
	if err := writeLockedSeatWaitFlag(locks, "1"); err != nil {
		return seatWaitProveResult{}, err
	}
	defer func() {
		if werr := writeLockedSeatWaitFlag(locks, "0"); werr != nil && err == nil {
			err = werr
		}
	}()
	onFlag, err := readLockedSeatWaitFlag(locks)
	if err != nil {
		return seatWaitProveResult{}, err
	}
	onOut, onErr := runSeatWaitFixture(ctx, dir)
	if onErr != nil {
		return seatWaitProveResult{}, fmt.Errorf("flag-on fixture: %w", snippetErr(onErr, onOut))
	}
	if err := writeLockedSeatWaitFlag(locks, "0"); err != nil {
		return seatWaitProveResult{}, err
	}
	offOut, offErr := runSeatWaitFixture(ctx, dir)
	if offErr != nil {
		return seatWaitProveResult{}, fmt.Errorf("flag-off fixture: %w", snippetErr(offErr, offOut))
	}
	after, err := readLockedSeatWaitFlag(locks)
	if err != nil {
		return seatWaitProveResult{}, err
	}
	onTerms, err := parseSeatWaitFixture(onOut, "1")
	if err != nil {
		return seatWaitProveResult{}, fmt.Errorf("flag-on fixture output: %w", err)
	}
	offTerms, err := parseSeatWaitFixture(offOut, "0")
	if err != nil {
		return seatWaitProveResult{}, fmt.Errorf("flag-off fixture output: %w", err)
	}
	if !seatWaitTerminalsMatch(onTerms, offTerms) {
		return seatWaitProveResult{}, errors.New("flag-on terminals do not match flag-off terminals")
	}
	if after != "0" || onFlag != "1" {
		return seatWaitProveResult{}, fmt.Errorf("flag state before=%s on=%s after=%s", before, onFlag, after)
	}
	result = seatWaitProveResult{
		FlagBefore:       before,
		FlagOn:           onFlag,
		FlagAfter:        after,
		TerminalsMatch:   true,
		FlagOnTerminals:  onTerms,
		FlagOffTerminals: offTerms,
		Wrangler:         false,
		HopSpannedWait:   onTerms.HopElapsedMs >= onTerms.SeatWaitMinMs || offTerms.HopElapsedMs >= offTerms.SeatWaitMinMs,
	}
	if result.HopSpannedWait || result.FlagOnTerminals.ChildPromoted || result.FlagOffTerminals.ChildPromoted {
		return seatWaitProveResult{}, errors.New("seat-wait fixture terminals are not safe")
	}
	return result, nil
}

func snippetErr(err error, out string) error {
	out = strings.TrimSpace(out)
	if len(out) > 500 {
		out = out[len(out)-500:]
	}
	if out == "" {
		return err
	}
	return fmt.Errorf("%w: %s", err, out)
}

func validateSeatWaitWorkerDir(workerDir string) (string, error) {
	if workerDir == "" {
		return "", errors.New("--worker-dir is required")
	}
	if strings.ContainsRune(workerDir, 0) || strings.ContainsAny(workerDir, "\n\r") {
		return "", errors.New("--worker-dir contains invalid characters")
	}
	if !filepath.IsAbs(workerDir) {
		return "", errors.New("--worker-dir must be an absolute path")
	}
	for _, component := range strings.Split(filepath.ToSlash(workerDir), "/") {
		if component == ".." {
			return "", errors.New("--worker-dir must not contain '..'")
		}
	}
	cleaned := filepath.Clean(workerDir)
	if seatWaitSystemDir(cleaned) {
		return "", errors.New("--worker-dir must not be a system directory")
	}
	if err := rejectSeatWaitSymlinkPath(cleaned); err != nil {
		return "", err
	}
	info, err := os.Lstat(cleaned)
	if err != nil {
		return "", fmt.Errorf("inspect --worker-dir: %w", err)
	}
	if !info.IsDir() {
		return "", errors.New("--worker-dir is not a directory")
	}
	return cleaned, nil
}

func seatWaitSystemDir(path string) bool {
	switch path {
	case "/", "/var", "/tmp", "/private", "/private/var", "/private/tmp":
		return true
	}
	prefixes := []string{
		"/etc", "/usr", "/bin", "/sbin", "/System", "/Library",
		"/dev", "/proc", "/sys", "/opt", "/Applications",
		"/private/etc", "/tmp", "/private/tmp", "/var/tmp", "/private/var/tmp",
	}
	for _, prefix := range prefixes {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}

func rejectSeatWaitSymlinkPath(path string) error {
	current := path
	for {
		info, err := os.Lstat(current)
		if err != nil {
			return fmt.Errorf("inspect %s: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 && !seatWaitAllowedVolumeSymlink(current) {
			return fmt.Errorf("path component %s is a symlink", current)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return nil
		}
		current = parent
	}
}

func seatWaitAllowedVolumeSymlink(path string) bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	return path == "/var" || path == "/tmp"
}

func seatWaitEnvPaths(workerDir string) ([]string, error) {
	names := []string{"wrangler.toml", "wrangler.test.toml"}
	var paths []string
	for _, name := range names {
		path := filepath.Join(workerDir, name)
		info, err := os.Lstat(path)
		if err != nil {
			if name == "wrangler.toml" || !os.IsNotExist(err) {
				return nil, fmt.Errorf("inspect %s: %w", name, err)
			}
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("%s is a symlink", name)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("%s is not a regular file", name)
		}
		data, err := readSeatWaitFileNoFollow(path)
		if err != nil {
			return nil, err
		}
		if name == "wrangler.test.toml" && !seatWaitFlagLine.Match(data) {
			continue
		}
		if _, err := seatWaitFlagValue(data); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		paths = append(paths, path)
	}
	if len(paths) == 0 {
		return nil, errors.New("worker env SEAT_WAIT_WORKFLOW_PRIMITIVE not found")
	}
	return paths, nil
}

func readSeatWaitFileNoFollow(path string) (data []byte, err error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}
	f := os.NewFile(uintptr(fd), path)
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("close %s: %w", filepath.Base(path), cerr)
		}
	}()
	data, err = io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}
	return data, nil
}

func lockSeatWaitEnvFiles(paths []string) ([]*seatWaitLockedFile, error) {
	var locks []*seatWaitLockedFile
	for _, path := range paths {
		fd, err := unix.Open(path, unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if err != nil {
			_ = unlockSeatWaitEnvFiles(locks)
			return nil, fmt.Errorf("open %s: %w", filepath.Base(path), err)
		}
		if err := unix.Flock(fd, unix.LOCK_EX); err != nil {
			_ = unix.Close(fd)
			_ = unlockSeatWaitEnvFiles(locks)
			return nil, fmt.Errorf("lock %s: %w", filepath.Base(path), err)
		}
		locks = append(locks, &seatWaitLockedFile{
			path: path,
			f:    os.NewFile(uintptr(fd), path),
			fd:   fd,
		})
	}
	return locks, nil
}

func unlockSeatWaitEnvFiles(locks []*seatWaitLockedFile) error {
	var err error
	for i := len(locks) - 1; i >= 0; i-- {
		lock := locks[i]
		if lock == nil || lock.f == nil {
			continue
		}
		if uerr := unix.Flock(lock.fd, unix.LOCK_UN); uerr != nil && err == nil {
			err = uerr
		}
		if cerr := lock.f.Close(); cerr != nil && err == nil {
			err = cerr
		}
		lock.f = nil
	}
	return err
}

func readLockedSeatWaitFlag(locks []*seatWaitLockedFile) (string, error) {
	var flag string
	for i, lock := range locks {
		data, err := readLockedSeatWaitFile(lock.f)
		if err != nil {
			return "", err
		}
		got, err := seatWaitFlagValue(data)
		if err != nil {
			return "", fmt.Errorf("%s: %w", filepath.Base(lock.path), err)
		}
		if i == 0 {
			flag = got
			continue
		}
		if got != flag {
			return "", errors.New("worker env flag files disagree")
		}
	}
	return flag, nil
}

func writeLockedSeatWaitFlag(locks []*seatWaitLockedFile, value string) error {
	if value != "0" && value != "1" {
		return errors.New("seat-wait flag must be 0 or 1")
	}
	if strings.ContainsAny(value, "\n\r\x00") {
		return errors.New("seat-wait flag contains invalid characters")
	}
	for _, lock := range locks {
		data, err := readLockedSeatWaitFile(lock.f)
		if err != nil {
			return err
		}
		next, err := setSeatWaitFlag(data, value)
		if err != nil {
			return fmt.Errorf("%s: %w", filepath.Base(lock.path), err)
		}
		if err := writeLockedSeatWaitFile(lock.f, next); err != nil {
			return fmt.Errorf("%s: %w", filepath.Base(lock.path), err)
		}
	}
	return nil
}

func readLockedSeatWaitFile(f *os.File) ([]byte, error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	return io.ReadAll(f)
}

func writeLockedSeatWaitFile(f *os.File, data []byte) error {
	if err := f.Truncate(0); err != nil {
		return err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		return err
	}
	return f.Sync()
}

func seatWaitFlagValue(data []byte) (string, error) {
	if bytes.Contains(data, []byte{0}) {
		return "", errors.New("worker env contains a null byte")
	}
	matches := seatWaitFlagLine.FindAllSubmatch(data, -1)
	if len(matches) != 1 {
		return "", errors.New("SEAT_WAIT_WORKFLOW_PRIMITIVE must appear once as \"0\" or \"1\"")
	}
	value := string(matches[0][1])
	if strings.ContainsAny(value, "\n\r\x00") {
		return "", errors.New("seat-wait flag contains invalid characters")
	}
	return value, nil
}

func setSeatWaitFlag(data []byte, value string) ([]byte, error) {
	if _, err := seatWaitFlagValue(data); err != nil {
		return nil, err
	}
	loc := seatWaitFlagLine.FindSubmatchIndex(data)
	if loc == nil {
		return nil, errors.New("SEAT_WAIT_WORKFLOW_PRIMITIVE not found")
	}
	out := append([]byte{}, data[:loc[2]]...)
	out = append(out, value...)
	out = append(out, data[loc[3]:]...)
	got, err := seatWaitFlagValue(out)
	if err != nil {
		return nil, err
	}
	if got != value {
		return nil, errors.New("seat-wait flag write did not stick")
	}
	return out, nil
}

func parseSeatWaitFixture(out, workerFlag string) (seatWaitTerminalView, error) {
	if !strings.Contains(out, seatWaitEnvLinePrefix+workerFlag) {
		return seatWaitTerminalView{}, fmt.Errorf("fixture did not observe worker env %s", workerFlag)
	}
	var on, off *seatWaitProof
	for _, line := range strings.Split(out, "\n") {
		idx := strings.Index(line, seatWaitProofLinePrefix)
		if idx < 0 {
			continue
		}
		raw := strings.TrimSpace(line[idx+len(seatWaitProofLinePrefix):])
		var proof seatWaitProof
		if err := json.Unmarshal([]byte(raw), &proof); err != nil {
			return seatWaitTerminalView{}, fmt.Errorf("parse fixture line: %w", err)
		}
		switch proof.Flag {
		case "1":
			on = &proof
		case "0":
			off = &proof
		}
	}
	if on == nil || off == nil {
		return seatWaitTerminalView{}, errors.New("fixture did not print both flag terminals")
	}
	if on.WorkerEnvFlag != workerFlag || off.WorkerEnvFlag != workerFlag {
		return seatWaitTerminalView{}, errors.New("fixture worker_env_flag does not match the flipped flag")
	}
	if !seatWaitProofsMatch(on, off) {
		return seatWaitTerminalView{}, errors.New("fixture flag-on and flag-off terminals differ")
	}
	picked := off
	if workerFlag == "1" {
		picked = on
	}
	return seatWaitView(picked), nil
}

func seatWaitProofsMatch(a, b *seatWaitProof) bool {
	if a == nil || b == nil || a.ParentError == nil || b.ParentError == nil || a.ChildError == nil || b.ChildError == nil {
		return false
	}
	if *a.ParentError == "" || *a.ChildError == "" || *b.ParentError == "" || *b.ChildError == "" {
		return false
	}
	return *a.ParentError == *b.ParentError &&
		*a.ChildError == *b.ChildError &&
		a.ChildPromoted == b.ChildPromoted &&
		!a.ChildPromoted &&
		a.RetryOnHop == 0 && b.RetryOnHop == 0 &&
		a.StartOnHop == 0 && b.StartOnHop == 0 &&
		a.HopElapsedMs < a.SeatWaitMinMs &&
		b.HopElapsedMs < b.SeatWaitMinMs &&
		a.SeatWaitMinMs > 0
}

func seatWaitTerminalsMatch(on, off seatWaitTerminalView) bool {
	return on.ParentError == off.ParentError &&
		on.ChildError == off.ChildError &&
		on.ParentError != "" &&
		on.ChildError != "" &&
		!on.ChildPromoted &&
		!off.ChildPromoted &&
		on.RetryOnHop == 0 && off.RetryOnHop == 0 &&
		on.StartOnHop == 0 && off.StartOnHop == 0 &&
		on.HopElapsedMs < on.SeatWaitMinMs &&
		off.HopElapsedMs < off.SeatWaitMinMs
}

func seatWaitView(p *seatWaitProof) seatWaitTerminalView {
	parent := ""
	child := ""
	if p.ParentError != nil {
		parent = *p.ParentError
	}
	if p.ChildError != nil {
		child = *p.ChildError
	}
	return seatWaitTerminalView{
		Flag:          p.Flag,
		ParentError:   parent,
		ChildError:    child,
		ChildPromoted: p.ChildPromoted,
		HopElapsedMs:  p.HopElapsedMs,
		SeatWaitMinMs: p.SeatWaitMinMs,
		RetryOnHop:    p.RetryOnHop,
		StartOnHop:    p.StartOnHop,
		WorkerEnvFlag: p.WorkerEnvFlag,
	}
}
