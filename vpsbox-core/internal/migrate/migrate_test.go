package migrate

import (
	"strings"
	"testing"
)

func TestParseTarget(t *testing.T) {
	target, err := ParseTarget("root@203.0.113.10")
	if err != nil {
		t.Fatalf("ParseTarget: %v", err)
	}
	if target.User != "root" || target.Host != "203.0.113.10" || target.Port != 22 {
		t.Fatalf("unexpected target: %+v", target)
	}
	if target.Label() != "root@203.0.113.10" {
		t.Fatalf("unexpected label: %s", target.Label())
	}

	for _, bad := range []string{"", "root", "@host", "root@", "root@host with space"} {
		if _, err := ParseTarget(bad); err == nil {
			t.Fatalf("ParseTarget(%q) should fail", bad)
		}
	}
}

const sampleCapture = `@@privilege
root
@@os
ubuntu|24.04|Ubuntu 24.04.1 LTS
@@resources
cpus 2
memmb 3809
diskkb 4194304
@@users
1000 deploy
@@packages
curl
docker-ce
nginx
linux-image-generic
@@installed
adduser
apt
curl
openssh-server
@@services
docker.service
myapp.service
ssh.service
ufw.service
@@paths
120 root
2048 var/www
64 etc/nginx
@@volumes
5120 myapp_data
0 bad name
@@compose
[{"Name":"myapp","Status":"running(2)","ConfigFiles":"/root/myapp/docker-compose.yml"},{"Name":"paused","Status":"exited(1)","ConfigFiles":"/opt/paused/compose.yml"}]
@@containers
myapp-web-1
stray-redis
@@end
`

func TestParseManifest(t *testing.T) {
	m, err := ParseManifest(sampleCapture)
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	if m.Privilege != "root" {
		t.Fatalf("privilege: %q", m.Privilege)
	}
	if m.OS.ID != "ubuntu" || m.OS.VersionID != "24.04" {
		t.Fatalf("os: %+v", m.OS)
	}
	if m.Resources.CPUs != 2 || m.Resources.MemoryMB != 3809 || m.Resources.UsedDiskKB != 4194304 {
		t.Fatalf("resources: %+v", m.Resources)
	}
	if len(m.Users) != 1 || m.Users[0].Name != "deploy" || m.Users[0].UID != 1000 {
		t.Fatalf("users: %+v", m.Users)
	}
	if len(m.Packages) != 4 || len(m.Installed) != 4 {
		t.Fatalf("packages: %v installed: %v", m.Packages, m.Installed)
	}
	if len(m.Paths) != 3 || m.Paths[1].Rel != "var/www" || m.Paths[1].SizeKB != 2048 {
		t.Fatalf("paths: %+v", m.Paths)
	}
	// "bad name" fails volume-name validation and is dropped.
	if len(m.Volumes) != 1 || m.Volumes[0].Name != "myapp_data" || m.Volumes[0].SizeKB != 5120 {
		t.Fatalf("volumes: %+v", m.Volumes)
	}
	if len(m.Compose) != 2 {
		t.Fatalf("compose: %+v", m.Compose)
	}
	if !m.Compose[0].Running || m.Compose[0].Name != "myapp" {
		t.Fatalf("compose[0]: %+v", m.Compose[0])
	}
	if m.Compose[1].Running {
		t.Fatalf("compose[1] should not be running: %+v", m.Compose[1])
	}
	if err := m.Supported(); err != nil {
		t.Fatalf("Supported: %v", err)
	}
}

func TestParseManifestRequiresEndMarker(t *testing.T) {
	if _, err := ParseManifest("@@privilege\nroot\n"); err == nil {
		t.Fatal("truncated capture should fail")
	}
}

func TestParseComposeListNDJSON(t *testing.T) {
	projects := parseComposeList(`{"Name":"a","Status":"running(1)","ConfigFiles":"/x/a.yml,/x/b.yml"}` + "\n" + `{"Name":"b","Status":"exited(0)","ConfigFiles":"/y/c.yml"}`)
	if len(projects) != 2 || len(projects[0].ConfigFiles) != 2 {
		t.Fatalf("ndjson parse: %+v", projects)
	}
}

func TestSupportedRejections(t *testing.T) {
	m := &Manifest{Privilege: "root", OS: OSInfo{ID: "centos", PrettyName: "CentOS"}}
	if err := m.Supported(); err == nil {
		t.Fatal("non-apt OS should be rejected")
	}
	m = &Manifest{Privilege: "none", OS: OSInfo{ID: "ubuntu"}}
	if err := m.Supported(); err == nil {
		t.Fatal("no-privilege user should be rejected")
	}
}

