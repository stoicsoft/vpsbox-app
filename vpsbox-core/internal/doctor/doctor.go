package doctor

import (
	"context"
	"fmt"
	"os"
	"runtime"

	"github.com/stoicsoft/vpsbox/internal/config"
	"github.com/stoicsoft/vpsbox/internal/executil"
)

type Status string

const (
	StatusOK   Status = "ok"
	StatusWarn Status = "warn"
	StatusFail Status = "fail"
)

type Check struct {
	Name    string `json:"name"`
	Status  Status `json:"status"`
	Details string `json:"details"`
}

func Run(ctx context.Context, paths config.Paths) []Check {
	_ = ctx
	checks := []Check{
		checkOS(),
		checkBinary("multipass", "VM backend"),
		checkBinary("mkcert", "TLS helper"),
		checkBinary("cloudflared", "share relay"),
		checkBinary("ssh", "SSH client"),
		checkHosts(paths.HostsFilePath),
		checkDisk(paths.BaseDir),
	}
	return checks
}

func checkOS() Check {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		return Check{Name: "platform", Status: StatusFail, Details: fmt.Sprintf("%s/%s is not supported", runtime.GOOS, runtime.GOARCH)}
	}

	status := StatusWarn
	details := fmt.Sprintf("%s/%s detected; macOS is the primary v1 target", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "darwin" {
		status = StatusOK
		details = fmt.Sprintf("%s/%s detected", runtime.GOOS, runtime.GOARCH)
	}
	return Check{Name: "platform", Status: status, Details: details}
}

func checkBinary(bin, name string) Check {
	if executil.LookPath(bin) {
		return Check{Name: bin, Status: StatusOK, Details: name + " is installed"}
	}
	return Check{Name: bin, Status: StatusWarn, Details: name + " is not installed yet"}
}

func checkHosts(path string) Check {
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err == nil {
		_ = file.Close()
		return Check{Name: "hosts", Status: StatusOK, Details: path + " is writable"}
	}
	return Check{Name: "hosts", Status: StatusWarn, Details: path + " will require sudo for domain setup"}
}

func checkDisk(path string) Check {
	freeGB, err := freeDiskGB(path)
	if err != nil {
		return Check{Name: "disk", Status: StatusWarn, Details: "unable to read free disk space"}
	}

	status := StatusOK
	details := fmt.Sprintf("%.1f GB free in %s", freeGB, path)
	if freeGB < 10 {
		status = StatusWarn
		details = fmt.Sprintf("low free disk space: %.1f GB in %s", freeGB, path)
	}
	return Check{Name: "disk", Status: status, Details: details}
}
