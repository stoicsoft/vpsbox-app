//go:build !windows

package share

import "syscall"

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
