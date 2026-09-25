//go:build darwin

package desktopbackend

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppBundleForExecutable(t *testing.T) {
	bundle, err := appBundleForExecutable("/Applications/VPSBox.app/Contents/MacOS/vpsbox")
	if err != nil {
		t.Fatalf("appBundleForExecutable() error = %v", err)
	}
	if bundle != "/Applications/VPSBox.app" {
		t.Fatalf("appBundleForExecutable() = %q, want /Applications/VPSBox.app", bundle)
	}
}

func TestAppBundleForExecutableRejectsLooseBinaries(t *testing.T) {
	// A `go run` build, a Homebrew binary, and a bundle-shaped path that isn't
	// one all have to be refused: replacing the wrong thing is unrecoverable.
	for _, exe := range []string{
		"/usr/local/bin/vpsbox",
		"/tmp/go-build123/b001/exe/vpsbox",
		"/Applications/VPSBox.app/Contents/Resources/vpsbox",
		"/Applications/VPSBox/Contents/MacOS/vpsbox",
	} {
		if bundle, err := appBundleForExecutable(exe); err == nil {
			t.Fatalf("appBundleForExecutable(%q) = %q, want an error", exe, bundle)
		}
	}
}

const fakeInfoPlist = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleExecutable</key><string>vpsbox</string>
<key>CFBundleIdentifier</key><string>com.stoicsoft.vpsbox.test</string>
<key>CFBundleName</key><string>VPSBox</string>
<key>CFBundlePackageType</key><string>APPL</string>
</dict></plist>
`

// fakeBundle builds an .app whose executable is a copy of a real Mach-O binary
// for this machine, which is what the architecture check reads.
func fakeBundle(t *testing.T, dir, name string) string {
	t.Helper()
	bundle := filepath.Join(dir, name)
	macos := filepath.Join(bundle, "Contents", "MacOS")
	if err := os.MkdirAll(macos, 0o755); err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile("/bin/echo")
	if err != nil {
		t.Skipf("no system binary to copy: %v", err)
	}
	if err := os.WriteFile(filepath.Join(macos, "vpsbox"), binary, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "Contents", "Info.plist"), []byte(fakeInfoPlist), 0o644); err != nil {
		t.Fatal(err)
	}
	return bundle
}

func TestFindAppBundle(t *testing.T) {
	dir := t.TempDir()
	want := fakeBundle(t, dir, "VPSBox.app")

	got, err := findAppBundle(dir)
	if err != nil {
		t.Fatalf("findAppBundle() error = %v", err)
	}
	if got != want {
		t.Fatalf("findAppBundle() = %q, want %q", got, want)
	}

	if _, err := findAppBundle(t.TempDir()); err == nil {
		t.Fatal("findAppBundle() on an empty directory = nil error, want a failure")
	}
}

func TestVerifyBundleArchitectureAcceptsAHostBinary(t *testing.T) {
	bundle := fakeBundle(t, t.TempDir(), "VPSBox.app")
	if err := verifyBundleArchitecture(bundle); err != nil {
		t.Fatalf("verifyBundleArchitecture() error = %v, want nil for a native binary", err)
	}
}

func TestVerifyBundleArchitectureRejectsGarbage(t *testing.T) {
	dir := t.TempDir()
	bundle := filepath.Join(dir, "VPSBox.app")
	macos := filepath.Join(bundle, "Contents", "MacOS")
	if err := os.MkdirAll(macos, 0o755); err != nil {
		t.Fatal(err)
	}
	// A truncated download looks exactly like this: right shape, wrong content.
	if err := os.WriteFile(filepath.Join(macos, "vpsbox"), []byte("not a mach-o"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := verifyBundleArchitecture(bundle); err == nil {
		t.Fatal("verifyBundleArchitecture() = nil, want an error for a non-executable")
	}
}

// TestStageAppBundleFromZip runs the real macOS staging path: a bundle packed
// exactly the way the release workflow packs it (`ditto -c -k --sequesterRsrc
// --keepParent`), unpacked and verified the way an update would be.
func TestStageAppBundleFromZip(t *testing.T) {
	requireTool(t, "/usr/bin/ditto")
	requireTool(t, "/usr/bin/codesign")

	source := fakeBundle(t, t.TempDir(), "VPSBox.app")
	adhocSign(t, source)

	// The installed bundle the update would replace.
	target := fakeBundle(t, t.TempDir(), "VPSBox.app")
	adhocSign(t, target)

	updates := t.TempDir()
	archive := filepath.Join(updates, "VPSBox-9-9-9-macos.zip")
	run(t, "/usr/bin/ditto", "-c", "-k", "--sequesterRsrc", "--keepParent", source, archive)

	// A Developer ID signature can't be made in a test; the signer check has
	// its own test below. This one covers unpacking and the other checks.
	realSigner := verifyBundleSigner
	verifyBundleSigner = func(codesign, source, target string) error { return nil }
	t.Cleanup(func() { verifyBundleSigner = realSigner })

	staged, err := stageAppBundle(archive, "9.9.9", target, func(string) {})
	if err != nil {
		t.Fatalf("stageAppBundle() error = %v", err)
	}
	if staged.Version != "9.9.9" || staged.Target != target {
		t.Fatalf("stageAppBundle() = %+v, want version 9.9.9 targeting %s", staged, target)
	}
	if _, err := os.Stat(filepath.Join(staged.Source, "Contents", "MacOS", "vpsbox")); err != nil {
		t.Fatalf("the staged bundle is missing its executable: %v", err)
	}
	if filepath.Dir(staged.Source) != staged.Dir {
		t.Fatalf("staged source %q is not inside the staging dir %q", staged.Source, staged.Dir)
	}
}

