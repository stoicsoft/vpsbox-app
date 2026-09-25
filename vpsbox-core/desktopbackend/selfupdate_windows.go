//go:build windows

package desktopbackend

import (
	"debug/pe"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
)

// Windows self-update takes one of two routes, decided by where the app is
// installed:
//
//   - Installed by the NSIS setup (Program Files): run the downloaded setup
//     again. It elevates through UAC, replaces the install, and we relaunch.
//   - Running as a standalone exe somewhere writable: rename the running exe
//     aside and move the new one in. Windows allows renaming a locked exe,
//     which is what makes the in-place swap possible at all.
//
// Either way the work happens in a detached .cmd helper that waits for this
// process to exit first.

func canSelfUpdate() error {
	if _, err := currentExecutable(); err != nil {
		return err
	}
	return nil
}

// stageUpdate prepares the downloaded asset. An installer is used as-is; a
// standalone exe is checked for the right architecture before it can replace
// the running one.
func stageUpdate(archivePath, version string, progress func(string)) (stagedUpdate, error) {
	target, err := currentExecutable()
	if err != nil {
		return stagedUpdate{}, err
	}
	if !strings.EqualFold(filepath.Ext(archivePath), ".exe") {
		return stagedUpdate{}, fmt.Errorf("unexpected update download %q", filepath.Base(archivePath))
	}

	dir := filepath.Join(filepath.Dir(archivePath), "staged-"+version)
	if err := os.RemoveAll(dir); err != nil {
		return stagedUpdate{}, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return stagedUpdate{}, err
	}

	source := filepath.Join(dir, filepath.Base(archivePath))
	progress("Preparing the downloaded build")
	// The download already sits in the updates directory, so moving it into
	// the staging folder beside it is a rename, not another 100 MB of copying.
	if err := os.Rename(archivePath, source); err != nil {
		if err := copyFile(archivePath, source, 0o755); err != nil {
			return stagedUpdate{}, err
		}
	}

	if !isInstallerAsset(source) {
		progress("Checking the new build")
		if err := verifyStagedExecutable(source); err != nil {
			return stagedUpdate{}, err
		}
	}

	return stagedUpdate{Version: version, Source: source, Target: target, Dir: dir}, nil
}

// isInstallerAsset distinguishes the NSIS setup from the standalone exe. The
// release workflow names them VPSBox-<version>-windows-setup.exe and
// VPSBox-<version>-windows.exe.
func isInstallerAsset(path string) bool {
	name := strings.ToLower(filepath.Base(path))
	return strings.Contains(name, "setup") || strings.Contains(name, "installer")
}

// verifyStagedExecutable reads the PE header — enough to catch a truncated
// download or a build for the wrong architecture without running the file.
func verifyStagedExecutable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Size() == 0 {
		return fmt.Errorf("the downloaded build is empty")
	}

	file, err := pe.Open(path)
	if err != nil {
		return fmt.Errorf("the downloaded build is not a Windows executable: %w", err)
	}
	defer file.Close()

	want, known := peMachineFor(runtime.GOARCH)
	if known && file.Machine != want {
		return fmt.Errorf("the downloaded build is for a different architecture (0x%x, expected 0x%x)", file.Machine, want)
	}
	return nil
}

func peMachineFor(goarch string) (uint16, bool) {
	switch goarch {
	case "amd64":
		return pe.IMAGE_FILE_MACHINE_AMD64, true
	case "arm64":
		return pe.IMAGE_FILE_MACHINE_ARM64, true
	case "386":
		return pe.IMAGE_FILE_MACHINE_I386, true
	default:
		return 0, false
	}
}

// launchUpdateSwap writes and starts the detached helper.
func launchUpdateSwap(staged stagedUpdate) error {
	logPath, err := updateLogPath()
	if err != nil {
		return err
	}

	var action string
	if isInstallerAsset(staged.Source) {
		// The installer needs a visible window: it elevates through UAC and
		// the user has to approve it. /wait keeps the relaunch until it's done.
		action = fmt.Sprintf(`echo Running the installer...>>%s
start "" /wait %s
if errorlevel 1 echo Installer exited with an error>>%s`,
			quoteBatch(logPath), quoteBatch(staged.Source), quoteBatch(logPath))
	} else {
		action = fmt.Sprintf(`echo Replacing %s...>>%s
del /q %s >nul 2>&1
move /y %s %s >>%s 2>&1
if errorlevel 1 (
  echo Could not move the running exe aside>>%s
  goto relaunch
)
move /y %s %s >>%s 2>&1
if errorlevel 1 (
  echo Could not move the new exe into place; restoring>>%s
  move /y %s %s >nul 2>&1
  goto relaunch
)
del /q %s >nul 2>&1`,
			quoteBatch(staged.Target), quoteBatch(logPath),
			quoteBatch(staged.Target+".vpsbox-old"),
			quoteBatch(staged.Target), quoteBatch(staged.Target+".vpsbox-old"), quoteBatch(logPath),
			quoteBatch(logPath),
			quoteBatch(staged.Source), quoteBatch(staged.Target), quoteBatch(logPath),
			quoteBatch(logPath),
			quoteBatch(staged.Target+".vpsbox-old"), quoteBatch(staged.Target),
			quoteBatch(staged.Target+".vpsbox-old"))
	}

	script := fmt.Sprintf(`@echo off
rem VPSBox update helper. Written at update time; safe to delete.
setlocal
echo === %%DATE%% %%TIME%% installing VPSBox %s ===>>%s

rem Wait for the app to exit before touching its files (~30s ceiling).
set /a tries=0
:waitloop
tasklist /FI "PID eq %d" /NH 2>nul | find "%d" >nul
if errorlevel 1 goto ready
set /a tries+=1
if %%tries%% GEQ 60 (
  echo Timed out waiting for pid %d to exit>>%s
  goto ready
)
timeout /t 1 /nobreak >nul
goto waitloop

:ready
%s

:relaunch
rmdir /s /q %s >nul 2>&1
start "" %s
`,
		staged.Version, quoteBatch(logPath),
		os.Getpid(), os.Getpid(),
		os.Getpid(), quoteBatch(logPath),
		action,
		quoteBatch(staged.Dir),
		quoteBatch(staged.Target),
	)

	path, err := writeHelperScript(filepath.Dir(staged.Dir), "apply-update.cmd", script)
	if err != nil {
		return err
	}

	cmd := exec.Command("cmd.exe", "/c", path)
	// Detach from this process so the helper survives the app quitting, and
	// keep its console window hidden.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windowsDetachedProcess | windowsCreateNewProcessGroup,
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("could not start the update helper: %w", err)
	}
	go cmd.Wait()
	return nil
}

const (
	windowsDetachedProcess       = 0x00000008
	windowsCreateNewProcessGroup = 0x00000200
)

// quoteBatch wraps a path for cmd.exe. Batch has no escape for a double quote
// inside a quoted string, so a path containing one can't be represented — and
// Windows doesn't allow " in paths, so rejecting it here is free.
func quoteBatch(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, "") + `"`
}
