//go:build windows

package objstore

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

// processIsBinary cannot be answered cheaply here, and local object storage is not
// supported on Windows anyway, so nothing reaches this path in practice.
func processIsBinary(pid int, name string) bool {
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
