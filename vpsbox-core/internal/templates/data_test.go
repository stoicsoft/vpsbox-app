package templates

import (
	"strings"
	"testing"
)

func TestCatalogEntriesAreWellFormed(t *testing.T) {
	seenID := map[string]bool{}
	// Only apps can coexist on one sandbox, so only their host ports must be
	// unique. Platforms each take over the whole VM and legitimately share
	// conventional ports (Dokploy and CapRover both use 3000).
	seenAppPort := map[int]string{}
	for _, tpl := range catalog {
		if tpl.ID == "" || tpl.Name == "" || tpl.Summary == "" {
			t.Fatalf("template %q is missing id/name/summary: %+v", tpl.ID, tpl)
		}
		if seenID[tpl.ID] {
			t.Fatalf("duplicate template id %q", tpl.ID)
		}
		seenID[tpl.ID] = true

		if tpl.Port <= 0 {
			t.Fatalf("template %q has no port", tpl.ID)
		}
		if tpl.Category == CategoryApp {
			if other, clash := seenAppPort[tpl.Port]; clash {
				t.Fatalf("apps %q and %q both use host port %d", other, tpl.ID, tpl.Port)
			}
			seenAppPort[tpl.Port] = tpl.ID
		}

		if strings.TrimSpace(tpl.Install) == "" {
			t.Fatalf("template %q has an empty install script", tpl.ID)
		}
		if tpl.Category != CategoryPlatform && tpl.Category != CategoryApp {
			t.Fatalf("template %q has an unknown category %q", tpl.ID, tpl.Category)
		}
	}
}

func TestTemplatesMapMatchesCatalog(t *testing.T) {
	if len(Templates) != len(catalog) {
		t.Fatalf("Templates map has %d entries, catalog has %d", len(Templates), len(catalog))
	}
	for _, tpl := range catalog {
		got, ok := Templates[tpl.ID]
		if !ok {
			t.Fatalf("catalog template %q is missing from the Templates map", tpl.ID)
		}
		if got.Name != tpl.Name {
			t.Fatalf("Templates[%q].Name = %q, want %q", tpl.ID, got.Name, tpl.Name)
		}
	}
}

func TestListPutsPlatformsFirstAndStartersLast(t *testing.T) {
	list := List()
	if len(list) != len(catalog) {
		t.Fatalf("List returned %d entries, want %d", len(list), len(catalog))
	}

	lastPlatform, firstStarter := -1, len(list)
	for i, tpl := range list {
		if tpl.Category == CategoryPlatform {
			lastPlatform = i
		}
		if tpl.Kind == KindStarter && i < firstStarter {
			firstStarter = i
		}
	}
	// Every platform sorts ahead of every non-platform.
	for i, tpl := range list {
		if tpl.Category != CategoryPlatform && i < lastPlatform {
			t.Fatalf("non-platform %q at %d appears before a platform at %d", tpl.ID, i, lastPlatform)
		}
	}
	// Starters sort behind ordinary apps.
	for i, tpl := range list {
		if tpl.Category == CategoryApp && tpl.Kind != KindStarter && i > firstStarter {
			t.Fatalf("app %q at %d appears after a starter at %d", tpl.ID, i, firstStarter)
		}
	}
}

func TestNamedPlatformsArePresent(t *testing.T) {
	for _, id := range []string{"coolify", "dokploy", "dokku", "caprover"} {
		tpl, ok := Templates[id]
		if !ok {
			t.Fatalf("expected platform template %q to exist", id)
		}
		if tpl.Category != CategoryPlatform {
			t.Fatalf("template %q should be a platform, got %q", id, tpl.Category)
		}
	}
}

// composeInstall must not expand ${SECRET} on the host: the compose heredoc is
// quoted so the value is interpolated in-VM from .env, never baked into the
// script that gets shipped over SSH.
func TestComposeInstallKeepsSecretsUnexpanded(t *testing.T) {
	script := composeInstall("demo", 8080,
		"printf 'TOKEN=%s\\n' \"$(openssl rand -hex 8)\" > .env\n",
		"services:\n  demo:\n    image: demo\n    environment:\n      - TOKEN=${TOKEN}\n")

	if !strings.Contains(script, "<<'YAML'") {
		t.Fatal("compose heredoc must be quoted so ${VAR} is not host-expanded")
	}
	if !strings.Contains(script, "${TOKEN}") {
		t.Fatal("the ${TOKEN} reference should survive into the compose file")
	}
	if !strings.Contains(script, `cd "$APP_DIR"`) {
		t.Fatal("install should cd into the app dir so compose reads its .env")
	}
	if !strings.Contains(script, "docker compose up -d") {
		t.Fatal("install should bring the stack up")
	}
}
