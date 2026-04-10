package prep

import (
	"bytes"
	"fmt"
	"os"
	"text/template"
)

type CloudInitData struct {
	InstanceName string
	Hostname     string
	User         string
	PublicKey    string
}

const cloudInitTemplate = `#cloud-config
package_update: true
package_upgrade: false
packages:
  - curl
  - wget
  - git
  - htop
  - unzip
  - ufw
  - fail2ban
  - ca-certificates
  - gnupg
  - lsb-release
users:
  - default
ssh_authorized_keys:
  - {{ .PublicKey }}
write_files:
  - path: /usr/local/bin/vpsbox-prep.sh
    permissions: '0755'
    content: |
      #!/usr/bin/env bash
      set -euxo pipefail
      export DEBIAN_FRONTEND=noninteractive
      install -m 0755 -d /etc/apt/keyrings
      curl -fsSL https://download.docker.com/linux/ubuntu/gpg | gpg --dearmor -o /etc/apt/keyrings/docker.gpg
      chmod a+r /etc/apt/keyrings/docker.gpg
      . /etc/os-release
      echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu ${VERSION_CODENAME} stable" > /etc/apt/sources.list.d/docker.list
      apt-get update
      apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
      hostnamectl set-hostname {{ .Hostname }}
      # Enable root SSH access (matches real VPS providers).
      mkdir -p /root/.ssh
      cp /home/ubuntu/.ssh/authorized_keys /root/.ssh/authorized_keys 2>/dev/null || true
      chmod 700 /root/.ssh
      chmod 600 /root/.ssh/authorized_keys
      chown -R root:root /root/.ssh
      cp /etc/ssh/sshd_config /etc/ssh/sshd_config.vpsbox.bak
      sed -i -e 's/^#\?PasswordAuthentication.*/PasswordAuthentication no/' /etc/ssh/sshd_config
      sed -i -e 's/^#\?PermitRootLogin.*/PermitRootLogin yes/' /etc/ssh/sshd_config
      sed -i -e 's/^#\?PubkeyAuthentication.*/PubkeyAuthentication yes/' /etc/ssh/sshd_config
      systemctl restart ssh || systemctl restart sshd || true
      printf 'instance=%s\nhostname=%s\nuser=%s\n' '{{ .InstanceName }}' '{{ .Hostname }}' '{{ .User }}' > /etc/vpsbox-info
runcmd:
  - [ bash, -lc, "/usr/local/bin/vpsbox-prep.sh" ]
`

func RenderCloudInit(data CloudInitData) ([]byte, error) {
	tpl, err := template.New("cloud-init").Parse(cloudInitTemplate)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	if err := tpl.Execute(&buf, data); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func WriteCloudInit(path string, data CloudInitData) error {
	content, err := RenderCloudInit(data)
	if err != nil {
		return fmt.Errorf("render cloud-init: %w", err)
	}
	return os.WriteFile(path, content, 0o644)
}
