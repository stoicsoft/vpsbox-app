//go:build darwin

package desktopbackend

import (
	"debug/macho"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"syscall"
)

// macOS self-update: the release ships a ditto archive of vpsbox.app (or a DMG
// containing it). Staging unpacks the new bundle, and the swap replaces the
// installed .app wholesale — the same thing dragging it onto Applications does.

// canSelfUpdate reports whether the installed app can be replaced in place.
func canSelfUpdate() error {
	bundle, err := currentAppBundle()
	if err != nil {
		return err
	}
	// Gatekeeper runs unquarantined apps from a randomized read-only copy.
	// Replacing that copy would do nothing; the real bundle is elsewhere.
	if strings.Contains(bundle, "/AppTranslocation/") {
		return fmt.Errorf("macOS is running VPSBox from a temporary read-only copy. Move VPSBox into your Applications folder and reopen it, then check for updates again")
	}
	if strings.HasPrefix(bundle, "/Volumes/") {
		return fmt.Errorf("VPSBox is running from a disk image. Drag it into your Applications folder first, then check for updates again")
	}
	if !dirIsWritable(filepath.Dir(bundle)) {
		return fmt.Errorf("this account cannot write to %s, so VPSBox cannot replace itself there — download the new version and install it by hand", filepath.Dir(bundle))
	}
	// Without a team on the running app there's nothing to hold the download
	// to, so say so now rather than after a 50 MB download.
	if codesign, err := exec.LookPath("codesign"); err == nil && codesignTeamID(codesign, bundle) == "" {
		return fmt.Errorf("this copy of VPSBox is not signed with a Developer ID, so it cannot confirm who built an update — download the new version and install it by hand")
	}
	return nil
}

// currentAppBundle walks up from the running binary to the .app it lives in:
// VPSBox.app/Contents/MacOS/vpsbox → VPSBox.app.
func currentAppBundle() (string, error) {
	exe, err := currentExecutable()
	if err != nil {
		return "", err
	}
	return appBundleForExecutable(exe)
}

// appBundleForExecutable is the path arithmetic behind currentAppBundle, split
// out so the layout rules can be tested without being inside a bundle.
func appBundleForExecutable(exe string) (string, error) {
	macos := filepath.Dir(exe)
	contents := filepath.Dir(macos)
	bundle := filepath.Dir(contents)
	if filepath.Base(macos) != "MacOS" || filepath.Base(contents) != "Contents" || filepath.Ext(bundle) != ".app" {
		return "", fmt.Errorf("VPSBox is not running from an app bundle (%s), so it cannot update itself — this is expected during development", exe)
	}
	return bundle, nil
}

// stageUpdate unpacks the downloaded archive and checks the bundle inside it
// before anything is allowed near the installed copy.
func stageUpdate(archivePath, version string, progress func(string)) (stagedUpdate, error) {
	target, err := currentAppBundle()
	if err != nil {
		return stagedUpdate{}, err
	}
	return stageAppBundle(archivePath, version, target, progress)
}

// stageAppBundle unpacks archivePath and checks the bundle inside it against
// target, the installed bundle it would replace. Split from stageUpdate so the
// unpack-and-verify half can be exercised outside a real app bundle.
func stageAppBundle(archivePath, version, target string, progress func(string)) (stagedUpdate, error) {
	dir := filepath.Join(filepath.Dir(archivePath), "staged-"+version)
	if err := os.RemoveAll(dir); err != nil {
		return stagedUpdate{}, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return stagedUpdate{}, err
	}

	switch strings.ToLower(filepath.Ext(archivePath)) {
	case ".zip":
		progress("Unpacking the new app bundle")
		// ditto is the counterpart of the `ditto -c -k` the release workflow
		// uses; unlike archive/zip it preserves symlinks, permissions, and
		// resource forks, all of which a signed bundle needs intact.
		if out, err := exec.Command("/usr/bin/ditto", "-x", "-k", archivePath, dir).CombinedOutput(); err != nil {
			return stagedUpdate{}, fmt.Errorf("could not unpack the update: %s", firstLine(string(out), err))
		}
	case ".dmg":
		progress("Opening the disk image")
		if err := extractAppFromDMG(archivePath, dir); err != nil {
			return stagedUpdate{}, err
		}
	default:
		return stagedUpdate{}, fmt.Errorf("unexpected update archive %q", filepath.Base(archivePath))
	}

	source, err := findAppBundle(dir)
	if err != nil {
		return stagedUpdate{}, err
	}
	if err := verifyStagedBundle(source, target, progress); err != nil {
		return stagedUpdate{}, err
	}

	return stagedUpdate{Version: version, Source: source, Target: target, Dir: dir}, nil
}

