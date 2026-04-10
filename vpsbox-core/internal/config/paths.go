package config

import (
	"os"
	"path/filepath"
)

const (
	EnvHome      = "VPSBOX_HOME"
	EnvHostsPath = "VPSBOX_HOSTS_PATH"
)

type Paths struct {
	BaseDir       string
	KeysDir       string
	CertsDir      string
	StateDir      string
	LogsDir       string
	TmpDir        string
	RegistryPath  string
	SharesPath    string
	AuthPath      string
	KnownHosts    string
	HostsFilePath string
}

func DefaultPaths() (Paths, error) {
	base := os.Getenv(EnvHome)
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return Paths{}, err
		}
		base = filepath.Join(home, ".vpsbox")
	}

	hostsPath := os.Getenv(EnvHostsPath)
	if hostsPath == "" {
		hostsPath = "/etc/hosts"
	}

	return FromBase(base, hostsPath), nil
}

func FromBase(baseDir, hostsPath string) Paths {
	return Paths{
		BaseDir:       baseDir,
		KeysDir:       filepath.Join(baseDir, "keys"),
		CertsDir:      filepath.Join(baseDir, "certs"),
		StateDir:      filepath.Join(baseDir, "state"),
		LogsDir:       filepath.Join(baseDir, "logs"),
		TmpDir:        filepath.Join(baseDir, "tmp"),
		RegistryPath:  filepath.Join(baseDir, "instances.json"),
		SharesPath:    filepath.Join(baseDir, "shares.json"),
		AuthPath:      filepath.Join(baseDir, "auth.json"),
		KnownHosts:    filepath.Join(baseDir, "known_hosts"),
		HostsFilePath: hostsPath,
	}
}

func (p Paths) Ensure() error {
	dirs := []string{
		p.BaseDir,
		p.KeysDir,
		p.CertsDir,
		p.StateDir,
		p.LogsDir,
		p.TmpDir,
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	return nil
}

func (p Paths) KeyPath(name string) string {
	return filepath.Join(p.KeysDir, name)
}

func (p Paths) PublicKeyPath(name string) string {
	return p.KeyPath(name) + ".pub"
}

func (p Paths) CertPath(name string) string {
	return filepath.Join(p.CertsDir, name+".pem")
}

func (p Paths) CertKeyPath(name string) string {
	return filepath.Join(p.CertsDir, name+"-key.pem")
}

func (p Paths) CloudInitPath(name string) string {
	return filepath.Join(p.TmpDir, name+"-cloud-init.yaml")
}

func (p Paths) ShareLogPath(name string) string {
	return filepath.Join(p.LogsDir, "share-"+name+".log")
}
