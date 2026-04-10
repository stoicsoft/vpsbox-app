package registry

import (
	"encoding/json"
	"errors"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/stoicsoft/vpsbox/internal/config"
)

const currentVersion = 1

type Instance struct {
	Name             string    `json:"name"`
	Status           string    `json:"status"`
	Host             string    `json:"host"`
	Hostname         string    `json:"hostname,omitempty"`
	Port             int       `json:"port"`
	Username         string    `json:"username"`
	PrivateKeyPath   string    `json:"private_key_path"`
	PublicKeyPath    string    `json:"public_key_path,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
	Image            string    `json:"image"`
	Labels           []string  `json:"labels,omitempty"`
	Backend          string    `json:"backend,omitempty"`
	CPUs             int       `json:"cpus,omitempty"`
	MemoryGB         int       `json:"memory_gb,omitempty"`
	DiskGB           int       `json:"disk_gb,omitempty"`
	CertPath         string    `json:"cert_path,omitempty"`
	CertKeyPath      string    `json:"cert_key_path,omitempty"`
	SandboxMarker    string    `json:"sandbox_marker,omitempty"`
	QuickTunnelURL   string    `json:"quick_tunnel_url,omitempty"`
	CloudInitPath    string    `json:"cloud_init_path,omitempty"`
	SnapshotsEnabled bool      `json:"snapshots_enabled,omitempty"`
}

type Share struct {
	Name      string     `json:"name"`
	URL       string     `json:"url"`
	TargetURL string     `json:"target_url"`
	Provider  string     `json:"provider"`
	PID       int        `json:"pid"`
	LogPath   string     `json:"log_path,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

type AuthSession struct {
	Email     string     `json:"email"`
	Token     string     `json:"token,omitempty"`
	APIBase   string     `json:"api_base,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

type instancesFile struct {
	Version   int        `json:"version"`
	Instances []Instance `json:"instances"`
}

type sharesFile struct {
	Version int     `json:"version"`
	Shares  []Share `json:"shares"`
}

type authFile struct {
	Version int         `json:"version"`
	Session AuthSession `json:"session"`
}

type Store struct {
	paths config.Paths
	mu    sync.Mutex
}

func NewStore(paths config.Paths) *Store {
	return &Store{paths: paths}
}

func (s *Store) LoadInstances() ([]Instance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var f instancesFile
	if err := readJSON(s.paths.RegistryPath, &f); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	sort.Slice(f.Instances, func(i, j int) bool { return f.Instances[i].Name < f.Instances[j].Name })
	return f.Instances, nil
}

func (s *Store) SaveInstances(instances []Instance) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sort.Slice(instances, func(i, j int) bool { return instances[i].Name < instances[j].Name })
	return writeJSON(s.paths.RegistryPath, instancesFile{
		Version:   currentVersion,
		Instances: instances,
	})
}

func (s *Store) UpsertInstance(instance Instance) error {
	instances, err := s.LoadInstances()
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	if instance.CreatedAt.IsZero() {
		instance.CreatedAt = now
	}
	instance.UpdatedAt = now

	replaced := false
	for i := range instances {
		if instances[i].Name == instance.Name {
			instance.CreatedAt = instances[i].CreatedAt
			instances[i] = instance
			replaced = true
			break
		}
	}

	if !replaced {
		instances = append(instances, instance)
	}

	return s.SaveInstances(instances)
}

func (s *Store) DeleteInstance(name string) error {
	instances, err := s.LoadInstances()
	if err != nil {
		return err
	}

	filtered := make([]Instance, 0, len(instances))
	for _, inst := range instances {
		if inst.Name != name {
			filtered = append(filtered, inst)
		}
	}

	return s.SaveInstances(filtered)
}

func (s *Store) GetInstance(name string) (*Instance, error) {
	instances, err := s.LoadInstances()
	if err != nil {
		return nil, err
	}

	for _, inst := range instances {
		if inst.Name == name {
			copy := inst
			return &copy, nil
		}
	}

	return nil, os.ErrNotExist
}

func (s *Store) LoadShares() ([]Share, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var f sharesFile
	if err := readJSON(s.paths.SharesPath, &f); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	sort.Slice(f.Shares, func(i, j int) bool { return f.Shares[i].CreatedAt.After(f.Shares[j].CreatedAt) })
	return f.Shares, nil
}

func (s *Store) SaveShares(shares []Share) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeJSON(s.paths.SharesPath, sharesFile{
		Version: currentVersion,
		Shares:  shares,
	})
}

func (s *Store) UpsertShare(share Share) error {
	shares, err := s.LoadShares()
	if err != nil {
		return err
	}

	replaced := false
	for i := range shares {
		if shares[i].Name == share.Name {
			shares[i] = share
			replaced = true
			break
		}
	}
	if !replaced {
		shares = append(shares, share)
	}

	return s.SaveShares(shares)
}

func (s *Store) DeleteShare(name string) error {
	shares, err := s.LoadShares()
	if err != nil {
		return err
	}

	filtered := make([]Share, 0, len(shares))
	for _, share := range shares {
		if share.Name != name {
			filtered = append(filtered, share)
		}
	}

	return s.SaveShares(filtered)
}

func (s *Store) SaveAuth(session AuthSession) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeJSON(s.paths.AuthPath, authFile{
		Version: currentVersion,
		Session: session,
	})
}

func (s *Store) LoadAuth() (*AuthSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var f authFile
	if err := readJSON(s.paths.AuthPath, &f); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	return &f.Session, nil
}

func (s *Store) DeleteAuth() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(s.paths.AuthPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func readJSON(path string, out any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	return json.Unmarshal(data, out)
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}
