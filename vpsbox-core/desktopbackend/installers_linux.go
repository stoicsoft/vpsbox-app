//go:build linux

package desktopbackend

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"

	"github.com/stoicsoft/vpsbox/internal/executil"
)

func installAllPrerequisites(progress func(string)) error {
	report := func(message string) {
		if progress != nil {
			progress(message)
		}
	}

	if !executil.LookPath("multipass") {
		report("Installing Multipass via snap (this requires sudo)")
		if err := installMultipassLinux(); err != nil {
			return err
		}
	}

	report("Installing mkcert into ~/.vpsbox/bin")
	if err := installMKCertLinux(); err != nil {
		return err
	}

	if !executil.LookPath("cloudflared") {
		report("Installing cloudflared into ~/.vpsbox/bin")
		if err := installCloudflaredLinux(); err != nil {
			return err
		}
	}

	report("System packages are installed")
	return nil
}

func installMultipassLinux() error {
	if !executil.LookPath("snap") {
		return fmt.Errorf("snap is required to install Multipass on Linux. Install snapd first (e.g. `sudo apt install snapd`) and try again")
	}
	// Multipass on Linux is officially distributed via snap. --classic is
	// required because multipassd needs broad system access.
	return runAdminCommand("snap install multipass --classic")
}

func installMKCertLinux() error {
	arch := "amd64"
	if runtime.GOARCH == "arm64" {
		arch = "arm64"
	}
	pattern := regexp.MustCompile(fmt.Sprintf(`mkcert-.*-linux-%s$`, arch))
	asset, err := latestGitHubAsset("FiloSottile", "mkcert", pattern)
	if err != nil {
		return err
	}
	binaryPath, err := downloadAsset(asset.URL, asset.Name)
	if err != nil {
		return err
	}
	defer os.RemoveAll(filepath.Dir(binaryPath))
	binDir, err := vpsboxBinDir()
	if err != nil {
		return err
	}
	target := filepath.Join(binDir, "mkcert")
	if err := copyFile(binaryPath, target, 0o755); err != nil {
		return err
	}
	// Best-effort: try to install the user trust store. mkcert -install
	// writes to ~/.local/share/mkcert and ~/.pki/nssdb without root, so it
	// usually works without elevation. We deliberately swallow errors —
	// the TLS manager has a self-signed fallback if mkcert isn't trusted.
	_, _ = executil.Run(context.Background(), target, "-install")
	return nil
}

func installCloudflaredLinux() error {
	arch := "amd64"
	if runtime.GOARCH == "arm64" {
		arch = "arm64"
	}
	pattern := regexp.MustCompile(fmt.Sprintf(`cloudflared-linux-%s$`, arch))
	asset, err := latestGitHubAsset("cloudflare", "cloudflared", pattern)
	if err != nil {
		return err
	}
	binaryPath, err := downloadAsset(asset.URL, asset.Name)
	if err != nil {
		return err
	}
	defer os.RemoveAll(filepath.Dir(binaryPath))
	binDir, err := vpsboxBinDir()
	if err != nil {
		return err
	}
	target := filepath.Join(binDir, "cloudflared")
	return copyFile(binaryPath, target, 0o755)
}
