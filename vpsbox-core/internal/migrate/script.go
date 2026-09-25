package migrate

import (
	"fmt"
	"strings"
)

// sudoPreamble makes every script work both as root (the sandbox) and as a
// sudo-capable user (a typical VPS account). sudo -n rather than sudo: a
// password prompt inside a non-interactive SSH session hangs forever.
const sudoPreamble = `SUDO=""
if [ "$(id -u)" != "0" ]; then SUDO="sudo -n"; fi
`

// aptCommand wraps apt-get so it can never stop to ask a question: no dpkg
// conffile prompts (existing files win — the file sync that follows delivers
// the source's config anyway) and no needrestart menus on newer Ubuntu.
const aptCommand = `$SUDO env DEBIAN_FRONTEND=noninteractive NEEDRESTART_MODE=a apt-get -o Dpkg::Options::=--force-confdef -o Dpkg::Options::=--force-confold`

// Markers the apply scripts print so the caller can collect partial failures
// without parsing free-form tool output.
const (
	MarkerPackageFailed = "##PKGFAIL##"
	MarkerServiceFailed = "##SVCFAIL##"
	MarkerServiceSkip   = "##SVCSKIP##"
	MarkerComposeFailed = "##COMPOSEFAIL##"
	MarkerComposeSkip   = "##COMPOSESKIP##"
)

// CaptureScript is one SSH round trip that answers everything BuildPlan needs
// to know about a machine. Sections are fenced the way liveAppsScript fences
// them, so the output parses deterministically no matter what the tools print.
func CaptureScript() string {
	var b strings.Builder
	b.WriteString("set +e\n")
	b.WriteString(sudoPreamble)
	b.WriteString(`echo '@@privilege'
if [ "$(id -u)" = "0" ]; then echo root; elif sudo -n true 2>/dev/null; then echo sudo; else echo none; fi
echo '@@os'
. /etc/os-release 2>/dev/null && echo "$ID|$VERSION_ID|$PRETTY_NAME"
echo '@@resources'
echo "cpus $(nproc 2>/dev/null)"
echo "memmb $(free -m 2>/dev/null | awk '/^Mem:/{print $2}')"
echo "diskkb $(df -k / 2>/dev/null | awk 'NR==2{print $3}')"
echo '@@users'
awk -F: '$3 >= 1000 && $3 < 65534 {print $3" "$1}' /etc/passwd 2>/dev/null
echo '@@packages'
apt-mark showmanual 2>/dev/null | sort
echo '@@installed'
dpkg-query -W -f='${binary:Package}\n' 2>/dev/null | sort
echo '@@services'
systemctl list-unit-files --type=service --state=enabled --no-legend --plain 2>/dev/null | awk '{print $1}' | sort
echo '@@paths'
`)
	for _, path := range CandidatePaths() {
		fmt.Fprintf(&b, `if [ -e %s ]; then s=$($SUDO du -sk %s 2>/dev/null | cut -f1); echo "${s:-0} %s"; fi
`, shellQuote("/"+path), shellQuote("/"+path), path)
	}
	b.WriteString(`echo '@@volumes'
if command -v docker >/dev/null 2>&1; then
  $SUDO docker volume ls -q 2>/dev/null | while read -r v; do
    mp=$($SUDO docker volume inspect -f '{{.Mountpoint}}' "$v" 2>/dev/null)
    s=0
    [ -n "$mp" ] && s=$($SUDO du -sk "$mp" 2>/dev/null | cut -f1)
    echo "${s:-0} $v"
  done
fi
echo '@@compose'
if command -v docker >/dev/null 2>&1; then $SUDO docker compose ls -a --format json 2>/dev/null; fi
echo '@@containers'
if command -v docker >/dev/null 2>&1; then $SUDO docker ps --format '{{.Names}}' 2>/dev/null; fi
echo '@@end'
`)
	return b.String()
}

// PrepScript readies the destination: missing users first (so the file sync
// lands on accounts that exist), then the package delta. Bulk install first
// for speed; if the bulk fails, packages retry one by one so a single
// unavailable package costs a marker line instead of the whole migration.
func PrepScript(users []User, packages []string) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	b.WriteString(sudoPreamble)
	for _, user := range users {
		name := shellQuote(user.Name)
		fmt.Fprintf(&b, "id -u %s >/dev/null 2>&1 || $SUDO useradd --create-home --shell /bin/bash --uid %d %s || $SUDO useradd --create-home --shell /bin/bash %s\n",
			name, user.UID, name, name)
	}
	if len(packages) > 0 {
		quoted := make([]string, 0, len(packages))
		for _, pkg := range packages {
			quoted = append(quoted, shellQuote(pkg))
		}
		list := strings.Join(quoted, " ")
		// The synced repository files may pin the source's CPU architecture
		// (docker's install writes `deb [arch=arm64 ...]` on an Apple Silicon
		// sandbox). Left alone, an Intel VPS would look for arm64 packages and
		// find nothing — the single most common push is exactly that pairing.
		// Stripping the pin restores apt's default: the machine's own arch.
		b.WriteString(`if ls /etc/apt/sources.list.d/*.list >/dev/null 2>&1; then
  $SUDO sed -E -i 's/arch=[a-zA-Z0-9,]+ ?//g; s/\[ +/[/g; s/ +\]/]/g; s/\[\] *//g' /etc/apt/sources.list.d/*.list
fi
if ls /etc/apt/sources.list.d/*.sources >/dev/null 2>&1; then
  $SUDO sed -i '/^Architectures:/d' /etc/apt/sources.list.d/*.sources
fi
`)
		// A single broken third-party repo must not sink the migration; if the
		// index is genuinely unusable the installs below will say so per package.
		fmt.Fprintf(&b, "%s update || echo 'apt update reported problems; continuing'\n", aptCommand)
		fmt.Fprintf(&b, "if ! %s install -y %s; then\n", aptCommand, list)
		fmt.Fprintf(&b, "  for p in %s; do\n", list)
		fmt.Fprintf(&b, "    %s install -y \"$p\" || echo '%s' \"$p\"\n", aptCommand, MarkerPackageFailed)
		b.WriteString("  done\nfi\n")
	}
	return b.String()
}

