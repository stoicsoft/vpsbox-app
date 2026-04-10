package registry

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// Baseline is a snapshot of the in-VM state captured at checkpoint time.
// It is used by `vpsbox diff`, `vpsbox undo`, and `vpsbox panic` to tell the
// user (and themselves) what changed since the last known-good state.
type Baseline struct {
	Instance   string            `json:"instance"`
	Checkpoint string            `json:"checkpoint"`
	CapturedAt time.Time         `json:"captured_at"`
	Packages   []string          `json:"packages,omitempty"`
	Services   []string          `json:"services,omitempty"`
	Ports      []string          `json:"ports,omitempty"`
	EtcFiles   map[string]string `json:"etc_files,omitempty"`
}

func (s *Store) baselinePath(name string) string {
	return filepath.Join(s.paths.BaseDir, "baselines", name+".json")
}

func (s *Store) SaveBaseline(b Baseline) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := s.baselinePath(b.Instance)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func (s *Store) LoadBaseline(name string) (*Baseline, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var b Baseline
	if err := readJSON(s.baselinePath(name), &b); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	return &b, nil
}

func (s *Store) DeleteBaseline(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(s.baselinePath(name)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
