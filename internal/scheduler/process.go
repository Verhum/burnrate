package scheduler

import (
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}

// ProcessIsClaude checks whether the given pid's command line contains "claude".
// Used as a pid-recycling guard: if the OS has reassigned the pid to an
// unrelated process, we must not kill it. Package-level var for test overrides.
var ProcessIsClaude = defaultProcessIsClaude

func defaultProcessIsClaude(pid int) bool {
	out, err := exec.Command("ps", "-o", "command=", "-p", fmt.Sprintf("%d", pid)).Output()
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(out)), "claude")
}

func killOrphan(pid int) {
	pgid, err := syscall.Getpgid(pid)
	if err == nil && pgid > 0 {
		_ = syscall.Kill(-pgid, syscall.SIGTERM)
	} else {
		_ = syscall.Kill(pid, syscall.SIGTERM)
	}

	deadline := time.After(5 * time.Second)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-deadline:
			if err == nil && pgid > 0 {
				syscall.Kill(-pgid, syscall.SIGKILL)
			} else {
				syscall.Kill(pid, syscall.SIGKILL)
			}
			return
		case <-ticker.C:
			if !processAlive(pid) {
				return
			}
		}
	}
}
