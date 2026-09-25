//go:build darwin

package desktopbackend

import (
	"fmt"
	"os/exec"
	"strings"
)

// runAdminCommand executes a shell command with administrator privileges.
// On macOS we use osascript so the user gets the standard system password
// prompt instead of a TTY sudo prompt (which the desktop app doesn't have).
func runAdminCommand(command string) error {
	script := fmt.Sprintf(`do shell script %q with administrator privileges`, command)
	cmd := exec.Command("osascript", "-e", script)
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