// extractAppFromDMG mounts the image without showing it in Finder, copies the
// bundle out, and always detaches — a stuck mount is worse than a failed update.
func extractAppFromDMG(dmgPath, dest string) error {
	mount := filepath.Join(dest, "mnt")
	if err := os.MkdirAll(mount, 0o755); err != nil {
		return err
	}
	out, err := exec.Command("/usr/bin/hdiutil", "attach", dmgPath,
		"-nobrowse", "-readonly", "-noverify", "-mountpoint", mount).CombinedOutput()
	if err != nil {
		return fmt.Errorf("could not open the disk image: %s", firstLine(string(out), err))
	}
	defer func() {
		if out, err := exec.Command("/usr/bin/hdiutil", "detach", mount, "-quiet").CombinedOutput(); err != nil {
			_ = out
			exec.Command("/usr/bin/hdiutil", "detach", mount, "-force", "-quiet").Run()
		}
		os.RemoveAll(mount)
	}()

	app, err := findAppBundle(mount)
	if err != nil {
		return err
	}
	copied := filepath.Join(dest, filepath.Base(app))
	if out, err := exec.Command("/usr/bin/ditto", app, copied).CombinedOutput(); err != nil {
		return fmt.Errorf("could not copy the app out of the disk image: %s", firstLine(string(out), err))
	}
	return nil
}

// findAppBundle returns the single .app directly inside dir.
func findAppBundle(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if entry.IsDir() && filepath.Ext(entry.Name()) == ".app" {
			return filepath.Join(dir, entry.Name()), nil
		}
	}
	return "", fmt.Errorf("the download did not contain a VPSBox app bundle")
}

// verifyStagedBundle refuses anything that isn't a runnable, correctly signed
// bundle. The signing team is taken from the running app rather than
// hardcoded, so a build signed by someone else can never replace this one.
//
// The signature check is mandatory: stripping the quarantine flag afterwards
// means Gatekeeper never gets a second opinion. If codesign is somehow
// unavailable, self-update is refused and the user is left with the browser
// download rather than an unverified swap.
func verifyStagedBundle(source, target string, progress func(string)) error {
	if _, err := os.Stat(filepath.Join(source, "Contents", "MacOS")); err != nil {
		return fmt.Errorf("the downloaded app bundle is incomplete")
	}
	if err := verifyBundleArchitecture(source); err != nil {
		return err
	}

	codesign, err := exec.LookPath("codesign")
	if err != nil {
		return fmt.Errorf("codesign is not available on this Mac, so the download cannot be verified — install it by hand from the release page")
	}

	progress("Checking the signature of the new app")
	return verifyBundleSigner(codesign, source, target)
}

// verifyBundleSigner requires source to carry an Apple-issued Developer ID
// signature from the same team as target. Both halves are mandatory: an ad-hoc
// signature has no team, and "no team on either side" used to count as a
// match, which let any self-signed bundle through. The requirement is checked
// by codesign itself, so the team has to be in the certificate chain Apple
// issued, not just in a string the bundle reports. A variable so tests can
// exercise staging with ad-hoc bundles.
var verifyBundleSigner = func(codesign, source, target string) error {
	currentTeam := codesignTeamID(codesign, target)
	if currentTeam == "" {
		return fmt.Errorf("this copy of VPSBox is not signed with a Developer ID, so the update cannot be verified — install it by hand from the release page")
	}
	if !appleTeamIDPattern.MatchString(currentTeam) {
		return fmt.Errorf("this copy of VPSBox reports an unexpected signing team %q", currentTeam)
	}

	requirement := fmt.Sprintf(`=anchor apple generic and certificate leaf[subject.OU] = "%s"`, currentTeam)
	if out, err := exec.Command(codesign, "--verify", "--deep", "--strict", "-R", requirement, source).CombinedOutput(); err != nil {
		newTeam := codesignTeamID(codesign, source)
		if newTeam == "" {
			newTeam = "no Developer ID"
		}
		return fmt.Errorf("the downloaded app is not signed by this app's developer (%s, expected %s): %s",
			newTeam, currentTeam, firstLine(string(out), err))
	}
	return nil
}

// appleTeamIDPattern is the shape of an Apple team identifier. It goes into a
// codesign requirement string, so nothing else is allowed through.
var appleTeamIDPattern = regexp.MustCompile(`^[A-Z0-9]{10}$`)

// verifyBundleArchitecture refuses a build this Mac cannot run. The release
// workflow produces a single-architecture app (whatever the runner was), so an
// Intel Mac offered an Apple-silicon build would install an app that no longer
// launches — the one failure mode a user can't undo from inside the app.
func verifyBundleArchitecture(bundle string) error {
	exe, err := bundleExecutable(bundle)
	if err != nil {
		return err
	}

	want, known := machoCPUFor(runtime.GOARCH)
	if !known {
		return nil
	}

	if fat, err := macho.OpenFat(exe); err == nil {
		defer fat.Close()
		for _, arch := range fat.Arches {
			if arch.Cpu == want {
				return nil
			}
		}
		return fmt.Errorf("the downloaded app does not include a build for this Mac (%s)", runtime.GOARCH)
	}

	file, err := macho.Open(exe)
	if err != nil {
		return fmt.Errorf("the downloaded app does not contain a usable executable: %w", err)
	}
	defer file.Close()
	if file.Cpu != want {
		return fmt.Errorf("the downloaded app is built for %s, and this Mac needs %s — install it by hand if you know it will run", machoArchName(file.Cpu), runtime.GOARCH)
	}
	return nil
}

