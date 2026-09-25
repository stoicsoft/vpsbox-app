package app

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"

	"github.com/stoicsoft/vpsbox/internal/registry"
)

const (
	// remoteLineLimit caps a single streamed line; installers occasionally emit
	// very long progress lines and the default scanner buffer would error out.
	remoteLineLimit = 1 << 20
	// remoteTailLines is how much trailing output a failed run reports back.
	remoteTailLines = 60
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
	args, err := m.sshArgs(instance)
	if err != nil {
		return "", "", err
	}
	return runSSH(ctx, args, command)
}

// runSSH executes a command over an already-assembled SSH argument list. The
// instance helpers and the migration endpoints (which also talk to machines
// that are not sandboxes) share it.
func runSSH(ctx context.Context, sshArgs []string, command string) (string, string, error) {
	args := append(append([]string{}, sshArgs...), command)
	cmd := exec.CommandContext(ctx, "ssh", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String(), stderr.String(), fmt.Errorf("ssh: %w", err)
	}
	return stdout.String(), stderr.String(), nil
}

// streamRemoteOn runs a command in the sandbox and hands every output line to
// onLine as it arrives, instead of waiting for the command to finish. Installs
// take minutes, so the desktop job log and the CLI both need the running
// commentary rather than one buffered dump at the end.
//
// stdout and stderr are interleaved in arrival order — that is what the
// installer's own output looks like on a terminal. The trailing lines are kept
// so a failure can carry context back to the caller.
func (m *Manager) streamRemoteOn(ctx context.Context, instance *registry.Instance, command string, onLine func(string)) (string, error) {
	args, err := m.sshArgs(instance)
	if err != nil {
		return "", err
	}
	return streamSSH(ctx, args, command, onLine)
}

// streamSSH is runSSH's line-streaming sibling, shared for the same reason.
func streamSSH(ctx context.Context, sshArgs []string, command string, onLine func(string)) (string, error) {
	args := append(append([]string{}, sshArgs...), command)
	cmd := exec.CommandContext(ctx, "ssh", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", err
	}
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("ssh: %w", err)
	}

	var (
		mu   sync.Mutex
		tail []string
		wg   sync.WaitGroup
	)
	pump := func(r io.Reader) {
		defer wg.Done()
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 0, 64*1024), remoteLineLimit)
		for scanner.Scan() {
			line := strings.TrimRight(scanner.Text(), "\r")
			mu.Lock()
			tail = append(tail, line)
			if len(tail) > remoteTailLines {
				tail = tail[len(tail)-remoteTailLines:]
			}
			if onLine != nil {
				onLine(line)
			}
			mu.Unlock()
		}
	}
	wg.Add(2)
	go pump(stdout)
	go pump(stderr)
	wg.Wait()

	runErr := cmd.Wait()
	mu.Lock()
	defer mu.Unlock()
	output := strings.Join(tail, "\n")
	if runErr != nil {
		return output, fmt.Errorf("ssh: %w", runErr)
	}
	return output, nil
}

func (m *Manager) sshArgs(instance *registry.Instance) ([]string, error) {
	host := instance.Host
	if host == "" {
		host = instance.Hostname
	}
	if host == "" {
		return nil, errors.New("instance has no host address yet — wait a moment and try again")
	}
	if strings.TrimSpace(instance.PrivateKeyPath) == "" {
		return nil, errors.New("no SSH key is configured for this sandbox")
	}
	return []string{
		"-i", instance.PrivateKeyPath,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=" + m.paths.KnownHosts,
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=10",
		fmt.Sprintf("%s@%s", instance.Username, host),
	}, nil
}