// TarCreateCommand streams the given paths as a gzip tarball on stdout. No
// `set -e`: on a live machine files change and vanish mid-read, which GNU tar
// reports as exit 1 — that is a fact of life, not a failure.
func TarCreateCommand(paths []string) string {
	var b strings.Builder
	b.WriteString(sudoPreamble)
	b.WriteString("$SUDO tar -czf - --numeric-owner --ignore-failed-read --warning=no-file-changed --warning=no-file-removed")
	for _, exclude := range tarExcludes {
		b.WriteString(" --exclude=" + shellQuote(exclude))
	}
	b.WriteString(" -C /")
	for _, path := range paths {
		b.WriteString(" " + shellQuote(path))
	}
	b.WriteString("\nrc=$?\nif [ $rc -eq 1 ]; then rc=0; fi\nexit $rc\n")
	return b.String()
}

// TarExtractCommand unpacks a tar stream from stdin onto /. The excludes are
// repeated here on purpose: even a hand-crafted stream cannot plant SSH keys
// or server config on the destination.
func TarExtractCommand() string {
	var b strings.Builder
	b.WriteString(sudoPreamble)
	b.WriteString("$SUDO tar -xzf - --numeric-owner --overwrite")
	for _, exclude := range tarExcludes {
		b.WriteString(" --exclude=" + shellQuote(exclude))
	}
	b.WriteString(" -C /\n")
	return b.String()
}

// ServiceScript enables and restarts the planned units on the destination.
// Best-effort per unit: one unit that will not start should not abandon the
// rest, so failures come back as marker lines.
func ServiceScript(units []string) string {
	var b strings.Builder
	b.WriteString("set +e\n")
	b.WriteString(sudoPreamble)
	b.WriteString("$SUDO systemctl daemon-reload\n")
	for _, unit := range units {
		quoted := shellQuote(unit)
		fmt.Fprintf(&b, "if [ -n \"$($SUDO systemctl list-unit-files --no-legend %s 2>/dev/null)\" ]; then\n", quoted)
		fmt.Fprintf(&b, "  $SUDO systemctl enable %s >/dev/null 2>&1 || echo '%s' enable %s\n", quoted, MarkerServiceFailed, quoted)
		fmt.Fprintf(&b, "  $SUDO systemctl restart %s || echo '%s' restart %s\n", quoted, MarkerServiceFailed, quoted)
		fmt.Fprintf(&b, "else\n  echo '%s' %s\nfi\n", MarkerServiceSkip, quoted)
	}
	b.WriteString("exit 0\n")
	return b.String()
}

// StopContainersScript quiesces docker before volume data is replaced —
// restoring underneath a running database is how archives end up corrupt.
// Compose projects come back in ComposeUpScript; anything else stays stopped
// and the plan says so.
func StopContainersScript() string {
	return "set +e\n" + sudoPreamble + `if command -v docker >/dev/null 2>&1; then
  ids=$($SUDO docker ps -q 2>/dev/null)
  if [ -n "$ids" ]; then $SUDO docker stop $ids >/dev/null; fi
fi
exit 0
`
}

// VolumeTarCreate streams one docker volume's contents from the source.
func VolumeTarCreate(volume string) string {
	quoted := shellQuote(volume)
	return sudoPreamble + fmt.Sprintf(`mp=$($SUDO docker volume inspect -f '{{.Mountpoint}}' %s)
$SUDO tar -czf - --numeric-owner --ignore-failed-read --warning=no-file-changed --warning=no-file-removed -C "$mp" .
rc=$?
if [ $rc -eq 1 ]; then rc=0; fi
exit $rc
`, quoted)
}

// VolumeTarExtract creates the volume on the destination if needed and
// replaces its contents with the incoming stream.
func VolumeTarExtract(volume string) string {
	quoted := shellQuote(volume)
	return "set -e\n" + sudoPreamble + fmt.Sprintf(`$SUDO docker volume create %s >/dev/null
mp=$($SUDO docker volume inspect -f '{{.Mountpoint}}' %s)
$SUDO tar -xzf - --numeric-owner --overwrite -C "$mp"
`, quoted, quoted)
}

// ComposeUpScript restarts the compose projects that were running on the
// source. Projects that were present but stopped keep their files and volumes
// and stay stopped — starting something its owner had stopped is not a
// migration, it is a surprise.
func ComposeUpScript(projects []ComposeProject) string {
	var b strings.Builder
	b.WriteString("set +e\n")
	b.WriteString(sudoPreamble)
	for _, project := range projects {
		if !project.Running || len(project.ConfigFiles) == 0 {
			continue
		}
		name := shellQuote(project.Name)
		files := ""
		for _, file := range project.ConfigFiles {
			files += " -f " + shellQuote(file)
		}
		fmt.Fprintf(&b, "if [ -f %s ]; then\n", shellQuote(project.ConfigFiles[0]))
		fmt.Fprintf(&b, "  $SUDO docker compose -p %s%s up -d || echo '%s' %s\n", name, files, MarkerComposeFailed, name)
		fmt.Fprintf(&b, "else\n  echo '%s' %s\nfi\n", MarkerComposeSkip, name)
	}
	b.WriteString("exit 0\n")
	return b.String()
}

// shellQuote single-quotes a value for POSIX shell, the only quoting form
// with no surprises left in it.
func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
