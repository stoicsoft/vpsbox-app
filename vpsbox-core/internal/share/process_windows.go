//go:build windows

package share

import (
	"os"
	"syscall"
)

func processGroupAttrs() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{}
}

func processAlive(pid int) bool {
	return pid > 0
}

func killProcessGroup(pid int) error {
	if pid <= 0 {
		return nil
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Kill()
}
