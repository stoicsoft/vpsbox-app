//go:build !windows

package objstore

import (
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// processIsBinary reports whether pid is running the named binary. PIDs are
// recycled, so a recorded PID can belong to something else entirely by the time
// we come back to it — without this check, replacing a wedged server could
// terminate an unrelated process group.
//
// A process we cannot inspect is reported as not ours: leaving a stale server
// running is recoverable, killing the wrong process is not.
func processIsBinary(pid int, name string) bool {
	if pid <= 0 {
		return false
	}
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "comm=").Output()
	if err != nil {
		return false
	}
	command := strings.TrimSpace(string(out))
	if command == "" {
		return false
	}
	// comm= is the executable path or basename depending on the platform.
	return strings.HasSuffix(command, "/"+name) || command == name
}

func processGroupAttrs() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}

func killProcessGroup(pid int) error {
	if pid <= 0 {
		return nil
	}
	return syscall.Kill(-pid, syscall.SIGTERM)
}
