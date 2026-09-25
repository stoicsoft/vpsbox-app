package registry

import (
	"testing"
	"time"

	"github.com/stoicsoft/vpsbox/internal/config"
)

func TestStoreRoundTrip(t *testing.T) {
	t.Setenv(config.EnvHome, t.TempDir())
	paths, err := config.DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}

	store := NewStore(paths)
	instance := Instance{
		Name:           "dev-1",
		Status:         "running",
		Host:           "192.168.64.4",
		Port:           22,
		Username:       "root",
		PrivateKeyPath: "/tmp/dev-1",
		Image:          "24.04",
		CreatedAt:      time.Now().UTC(),
		DomainBase:     "192-168-64-4.sslip.io",
		ScenarioID:     "migration-run",
		ScenarioRole:   "easypanel-source",
	}
	if err := store.UpsertInstance(instance); err != nil {
		t.Fatal(err)
	}

	got, err := store.GetInstance("dev-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != instance.Name || got.Host != instance.Host || got.DomainBase != instance.DomainBase || got.ScenarioID != instance.ScenarioID {
		t.Fatalf("unexpected instance: %#v", got)
	}
}
