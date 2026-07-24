package desktopbackend

import (
	"testing"

	"github.com/stoicsoft/vpsbox/internal/templates"
)

func TestListTemplatesMapsEveryCatalogEntry(t *testing.T) {
	app := &App{}
	out := app.ListTemplates()

	if len(out) != len(templates.List()) {
		t.Fatalf("ListTemplates returned %d, want %d", len(out), len(templates.List()))
	}

	// Platforms come first, matching templates.List ordering.
	if out[0].Category != string(templates.CategoryPlatform) {
		t.Fatalf("first entry category = %q, want platform", out[0].Category)
	}

	for _, dt := range out {
		if dt.ID == "" || dt.Name == "" {
			t.Fatalf("mapped template missing id/name: %+v", dt)
		}
		src, ok := templates.Templates[dt.ID]
		if !ok {
			t.Fatalf("mapped template %q not in catalog", dt.ID)
		}
		if dt.Port != src.Port || dt.MinMemoryMB != src.MinMemoryMB {
			t.Fatalf("template %q port/memory mismatch: %+v vs %+v", dt.ID, dt, src)
		}
		if dt.Category != string(src.Category) || dt.Kind != string(src.Kind) {
			t.Fatalf("template %q category/kind mismatch: %+v vs %+v", dt.ID, dt, src)
		}
	}
}

func TestStartDeployValidatesInput(t *testing.T) {
	app := &App{}

	if _, err := app.StartDeploy("", "coolify"); err == nil {
		t.Fatal("expected an error for an empty sandbox name")
	}
	// manager is nil here, so a valid-looking call still fails fast rather than
	// panicking — the readiness guard must come before anything touches it.
	if _, err := app.StartDeploy("demo", "coolify"); err == nil {
		t.Fatal("expected an error when the backend is not ready")
	}
}
