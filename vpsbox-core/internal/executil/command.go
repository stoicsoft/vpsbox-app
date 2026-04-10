package executil

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Result struct {
	Stdout string
	Stderr string
}

func Run(ctx context.Context, name string, args ...string) (Result, error) {
	name = resolveExecutable(name)
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = withEnrichedPath()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	res := Result{
		Stdout: strings.TrimSpace(stdout.String()),
		Stderr: strings.TrimSpace(stderr.String()),
	}
	if err != nil {
		detail := res.Stderr
		if detail == "" {
			detail = strings.TrimSpace(res.Stdout)
		}
		if detail == "" {
			detail = err.Error()
		}
		return res, fmt.Errorf("%s %s: %s", name, strings.Join(args, " "), detail)
	}

	return res, nil
}

func LookPath(bin string) bool {
	resolved := resolveExecutable(bin)
	return resolved != ""
}

func resolveExecutable(bin string) string {
	if strings.Contains(bin, "/") {
		return bin
	}
	if resolved, err := exec.LookPath(bin); err == nil {
		return resolved
	}
	for _, dir := range searchPaths() {
		candidate := filepath.Join(dir, bin)
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return candidate
		}
	}
	return ""
}

func withEnrichedPath() []string {
	env := os.Environ()
	filtered := make([]string, 0, len(env)+1)
	for _, item := range env {
		if !strings.HasPrefix(item, "PATH=") {
			filtered = append(filtered, item)
		}
	}
	filtered = append(filtered, "PATH="+strings.Join(searchPaths(), ":"))
	return filtered
}

func searchPaths() []string {
	ordered := []string{}
	seen := map[string]bool{}
	for _, entry := range strings.Split(os.Getenv("PATH"), ":") {
		entry = strings.TrimSpace(entry)
		if entry == "" || seen[entry] {
			continue
		}
		seen[entry] = true
		ordered = append(ordered, entry)
	}
	for _, entry := range []string{
		"/opt/homebrew/bin",
		"/opt/homebrew/sbin",
		"/usr/local/bin",
		"/usr/local/sbin",
		"/usr/bin",
		"/bin",
		"/usr/sbin",
		"/sbin",
	} {
		if seen[entry] {
			continue
		}
		seen[entry] = true
		ordered = append(ordered, entry)
	}
	return ordered
}
