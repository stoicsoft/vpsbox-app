//go:build linux

package desktopbackend

import (
	"fmt"
	"os/exec"
	"strings"
)

// runAdminCommand executes a shell command with root privileges. On Linux
// we use pkexec (polkit) so the user gets the desktop's standard auth
// dialog instead of a TTY sudo prompt — the Wails app has no terminal.
func runAdminCommand(command string) error {
	if _, err := exec.LookPath("pkexec"); err != nil {
		return fmt.Errorf("pkexec is required for elevated installs on Linux. Install polkit (most desktops include it) or run this command yourself: sudo sh -c %q", command)
	}
	cmd := exec.Command("pkexec", "sh", "-c", command)
	output, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return fmt.Errorf("installer command failed: %s", message)
	}
	return nil
}
