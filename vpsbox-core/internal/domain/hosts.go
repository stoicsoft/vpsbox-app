package domain

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const (
	startMarker = "# >>> vpsbox >>>"
	endMarker   = "# <<< vpsbox <<<"
)

var ErrPrivilegesRequired = errors.New("updating hosts file requires elevated privileges")

type Record struct {
	Host string
	IP   string
}

type Names struct {
	Hostname string
}

func NamesForInstance(name string) Names {
	return Names{
		Hostname: fmt.Sprintf("%s.vpsbox.local", name),
	}
}

func ValidateIP(ip string) error {
	if parsed := net.ParseIP(ip); parsed == nil {
		return fmt.Errorf("invalid IP %q", ip)
	}
	return nil
}

func SyncHosts(path string, records []Record) error {
	if len(records) == 0 {
		return writeHosts(path, "")
	}

	lines := []string{startMarker}
	for _, record := range records {
		if err := ValidateIP(record.IP); err != nil {
			return err
		}
		lines = append(lines, fmt.Sprintf("%s %s", record.IP, record.Host))
	}
	lines = append(lines, endMarker)

	return writeHosts(path, strings.Join(lines, "\n")+"\n")
}

func writeHosts(path, section string) error {
	current, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	content := replaceManagedSection(string(current), section)
	if err := os.WriteFile(path, []byte(content), 0o644); err == nil {
		return nil
	}

	if !isInteractiveTerminal() {
		return ErrPrivilegesRequired
	}

	if runtime.GOOS == "windows" {
		return fmt.Errorf("%w: rerun as Administrator to update %s", ErrPrivilegesRequired, path)
	}

	// Use a temp file so sudo can still read the password from the terminal.
	tmp, err := os.CreateTemp("", "vpsbox-hosts-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sudo", "--non-interactive", "install", "-m", "0644", tmpPath, path)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stdout
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", ErrPrivilegesRequired, strings.TrimSpace(stdout.String()))
	}

	return nil
}

func replaceManagedSection(content, section string) string {
	start := strings.Index(content, startMarker)
	end := strings.Index(content, endMarker)
	if start >= 0 && end >= start {
		end += len(endMarker)
		content = strings.TrimSpace(content[:start] + content[end:])
	}

	content = strings.TrimSpace(content)
	if section == "" {
		if content == "" {
			return ""
		}
		return content + "\n"
	}

	if content == "" {
		return section
	}

	return content + "\n\n" + section
}

func isInteractiveTerminal() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}
