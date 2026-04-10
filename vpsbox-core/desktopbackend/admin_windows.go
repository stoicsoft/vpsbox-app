//go:build windows

package desktopbackend

import (
	"fmt"
	"os/exec"
	"strings"
)

// runAdminCommand launches the given command with elevated privileges via
// PowerShell's Start-Process -Verb RunAs, which triggers a UAC prompt.
//
// The command string is split into executable and arguments. If the string
// contains spaces the first token is used as -FilePath and the rest as
// -ArgumentList so that commands like "dism.exe /Online /Enable-Feature ..."
// work correctly.
func runAdminCommand(command string) error {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return fmt.Errorf("empty command")
	}

	filePath := parts[0]
	psSafe := strings.ReplaceAll(filePath, `"`, "`\"")
	var psCommand string
	if len(parts) > 1 {
		args := strings.ReplaceAll(strings.Join(parts[1:], " "), `"`, "`\"")
		psCommand = fmt.Sprintf(`Start-Process -FilePath "%s" -ArgumentList "%s" -Verb RunAs -Wait`, psSafe, args)
	} else {
		psCommand = fmt.Sprintf(`Start-Process -FilePath "%s" -Verb RunAs -Wait`, psSafe)
	}

	cmd := exec.Command("powershell.exe",
		"-NoProfile",
		"-NonInteractive",
		"-ExecutionPolicy", "Bypass",
		"-Command", psCommand,
	)
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