// bundleExecutable returns the main binary inside an .app. Wails bundles hold
// exactly one file in Contents/MacOS.
func bundleExecutable(bundle string) (string, error) {
	dir := filepath.Join(bundle, "Contents", "MacOS")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("the downloaded app bundle is incomplete")
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			return filepath.Join(dir, entry.Name()), nil
		}
	}
	return "", fmt.Errorf("the downloaded app bundle has no executable")
}

func machoCPUFor(goarch string) (macho.Cpu, bool) {
	switch goarch {
	case "amd64":
		return macho.CpuAmd64, true
	case "arm64":
		return macho.CpuArm64, true
	default:
		return 0, false
	}
}

func machoArchName(cpu macho.Cpu) string {
	switch cpu {
	case macho.CpuAmd64:
		return "amd64"
	case macho.CpuArm64:
		return "arm64"
	default:
		return cpu.String()
	}
}

// codesignTeamID reads the Apple team identifier out of a bundle's signature,
// returning "" for unsigned or ad-hoc signed builds.
func codesignTeamID(codesign, path string) string {
	out, err := exec.Command(codesign, "-dv", "--verbose=4", path).CombinedOutput()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		if value, ok := strings.CutPrefix(strings.TrimSpace(line), "TeamIdentifier="); ok {
			value = strings.TrimSpace(value)
			if value == "not set" {
				return ""
			}
			return value
		}
	}
	return ""
}

// launchUpdateSwap starts the detached helper that replaces the installed
// bundle once this process exits, then relaunches the app.
func launchUpdateSwap(staged stagedUpdate) error {
	logPath, err := updateLogPath()
	if err != nil {
		return err
	}

	statusPath, err := updateStatusPath()
	if err != nil {
		return err
	}
	path, err := writeHelperScript(filepath.Dir(staged.Dir), "apply-update.sh",
		updateSwapScript(staged, os.Getpid(), logPath, statusPath))
	if err != nil {
		return err
	}

	cmd := exec.Command("/bin/sh", path)
	// Setsid detaches the helper from this process group so quitting the app
	// (or the whole session) doesn't take the updater down with it.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("could not start the update helper: %w", err)
	}
	go cmd.Wait() // reap the helper if we outlive it
	return nil
}

// updateSwapScript renders the helper that replaces the installed bundle. It
// runs unattended after the app is gone, so every failure has to leave a
// working app behind: the old bundle is moved aside rather than deleted, and
// restored if anything goes wrong before the new one is in place.
func updateSwapScript(staged stagedUpdate, pid int, logPath, statusPath string) string {
	backup := staged.Target + ".vpsbox-old"
	return fmt.Sprintf(`#!/bin/sh
# VPSBox update helper. Written at update time; safe to delete.
set -u
exec >>%s 2>&1

pid=%d
target=%s
source=%s
backup=%s
staging=%s
status=%s
version=%s
echo "=== $(date) installing VPSBox $version ==="

# The app reads this on its next launch, which is the only way a failure that
# happens after we've quit can reach the user.
report() {
	printf '%%s\n' "$1" > "$status" 2>/dev/null || true
	echo "$1"
}

# Wait for the app to exit before touching its bundle.
tries=0
while kill -0 "$pid" 2>/dev/null; do
	tries=$((tries + 1))
	if [ "$tries" -gt %d ]; then
		# Never swap under a live process: it would keep running from the
		# moved-aside bundle while "open" started a second copy. Leave the
		# staged payload alone so the next launch can offer it again.
		report "failed VPSBox was still running, so version $version was not installed. It is downloaded and will be offered again."
		exit 1
	fi
	sleep 0.1
done

rm -rf "$backup"
if ! mv "$target" "$backup"; then
	report "failed Could not move the installed app aside to install $version."
	open "$target" 2>/dev/null || true
	exit 1
fi
if ! mv "$source" "$target"; then
	echo "could not move the new app into place; restoring the old one"
	rm -rf "$target"
	mv "$backup" "$target"
	report "failed Could not install $version. The previous version was restored."
	open "$target" 2>/dev/null || true
	exit 1
fi

rm -rf "$backup"
# The archive came from the internet, so the unpacked bundle carries a
# quarantine flag that would make Gatekeeper re-prompt on every launch.
xattr -dr com.apple.quarantine "$target" 2>/dev/null || true
rm -rf "$staging"
report "ok $version"
open "$target"
`,
		singleQuote(logPath),
		pid,
		singleQuote(staged.Target),
		singleQuote(staged.Source),
		singleQuote(backup),
		singleQuote(staged.Dir),
		singleQuote(statusPath),
		singleQuote(staged.Version),
		swapWaitTicks,
	)
}

// firstLine trims command output down to something worth showing in the UI.
func firstLine(output string, fallback error) string {
	for _, line := range strings.Split(output, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	if fallback == nil {
		return ""
	}
	return fallback.Error()
}
