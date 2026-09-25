//go:build darwin

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/stoicsoft/vpsbox/internal/executil"
)

func installAllPrerequisites(progress func(string)) error {
	report := func(message string) {
		if progress != nil {
			progress(message)
		}
	}

	if !executil.LookPath("multipass") {
		report("Installing Multipass")
		if err := installMultipass(); err != nil {
			return err
		}
	}

	report("Installing or updating mkcert")
	if err := installMKCert(); err != nil {
		return err
	}

	if !executil.LookPath("cloudflared") {
		report("Installing cloudflared")
		if err := installCloudflared(); err != nil {
			return err
		}
	}

	report("System packages are installed")
	return nil
}

func installMultipass() error {
	asset, err := latestGitHubAsset("canonical", "multipass", regexp.MustCompile(`multipass-.*\+mac-Darwin\.pkg$`))
	if err != nil {
		return err
	}
	pkgPath, err := downloadAsset(asset.URL, asset.Name)
	if err != nil {
		return err
	}
	defer os.RemoveAll(filepath.Dir(pkgPath))
	return runAdminCommand(fmt.Sprintf("/usr/sbin/installer -pkg %s -target /", singleQuote(pkgPath)))
}

func installCloudflared() error {
	pattern := regexp.MustCompile(`cloudflared-arm64\.pkg$`)
	if runtime.GOARCH == "amd64" {
		pattern = regexp.MustCompile(`cloudflared-amd64\.pkg$`)
	}
	asset, err := latestGitHubAsset("cloudflare", "cloudflared", pattern)
	if err != nil {
		return err
	}
	pkgPath, err := downloadAsset(asset.URL, asset.Name)
	if err != nil {
		return err
	}
	defer os.RemoveAll(filepath.Dir(pkgPath))
	return runAdminCommand(fmt.Sprintf("/usr/sbin/installer -pkg %s -target /", singleQuote(pkgPath)))
}

func installMKCert() error {
	pattern := regexp.MustCompile(`mkcert-.*-darwin-arm64$`)
	if runtime.GOARCH == "amd64" {
		pattern = regexp.MustCompile(`mkcert-.*-darwin-amd64$`)
	}
	asset, err := latestGitHubAsset("FiloSottile", "mkcert", pattern)
	if err != nil {
		return err
	}
	binaryPath, err := downloadAsset(asset.URL, asset.Name)
	if err != nil {
		return err
	}
	defer os.RemoveAll(filepath.Dir(binaryPath))
	if err := os.Chmod(binaryPath, 0o755); err != nil {
		return err
	}
	command := strings.Join([]string{
		"mkdir -p /usr/local/bin",
		fmt.Sprintf("install -m 0755 %s /usr/local/bin/mkcert", singleQuote(binaryPath)),
		"/usr/local/bin/mkcert -install",
	}, " && ")
	return runAdminCommand(command)
}
