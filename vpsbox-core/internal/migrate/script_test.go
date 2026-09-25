package migrate

import (
	"strings"
	"testing"
)

func TestCaptureScriptCoversSections(t *testing.T) {
	script := CaptureScript()
	for _, section := range []string{"@@privilege", "@@os", "@@resources", "@@users", "@@packages", "@@installed", "@@services", "@@paths", "@@volumes", "@@compose", "@@containers", "@@end"} {
		if !strings.Contains(script, section) {
			t.Fatalf("capture script is missing %s", section)
		}
	}
	for _, path := range CandidatePaths() {
		if !strings.Contains(script, "'/"+path+"'") {
			t.Fatalf("capture script does not measure /%s", path)
		}
	}
}

func TestPrepScriptFallsBackPerPackage(t *testing.T) {
	script := PrepScript([]User{{Name: "deploy", UID: 1000}}, []string{"nginx", "docker-ce"})
	if !strings.Contains(script, "useradd --create-home --shell /bin/bash --uid 1000 'deploy'") {
		t.Fatalf("user creation missing:\n%s", script)
	}
	if !strings.Contains(script, "install -y 'nginx' 'docker-ce'") {
		t.Fatalf("bulk install missing:\n%s", script)
	}
	if !strings.Contains(script, MarkerPackageFailed) {
		t.Fatalf("per-package fallback missing:\n%s", script)
	}
	if !strings.Contains(script, "DEBIAN_FRONTEND=noninteractive") {
		t.Fatalf("apt must be non-interactive:\n%s", script)
	}
	// Synced repo files may pin the source's CPU arch (Apple Silicon sandbox →
	// Intel VPS is the common pairing); the pin must be stripped before update.
	if !strings.Contains(script, "arch=") || !strings.Contains(script, "/^Architectures:/d") {
		t.Fatalf("arch pin normalization missing:\n%s", script)
	}
}

// The excludes are the safety contract of the whole feature: a push must not
// be able to lock the user out of their VPS or leak sandbox SSH keys onto it.
func TestTarCommandsKeepTheSafetyExcludes(t *testing.T) {
	create := TarCreateCommand([]string{"root", "etc/nginx"})
	extract := TarExtractCommand()
	for _, cmd := range []string{create, extract} {
		for _, exclude := range []string{"*/.ssh", "etc/systemd/system/*.wants", "etc/apt/sources.list.d/ubuntu.sources"} {
			if !strings.Contains(cmd, "--exclude='"+exclude+"'") {
				t.Fatalf("missing exclude %q in:\n%s", exclude, cmd)
			}
		}
		if !strings.Contains(cmd, "--numeric-owner") {
			t.Fatalf("missing --numeric-owner in:\n%s", cmd)
		}
	}
	// Live filesystems change mid-read; exit 1 must be tolerated on create.
	if !strings.Contains(create, "if [ $rc -eq 1 ]; then rc=0; fi") {
		t.Fatalf("tar create must tolerate exit 1:\n%s", create)
	}
}

func TestServiceScriptSkipsMissingUnits(t *testing.T) {
	script := ServiceScript([]string{"myapp.service"})
	if !strings.Contains(script, MarkerServiceSkip) || !strings.Contains(script, MarkerServiceFailed) {
		t.Fatalf("service markers missing:\n%s", script)
	}
	if !strings.Contains(script, "daemon-reload") {
		t.Fatalf("daemon-reload missing:\n%s", script)
	}
}

func TestComposeUpScriptOnlyStartsRunningProjects(t *testing.T) {
	script := ComposeUpScript([]ComposeProject{
		{Name: "live", ConfigFiles: []string{"/root/live/docker-compose.yml"}, Running: true},
		{Name: "parked", ConfigFiles: []string{"/opt/parked/compose.yml"}, Running: false},
	})
	if !strings.Contains(script, "-p 'live'") {
		t.Fatalf("running project missing:\n%s", script)
	}
	if strings.Contains(script, "parked") {
		t.Fatalf("stopped project must not be started:\n%s", script)
	}
}

func TestVolumeScriptsQuoteNames(t *testing.T) {
	create := VolumeTarCreate("app_data")
	extract := VolumeTarExtract("app_data")
	if !strings.Contains(create, "'app_data'") || !strings.Contains(extract, "docker volume create 'app_data'") {
		t.Fatalf("volume name quoting:\n%s\n%s", create, extract)
	}
}

func TestShellQuote(t *testing.T) {
	if got := shellQuote("it's"); got != `'it'\''s'` {
		t.Fatalf("shellQuote: %s", got)
	}
}
