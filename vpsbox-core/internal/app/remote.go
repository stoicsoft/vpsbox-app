package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/stoicsoft/vpsbox/internal/registry"
)

// RunRemote executes a non-interactive shell command inside the named sandbox
// and returns its stdout, stderr, and any error. It's the foundation for the
// learn / diff / deploy / tour / logs commands.
func (m *Manager) RunRemote(ctx context.Context, name string, command string) (string, string, error) {
	instance, err := m.requireInstance(name)
	if err != nil {
		return "", "", err
	}
	return m.runRemoteOn(ctx, instance, command)
}

func (m *Manager) runRemoteOn(ctx context.Context, instance *registry.Instance, command string) (string, string, error) {
	host := instance.Host
	if host == "" {
		host = instance.Hostname
	}
	if host == "" {
		return "", "", errors.New("instance has no host address yet — wait a moment and try again")
	}
	if strings.TrimSpace(instance.PrivateKeyPath) == "" {
		return "", "", errors.New("no SSH key is configured for this sandbox")
	}

	args := []string{
		"-i", instance.PrivateKeyPath,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=" + m.paths.KnownHosts,
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=10",
		fmt.Sprintf("%s@%s", instance.Username, host),
		command,
	}

	cmd := exec.CommandContext(ctx, "ssh", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String(), stderr.String(), fmt.Errorf("ssh: %w", err)
	}
	return stdout.String(), stderr.String(), nil
}
