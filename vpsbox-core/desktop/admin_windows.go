//go:build windows

package main

import (
	"fmt"
	"os/exec"
	"strings"
)

// runAdminCommand launches the given command with elevated privileges via
// PowerShell's Start-Process -Verb RunAs, which triggers a UAC prompt. The
// command argument is treated as either an .exe path or a full command
// line that cmd.exe can run.
//
// We pass through whatever the caller hands us, so the typical use is to
// give it the path to an installer .exe (e.g. the Multipass Windows
// installer), which then handles its own wizard UI.
func runAdminCommand(command string) error {
	// Quote the command for PowerShell. We avoid embedded double quotes by
	// substituting backticks (PowerShell escape) — most installer paths are
	// drive-letter style and don't need any escaping.
	psSafe := strings.ReplaceAll(command, `"`, "`\"")
	psCommand := fmt.Sprintf(`Start-Process -FilePath "%s" -Verb RunAs -Wait`, psSafe)

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
