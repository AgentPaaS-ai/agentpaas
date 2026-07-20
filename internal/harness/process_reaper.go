package harness

import (
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type childReaper struct {
	mu      sync.Mutex
	tracked map[int]struct{}

	sigCh   chan os.Signal
	done    chan struct{}
	stopped chan struct{}
	once    sync.Once
	noop    bool
}

func startChildReaper() *childReaper {
	r := &childReaper{
		tracked: make(map[int]struct{}),
		done:    make(chan struct{}),
		stopped: make(chan struct{}),
		noop:    runtime.GOOS != "linux",
	}
	if r.noop {
		close(r.stopped)
		return r
	}

	r.sigCh = make(chan os.Signal, 1)
	signal.Notify(r.sigCh, syscall.SIGCHLD)
	go r.run()
	return r
}

func (r *childReaper) Track(pid int) {
	if r == nil || pid <= 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tracked[pid] = struct{}{}
}

func (r *childReaper) Untrack(pid int) {
	if r == nil || pid <= 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.tracked, pid)
}

func (r *childReaper) Stop() {
	if r == nil {
		return
	}
	r.once.Do(func() {
		if r.noop {
			return
		}
		close(r.done)
		signal.Stop(r.sigCh)
		<-r.stopped
	})
}

func (r *childReaper) run() {
	defer close(r.stopped)

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-r.done:
			return
		case <-r.sigCh:
			r.reap()
		case <-ticker.C:
			r.reap()
		}
	}
}

func (r *childReaper) reap() {
	for _, pid := range r.reapableChildren() {
		var status syscall.WaitStatus
		var usage syscall.Rusage
		_, _ = syscall.Wait4(pid, &status, syscall.WNOHANG, &usage) // best-effort reaper wait
	}
}

func (r *childReaper) reapableChildren() []int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}

	ppid := os.Getpid()
	var pids []int
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}
		if r.isTracked(pid) {
			continue
		}
		childPPID, err := procPPID(pid)
		if err != nil || childPPID != ppid {
			continue
		}
		pids = append(pids, pid)
	}
	return pids
}

func (r *childReaper) isTracked(pid int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.tracked[pid]
	return ok
}

func procPPID(pid int) (int, error) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/status")
	if err != nil {
		return 0, fmt.Errorf("proc ppid: %w", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "PPid:") {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				return 0, nil
			}
			return strconv.Atoi(fields[1])
		}
	}
	return 0, nil
}
