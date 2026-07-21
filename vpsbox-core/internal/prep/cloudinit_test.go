package prep

import (
	"strings"
	"testing"
)

func TestRenderCloudInitPreservesPermissionAsExplicitString(t *testing.T) {
	content, err := RenderCloudInit(CloudInitData{
		InstanceName: "test-vps",
		Hostname:     "test-vps.vpsbox.local",
		User:         "root",
		PublicKey:    "ssh-ed25519 AAAA test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "permissions: !!str '0755'") {
		t.Fatalf("cloud-init permissions must retain an explicit string tag:\n%s", content)
	}
}