func TestBuildPlan(t *testing.T) {
	src, err := ParseManifest(sampleCapture)
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	dst := &Manifest{
		Privilege: "sudo",
		OS:        OSInfo{ID: "ubuntu", VersionID: "24.04"},
		Installed: []string{"curl", "openssh-server", "nginx"},
		Enabled:   []string{"ssh.service", "nginx.service"},
		Users:     []User{{Name: "admin", UID: 1000}},
	}

	plan := BuildPlan(src, dst, "dev-1", "root@vps")

	// curl is already installed, nginx too; linux-image-generic is machine-level.
	if len(plan.Packages) != 1 || plan.Packages[0] != "docker-ce" {
		t.Fatalf("packages: %v", plan.Packages)
	}
	// ssh enabled both sides, ufw is skip-listed; docker + myapp move.
	if strings.Join(plan.Services, ",") != "docker.service,myapp.service" {
		t.Fatalf("services: %v", plan.Services)
	}
	if len(plan.Users) != 1 || plan.Users[0].Name != "deploy" {
		t.Fatalf("users: %+v", plan.Users)
	}
	if plan.EstimatedKB() != 120+2048+64+5120 {
		t.Fatalf("estimated: %d", plan.EstimatedKB())
	}

	notes := strings.Join(plan.Notes, "\n")
	if !strings.Contains(notes, "linux-image-generic") {
		t.Fatalf("skipped package not mentioned: %s", notes)
	}
	if !strings.Contains(notes, `"stray-redis"`) {
		t.Fatalf("orphan container not mentioned: %s", notes)
	}
	if strings.Contains(notes, `"myapp-web-1"`) {
		t.Fatalf("compose-owned container flagged as orphan: %s", notes)
	}
}

func TestSplitPlanPaths(t *testing.T) {
	apt, data := SplitPlanPaths([]PathInfo{
		{Rel: "root"},
		{Rel: "etc/apt/sources.list.d"},
		{Rel: "var/www"},
		{Rel: "etc/apt/keyrings"},
	})
	if len(apt) != 2 || apt[0].Rel != "etc/apt/sources.list.d" || apt[1].Rel != "etc/apt/keyrings" {
		t.Fatalf("apt group: %+v", apt)
	}
	if len(data) != 2 || data[0].Rel != "root" || data[1].Rel != "var/www" {
		t.Fatalf("data group: %+v", data)
	}
}

func TestBuildPlanSkipsMachineLevelServices(t *testing.T) {
	src := &Manifest{
		Privilege: "root",
		OS:        OSInfo{ID: "ubuntu", VersionID: "24.04"},
		Enabled:   []string{"grub-common.service", "udisks2.service", "myapp.service"},
	}
	dst := &Manifest{Privilege: "root", OS: OSInfo{ID: "ubuntu", VersionID: "24.04"}}
	plan := BuildPlan(src, dst, "a", "b")
	if strings.Join(plan.Services, ",") != "myapp.service" {
		t.Fatalf("services: %v", plan.Services)
	}
}

func TestSandboxSize(t *testing.T) {
	cpus, mem, disk := SandboxSize(Resources{CPUs: 16, MemoryMB: 65536}, 50*1024*1024)
	if cpus != 4 || mem != 8 {
		t.Fatalf("clamps: cpus=%d mem=%d", cpus, mem)
	}
	if disk != 6+100+1 {
		t.Fatalf("disk: %d", disk)
	}
	cpus, mem, disk = SandboxSize(Resources{CPUs: 1, MemoryMB: 512}, 0)
	if cpus != 2 || mem != 2 || disk != 10 {
		t.Fatalf("minimums: cpus=%d mem=%d disk=%d", cpus, mem, disk)
	}
}

func TestImageForOS(t *testing.T) {
	if got := ImageForOS(OSInfo{ID: "ubuntu", VersionID: "26.04"}); got != "26.04" {
		t.Fatalf("26.04 → %s", got)
	}
	if got := ImageForOS(OSInfo{ID: "ubuntu", VersionID: "18.04"}); got != "24.04" {
		t.Fatalf("18.04 → %s", got)
	}
	if got := ImageForOS(OSInfo{ID: "debian", VersionID: "12"}); got != "24.04" {
		t.Fatalf("debian → %s", got)
	}
}