// TestVerifyBundleSignerRejectsAdhocBundles pins the bypass that used to exist:
// two ad-hoc signed bundles both report no team, and "no team on either side"
// counted as a match. Any self-signed download must now be refused.
func TestVerifyBundleSignerRejectsAdhocBundles(t *testing.T) {
	requireTool(t, "/usr/bin/codesign")

	source := fakeBundle(t, t.TempDir(), "VPSBox.app")
	adhocSign(t, source)
	target := fakeBundle(t, t.TempDir(), "VPSBox.app")
	adhocSign(t, target)

	if err := verifyBundleSigner("/usr/bin/codesign", source, target); err == nil {
		t.Fatal("verifyBundleSigner() = nil for ad-hoc signed bundles, want a refusal")
	}
}

func TestAppleTeamIDPattern(t *testing.T) {
	for _, team := range []string{"ABCDE12345", "Z9Y8X7W6V5"} {
		if !appleTeamIDPattern.MatchString(team) {
			t.Fatalf("appleTeamIDPattern rejected %q", team)
		}
	}
	for _, team := range []string{"", "abcde12345", "ABCDE1234", `ABCDE12345" or true`, "not set"} {
		if appleTeamIDPattern.MatchString(team) {
			t.Fatalf("appleTeamIDPattern accepted %q", team)
		}
	}
}

func TestStageAppBundleRejectsAnArchiveWithoutABundle(t *testing.T) {
	requireTool(t, "/usr/bin/ditto")

	updates := t.TempDir()
	loose := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(loose, []byte("nothing to install here"), 0o644); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(updates, "VPSBox-9-9-9-macos.zip")
	run(t, "/usr/bin/ditto", "-c", "-k", "--keepParent", loose, archive)

	if _, err := stageAppBundle(archive, "9.9.9", "/Applications/VPSBox.app", func(string) {}); err == nil {
		t.Fatal("stageAppBundle() = nil error, want a failure when the archive has no .app")
	}
}

// unusedPID is above macOS's pid ceiling, so `kill -0` on it always fails and
// the helper's "wait for the app to quit" loop falls straight through.
const unusedPID = 2147483647

