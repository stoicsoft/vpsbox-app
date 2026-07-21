package desktopbackend

import (
	"testing"
)

func TestParseServerLogOutput(t *testing.T) {
	output := serverLogSectionPrefix + "system\n" +
		`{"__REALTIME_TIMESTAMP":"1721563200123456","PRIORITY":"4","SYSLOG_IDENTIFIER":"kernel","MESSAGE":"link is down"}` + "\n" +
		serverLogSectionPrefix + "network\n" +
		"tcp ESTAB 0 0 10.0.0.2:22 10.0.0.1:51234\n" +
		serverLogSectionPrefix + "route\n" +
		"default via 10.0.0.1 dev eth0\n" +
		serverLogSectionPrefix + "docker\n" +
		serverLogSourcePrefix + "web\n" +
		"2026-07-21T08:00:00.123456789Z GET /health 200\n"

	entries := parseServerLogOutput(output)
	if len(entries) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(entries))
	}

	journal := entries[0]
	if journal.Category != "system" || journal.Level != "warning" || journal.Source != "kernel" {
		t.Fatalf("unexpected journal entry: %+v", journal)
	}
	if journal.Message != "link is down" || journal.Timestamp == "" {
		t.Fatalf("journal fields were not parsed: %+v", journal)
	}

	if entries[1].Category != "network" || entries[1].Source != "socket" {
		t.Fatalf("unexpected network entry: %+v", entries[1])
	}
	if entries[2].Category != "route" || entries[2].Source != "routing table" {
		t.Fatalf("unexpected route entry: %+v", entries[2])
	}
	if entries[3].Source != "web" || entries[3].Timestamp == "" || entries[3].Message != "GET /health 200" {
		t.Fatalf("unexpected Docker entry: %+v", entries[3])
	}
}

func TestParseJournalEntryFallsBackToRawLine(t *testing.T) {
	entry := parseJournalEntry("journal is unavailable")
	if entry.Message != "journal is unavailable" || entry.Source != "journal" {
		t.Fatalf("expected raw fallback, got %+v", entry)
	}
}
