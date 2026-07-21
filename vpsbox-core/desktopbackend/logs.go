package desktopbackend

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	serverLogSectionPrefix = "__VPSBOX_LOG_SECTION__="
	serverLogSourcePrefix  = "__VPSBOX_LOG_SOURCE__="
)

// serverDiagnosticsCommand is intentionally read-only. It collects a bounded
// journal tail and snapshots of the server's current network and Docker state.
const serverDiagnosticsCommand = `
set +e
export LC_ALL=C
printf '` + serverLogSectionPrefix + `system\n'
journalctl -r -n 160 --no-pager -o json 2>&1
printf '` + serverLogSectionPrefix + `network\n'
ss -H -tunap 2>&1
printf '` + serverLogSectionPrefix + `route\n'
ip -o route show table all 2>&1
printf '` + serverLogSectionPrefix + `docker\n'
if command -v docker >/dev/null 2>&1; then
  printf '` + serverLogSourcePrefix + `docker\n'
  docker ps -a --format '{{.Names}}\t{{.Image}}\t{{.Ports}}\t{{.Status}}' 2>&1 | head -n 100
  docker ps --format '{{.Names}}' 2>/dev/null | head -n 20 | while IFS= read -r container; do
    [ -n "$container" ] || continue
    printf '` + serverLogSourcePrefix + `%s\n' "$container"
    docker logs --timestamps --since 15m --tail 30 "$container" 2>&1
  done
  printf '` + serverLogSourcePrefix + `docker events\n'
  docker events --since 15m --until "$(date -Iseconds)" --filter type=container --filter type=network --format '{{.Time}}\t{{.Type}}\t{{.Action}}\t{{.Actor.Attributes.name}}' 2>&1 | tail -n 100
else
  printf 'Docker is not installed\n'
fi
`

type ServerLogs struct {
	FetchedAt string           `json:"fetchedAt"`
	Entries   []ServerLogEntry `json:"entries"`
}

type ServerLogEntry struct {
	ID        string `json:"id"`
	Category  string `json:"category"`
	Timestamp string `json:"timestamp,omitempty"`
	Level     string `json:"level"`
	Source    string `json:"source"`
	Message   string `json:"message"`
}

type journalRecord struct {
	RealtimeTimestamp string `json:"__REALTIME_TIMESTAMP"`
	Priority          string `json:"PRIORITY"`
	SyslogIdentifier  string `json:"SYSLOG_IDENTIFIER"`
	SystemdUnit       string `json:"_SYSTEMD_UNIT"`
	Command           string `json:"_COMM"`
	Message           any    `json:"MESSAGE"`
}

func (a *App) GetServerLogs(name string) (ServerLogs, error) {
	if a.manager == nil {
		return ServerLogs{}, fmt.Errorf("desktop backend is not ready")
	}
	if strings.TrimSpace(name) == "" {
		return ServerLogs{}, fmt.Errorf("sandbox name is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	stdout, stderr, err := a.manager.RunRemote(ctx, name, serverDiagnosticsCommand)
	if err != nil {
		if detail := strings.TrimSpace(stderr); detail != "" {
			return ServerLogs{}, fmt.Errorf("read server logs: %w: %s", err, detail)
		}
		return ServerLogs{}, fmt.Errorf("read server logs: %w", err)
	}

	return ServerLogs{
		FetchedAt: time.Now().UTC().Format(time.RFC3339),
		Entries:   parseServerLogOutput(stdout),
	}, nil
}

func parseServerLogOutput(output string) []ServerLogEntry {
	entries := make([]ServerLogEntry, 0, 256)
	category := "system"
	source := logSourceForCategory(category)
	scanner := bufio.NewScanner(strings.NewReader(output))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, serverLogSectionPrefix) {
			category = strings.TrimPrefix(line, serverLogSectionPrefix)
			source = logSourceForCategory(category)
			continue
		}
		if strings.HasPrefix(line, serverLogSourcePrefix) {
			source = strings.TrimPrefix(line, serverLogSourcePrefix)
			continue
		}

		entry := ServerLogEntry{
			Category: category,
			Level:    "info",
			Source:   source,
			Message:  line,
		}
		if category == "system" {
			entry = parseJournalEntry(line)
		} else if category == "docker" {
			entry = parseDockerEntry(line, source)
		}
		entry.ID = fmt.Sprintf("%s-%d", entry.Category, len(entries)+1)
		entries = append(entries, entry)
	}

	return entries
}

func parseDockerEntry(line, source string) ServerLogEntry {
	entry := ServerLogEntry{
		Category: "docker",
		Level:    "info",
		Source:   firstNonEmpty(source, "docker"),
		Message:  line,
	}

	if fields := strings.SplitN(line, " ", 2); len(fields) == 2 {
		if timestamp, err := time.Parse(time.RFC3339Nano, fields[0]); err == nil {
			entry.Timestamp = timestamp.UTC().Format(time.RFC3339Nano)
			entry.Message = fields[1]
			return entry
		}
	}
	if fields := strings.SplitN(line, "\t", 2); len(fields) == 2 {
		if seconds, err := strconv.ParseInt(fields[0], 10, 64); err == nil {
			entry.Timestamp = time.Unix(seconds, 0).UTC().Format(time.RFC3339)
			entry.Message = fields[1]
		}
	}

	return entry
}

func parseJournalEntry(line string) ServerLogEntry {
	entry := ServerLogEntry{
		Category: "system",
		Level:    "info",
		Source:   "journal",
		Message:  line,
	}

	var record journalRecord
	if err := json.Unmarshal([]byte(line), &record); err != nil {
		return entry
	}

	entry.Timestamp = journalTimestamp(record.RealtimeTimestamp)
	entry.Level = journalLevel(record.Priority)
	entry.Source = firstNonEmpty(record.SyslogIdentifier, record.SystemdUnit, record.Command, "journal")
	switch message := record.Message.(type) {
	case string:
		entry.Message = message
	case nil:
		entry.Message = "—"
	default:
		if encoded, err := json.Marshal(message); err == nil {
			entry.Message = string(encoded)
		}
	}

	return entry
}

func journalTimestamp(value string) string {
	microseconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return ""
	}
	return time.UnixMicro(microseconds).UTC().Format(time.RFC3339Nano)
}

func journalLevel(priority string) string {
	value, err := strconv.Atoi(priority)
	if err != nil {
		return "info"
	}
	switch {
	case value <= 3:
		return "error"
	case value == 4:
		return "warning"
	case value == 7:
		return "debug"
	default:
		return "info"
	}
}

func logSourceForCategory(category string) string {
	switch category {
	case "network":
		return "socket"
	case "route":
		return "routing table"
	case "docker":
		return "docker"
	default:
		return "journal"
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
