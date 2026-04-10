package domain

import (
	"strings"
	"testing"
)

func TestReplaceManagedSection(t *testing.T) {
	original := "127.0.0.1 localhost\n\n# >>> vpsbox >>>\n10.0.0.2 old.vpsbox.local\n# <<< vpsbox <<<\n"
	updated := replaceManagedSection(original, "# >>> vpsbox >>>\n10.0.0.3 new.vpsbox.local\n# <<< vpsbox <<<\n")
	if want := "10.0.0.3 new.vpsbox.local"; !strings.Contains(updated, want) {
		t.Fatalf("expected managed entry %q in %q", want, updated)
	}
	if strings.Contains(updated, "10.0.0.2 old.vpsbox.local") {
		t.Fatalf("expected old managed entry to be removed: %q", updated)
	}
}

func TestNamesForInstance(t *testing.T) {
	names := NamesForInstance("dev-1")
	if names.Hostname != "dev-1.vpsbox.local" {
		t.Fatalf("unexpected hostname: %s", names.Hostname)
	}
}