// runSwapScript executes the helper with `open` and `xattr` stubbed out, so the
// test exercises the real move-and-restore logic without launching anything.
// It returns the script's exit code and whatever the stub `open` was handed.
func runSwapScript(t *testing.T, staged stagedUpdate) (int, string) {
	t.Helper()

	stubs := t.TempDir()
	opened := filepath.Join(stubs, "opened.txt")
	for _, name := range []string{"open", "xattr"} {
		stub := "#!/bin/sh\n"
		if name == "open" {
			stub += "echo \"$@\" >> " + opened + "\n"
		}
		stub += "exit 0\n"
		if err := os.WriteFile(filepath.Join(stubs, name), []byte(stub), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	logPath := filepath.Join(t.TempDir(), "update.log")
	statusPath := filepath.Join(t.TempDir(), "update-status.json")
	scriptPath := filepath.Join(t.TempDir(), "apply-update.sh")
	if err := os.WriteFile(scriptPath, []byte(updateSwapScript(staged, unusedPID, logPath, statusPath)), 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("/bin/sh", scriptPath)
	cmd.Env = append(os.Environ(), "PATH="+stubs+":"+os.Getenv("PATH"))
	err := cmd.Run()

	code := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		code = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("running the helper: %v", err)
	}

	launched, _ := os.ReadFile(opened)
	if body, readErr := os.ReadFile(logPath); readErr == nil {
		t.Logf("helper log:\n%s", body)
	}
	return code, string(launched)
}

func TestUpdateSwapScriptReplacesTheBundle(t *testing.T) {
	installDir := t.TempDir()
	target := fakeBundle(t, installDir, "VPSBox.app")
	// A marker only the new bundle carries, so "did the swap happen" is a
	// question about content rather than timestamps.
	if err := os.WriteFile(filepath.Join(target, "Contents", "version.txt"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	staging := filepath.Join(t.TempDir(), "staged-9.9.9")
	source := fakeBundle(t, staging, "VPSBox.app")
	if err := os.WriteFile(filepath.Join(source, "Contents", "version.txt"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}

	code, launched := runSwapScript(t, stagedUpdate{
		Version: "9.9.9", Source: source, Target: target, Dir: staging,
	})
	if code != 0 {
		t.Fatalf("helper exited %d, want 0", code)
	}

	installed, err := os.ReadFile(filepath.Join(target, "Contents", "version.txt"))
	if err != nil {
		t.Fatalf("the installed bundle is missing after the swap: %v", err)
	}
	if string(installed) != "new" {
		t.Fatalf("installed bundle contains %q, want the new build", installed)
	}
	if _, err := os.Stat(target + ".vpsbox-old"); !os.IsNotExist(err) {
		t.Fatal("the backup bundle was left behind")
	}
	if _, err := os.Stat(staging); !os.IsNotExist(err) {
		t.Fatal("the staging directory was left behind")
	}
	if !strings.Contains(launched, target) {
		t.Fatalf("the helper relaunched %q, want %q", launched, target)
	}
}

func TestUpdateSwapScriptRestoresTheOldBundleWhenTheNewOneIsGone(t *testing.T) {
	installDir := t.TempDir()
	target := fakeBundle(t, installDir, "VPSBox.app")
	if err := os.WriteFile(filepath.Join(target, "Contents", "version.txt"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	// The staged bundle vanished between download and restart — the user must
	// still end up with a working app.
	staging := filepath.Join(t.TempDir(), "staged-9.9.9")
	if err := os.MkdirAll(staging, 0o755); err != nil {
		t.Fatal(err)
	}

	code, launched := runSwapScript(t, stagedUpdate{
		Version: "9.9.9",
		Source:  filepath.Join(staging, "VPSBox.app"),
		Target:  target,
		Dir:     staging,
	})
	if code == 0 {
		t.Fatal("helper exited 0, want a failure when the new bundle is missing")
	}

	restored, err := os.ReadFile(filepath.Join(target, "Contents", "version.txt"))
	if err != nil {
		t.Fatalf("the old bundle was not restored: %v", err)
	}
	if string(restored) != "old" {
		t.Fatalf("restored bundle contains %q, want the old build", restored)
	}
	if !strings.Contains(launched, target) {
		t.Fatalf("the helper relaunched %q, want %q", launched, target)
	}
}

func requireTool(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Skipf("%s is not available: %v", path, err)
	}
}

// adhocSign signs a fake bundle the way a release build is signed, minus the
// real identity. The inner executable is re-signed first: it's a copy of a
// system binary and its inherited Apple signature would fail --deep --strict.
func adhocSign(t *testing.T, bundle string) {
	t.Helper()
	run(t, "/usr/bin/codesign", "--force", "--sign", "-", filepath.Join(bundle, "Contents", "MacOS", "vpsbox"))
	run(t, "/usr/bin/codesign", "--force", "--deep", "--sign", "-", bundle)
}

func run(t *testing.T, name string, args ...string) {
	t.Helper()
	if out, err := exec.Command(name, args...).CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}

func TestBundleExecutableRejectsAnEmptyBundle(t *testing.T) {
	bundle := filepath.Join(t.TempDir(), "VPSBox.app")
	if err := os.MkdirAll(filepath.Join(bundle, "Contents", "MacOS"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := bundleExecutable(bundle); err == nil {
		t.Fatal("bundleExecutable() = nil error, want a failure when Contents/MacOS is empty")
	}
}
