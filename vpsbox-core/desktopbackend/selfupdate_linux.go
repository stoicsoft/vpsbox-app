//go:build linux

package desktopbackend

import (
	"archive/tar"
	"compress/gzip"
	"debug/elf"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
)

// Linux self-update: the release ships a tarball containing the vpsbox binary.
// Staging extracts it, and the swap renames the new binary over the installed
// one. Rename works even while the old binary is executing — the running
// process keeps its own inode.

// maxUpdateBinarySize bounds what we're willing to extract from the tarball.
// The desktop binary is well under 200 MB; anything larger is a bad archive.
const maxUpdateBinarySize = 512 << 20

func canSelfUpdate() error {
	exe, err := currentExecutable()
	if err != nil {
		return err
	}
	if !dirIsWritable(filepath.Dir(exe)) {
		return fmt.Errorf("this account cannot write to %s, so VPSBox cannot replace itself there — download the new version and install it by hand", filepath.Dir(exe))
	}
	return nil
}

// stageUpdate extracts the replacement binary from the downloaded tarball.
func stageUpdate(archivePath, version string, progress func(string)) (stagedUpdate, error) {
	target, err := currentExecutable()
	if err != nil {
		return stagedUpdate{}, err
	}

	dir := filepath.Join(filepath.Dir(archivePath), "staged-"+version)
	if err := os.RemoveAll(dir); err != nil {
		return stagedUpdate{}, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return stagedUpdate{}, err
	}

	progress("Unpacking the new build")
	source := filepath.Join(dir, filepath.Base(target))
	if err := extractBinaryFromTarGz(archivePath, source); err != nil {
		return stagedUpdate{}, err
	}

	progress("Checking the new build")
	if err := verifyStagedBinary(source, version); err != nil {
		return stagedUpdate{}, err
	}

	return stagedUpdate{Version: version, Source: source, Target: target, Dir: dir}, nil
}

// extractBinaryFromTarGz pulls the first regular executable file out of the
// tarball. The release tarball holds a single `vpsbox` binary at its root.
func extractBinaryFromTarGz(archivePath, dest string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()

	gz, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("the update download is not a valid archive: %w", err)
	}
	defer gz.Close()

	reader := tar.NewReader(gz)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("could not read the update archive: %w", err)
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		// Ignore any path inside the archive: we only ever want the binary,
		// and honouring archive paths is how tarballs escape their target dir.
		name := filepath.Base(header.Name)
		if strings.HasPrefix(name, ".") || header.FileInfo().Mode().Perm()&0o111 == 0 {
			continue
		}
		if header.Size > maxUpdateBinarySize {
			return fmt.Errorf("the update archive contains an implausibly large file (%s)", formatBytes(header.Size))
		}

		out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(out, io.LimitReader(reader, maxUpdateBinarySize))
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		return nil
	}
	return fmt.Errorf("the update archive did not contain a VPSBox binary")
}

// verifyStagedBinary inspects the extracted file without running it: a GUI
// binary can't be smoke-tested with --version, but reading its ELF header
// proves it's an executable built for this machine's architecture, which is
// what a truncated or wrong-platform download would fail.
func verifyStagedBinary(path, version string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Size() == 0 {
		return fmt.Errorf("the downloaded build is empty")
	}

	file, err := elf.Open(path)
	if err != nil {
		return fmt.Errorf("the downloaded build is not a Linux executable: %w", err)
	}
	defer file.Close()

	want, known := elfMachineFor(runtime.GOARCH)
	if known && file.Machine != want {
		return fmt.Errorf("the downloaded build is for a different architecture (%s, expected %s)", file.Machine, want)
	}
	return nil
}

// elfMachineFor maps a Go arch to its ELF machine type. Unknown arches report
// false so an exotic platform degrades to "no arch check" instead of a refusal.
func elfMachineFor(goarch string) (elf.Machine, bool) {
	switch goarch {
	case "amd64":
		return elf.EM_X86_64, true
	case "arm64":
		return elf.EM_AARCH64, true
	case "386":
		return elf.EM_386, true
	case "arm":
		return elf.EM_ARM, true
	default:
		return 0, false
	}
}

// launchUpdateSwap starts the detached helper that replaces the binary once
// this process exits, then relaunches it.
func launchUpdateSwap(staged stagedUpdate) error {
	logPath, err := updateLogPath()
	if err != nil {
		return err
	}

	path, err := writeHelperScript(filepath.Dir(staged.Dir), "apply-update.sh",
		updateSwapScript(staged, os.Getpid(), logPath))
	if err != nil {
		return err
	}

	cmd := exec.Command("/bin/sh", path)
	// Setsid detaches the helper so quitting the app doesn't kill the updater.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("could not start the update helper: %w", err)
	}
	go cmd.Wait()
	return nil
}

// updateSwapScript renders the helper that replaces the installed binary. It
// runs unattended after the app is gone, so it relaunches whatever is at the
// target path on every exit path — a failed update must still leave the user
// with a running app.
func updateSwapScript(staged stagedUpdate, pid int, logPath string) string {
	return fmt.Sprintf(`#!/bin/sh
# VPSBox update helper. Written at update time; safe to delete.
set -u
exec >>%s 2>&1

pid=%d
target=%s
source=%s
staging=%s
version=%s
incoming="$target.vpsbox-new"
echo "=== $(date) installing VPSBox $version ==="

tries=0
while kill -0 "$pid" 2>/dev/null; do
	tries=$((tries + 1))
	if [ "$tries" -gt 300 ]; then
		echo "timed out waiting for pid $pid to exit"
		break
	fi
	sleep 0.1
done

# Copy next to the target first so the final step is an atomic rename on the
# same filesystem; renaming over a running binary is allowed on Linux.
if ! cp "$source" "$incoming"; then
	echo "could not write next to $target"
	"$target" >/dev/null 2>&1 &
	exit 1
fi
chmod 755 "$incoming"
if ! mv -f "$incoming" "$target"; then
	echo "could not replace $target"
	rm -f "$incoming"
	"$target" >/dev/null 2>&1 &
	exit 1
fi

rm -rf "$staging"
echo "installed $target"
"$target" >/dev/null 2>&1 &
`,
		singleQuote(logPath),
		pid,
		singleQuote(staged.Target),
		singleQuote(staged.Source),
		singleQuote(staged.Dir),
		singleQuote(staged.Version),
	)
}
