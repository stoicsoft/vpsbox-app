package scenario

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	APIVersion       = "vpsbox.stoicsoft.com/v1alpha1"
	Kind             = "Lab"
	MaxManifestBytes = 256 * 1024
)

var safeName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,30}$`)

type Manifest struct {
	APIVersion string   `yaml:"apiVersion" json:"apiVersion"`
	Kind       string   `yaml:"kind" json:"kind"`
	Metadata   Metadata `yaml:"metadata" json:"metadata"`
	Spec       Spec     `yaml:"spec" json:"spec"`
}

type Metadata struct {
	Name string `yaml:"name" json:"name"`
}

type Spec struct {
	MaxParallel int            `yaml:"maxParallel" json:"maxParallel"`
	Instances   []InstanceSpec `yaml:"instances" json:"instances"`
}

type InstanceSpec struct {
	Name        string    `yaml:"name" json:"name"`
	Role        string    `yaml:"role" json:"role"`
	Image       string    `yaml:"image" json:"image"`
	Resources   Resources `yaml:"resources" json:"resources"`
	Fixture     string    `yaml:"fixture" json:"fixture"`
	JoinSwarmOf string    `yaml:"joinSwarmOf,omitempty" json:"joinSwarmOf,omitempty"`
	Snapshots   []string  `yaml:"snapshots,omitempty" json:"snapshots,omitempty"`
}

type Resources struct {
	CPUs     int `yaml:"cpus" json:"cpus"`
	MemoryGB int `yaml:"memoryGB" json:"memoryGB"`
	DiskGB   int `yaml:"diskGB" json:"diskGB"`
}

var allowedFixtures = map[string]bool{
	"base/ubuntu-docker":       true,
	"easypanel/mimic":          true,
	"easypanel/mimic-stable":   true,
	"easypanel/mimic-canary":   true,
	"easypanel/mimic-stateful": true,
	"generic/swarm-traefik":    true,
}

func Load(path string) (Manifest, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return Manifest{}, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return Manifest{}, errors.New("lab manifest must be a regular, non-symlink file")
	}
	if info.Size() > MaxManifestBytes {
		return Manifest{}, fmt.Errorf("lab manifest exceeds %d bytes", MaxManifestBytes)
	}

	file, err := os.Open(path)
	if err != nil {
		return Manifest{}, err
	}
	defer file.Close()

	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode lab manifest: %w", err)
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func (m *Manifest) Validate() error {
	if m.APIVersion != APIVersion {
		return fmt.Errorf("unsupported apiVersion %q", m.APIVersion)
	}
	if m.Kind != Kind {
		return fmt.Errorf("unsupported kind %q", m.Kind)
	}
	if !safeName.MatchString(m.Metadata.Name) {
		return errors.New("metadata.name must start with a letter and contain only lowercase letters, numbers, and hyphens (max 31 characters)")
	}
	if m.Spec.MaxParallel == 0 {
		m.Spec.MaxParallel = 1
	}
	if m.Spec.MaxParallel < 1 || m.Spec.MaxParallel > 4 {
		return errors.New("spec.maxParallel must be between 1 and 4")
	}
	if len(m.Spec.Instances) == 0 || len(m.Spec.Instances) > 8 {
		return errors.New("spec.instances must contain between 1 and 8 entries")
	}

	names := make(map[string]bool, len(m.Spec.Instances))
	for i := range m.Spec.Instances {
		instance := &m.Spec.Instances[i]
		if !safeName.MatchString(instance.Name) {
			return fmt.Errorf("instance %d has an invalid name", i+1)
		}
		if names[instance.Name] {
			return fmt.Errorf("duplicate instance name %q", instance.Name)
		}
		names[instance.Name] = true
		if !safeName.MatchString(instance.Role) {
			return fmt.Errorf("instance %q has an invalid role", instance.Name)
		}
		if instance.Image == "" {
			instance.Image = "24.04"
		}
		if instance.Image != "22.04" && instance.Image != "24.04" {
			return fmt.Errorf("instance %q uses unsupported image %q", instance.Name, instance.Image)
		}
		if instance.Resources.CPUs < 1 || instance.Resources.CPUs > 16 {
			return fmt.Errorf("instance %q CPUs must be between 1 and 16", instance.Name)
		}
		if instance.Resources.MemoryGB < 1 || instance.Resources.MemoryGB > 64 {
			return fmt.Errorf("instance %q memoryGB must be between 1 and 64", instance.Name)
		}
		if instance.Resources.DiskGB < 8 || instance.Resources.DiskGB > 500 {
			return fmt.Errorf("instance %q diskGB must be between 8 and 500", instance.Name)
		}
		if !allowedFixtures[instance.Fixture] {
			return fmt.Errorf("instance %q uses unknown fixture %q", instance.Name, instance.Fixture)
		}
		seenSnapshots := map[string]bool{}
		for _, snapshot := range instance.Snapshots {
			if !safeName.MatchString(snapshot) || seenSnapshots[snapshot] {
				return fmt.Errorf("instance %q has an invalid or duplicate snapshot %q", instance.Name, snapshot)
			}
			seenSnapshots[snapshot] = true
		}
	}

	for _, instance := range m.Spec.Instances {
		if instance.JoinSwarmOf == "" {
			continue
		}
		if instance.JoinSwarmOf == instance.Name || !names[instance.JoinSwarmOf] {
			return fmt.Errorf("instance %q has invalid joinSwarmOf %q", instance.Name, instance.JoinSwarmOf)
		}
	}
	return nil
}

func ValidateRunID(runID string) error {
	if !safeName.MatchString(runID) {
		return errors.New("run ID must start with a letter and contain only lowercase letters, numbers, and hyphens (max 31 characters)")
	}
	return nil
}

func InstanceName(runID, logicalName string) (string, error) {
	if err := ValidateRunID(runID); err != nil {
		return "", err
	}
	if !safeName.MatchString(logicalName) {
		return "", errors.New("invalid logical instance name")
	}
	name := strings.Trim(strings.Join([]string{runID, logicalName}, "-"), "-")
	if len(name) > 63 {
		hash := fmt.Sprintf("%x", sha256Short(name))
		name = strings.TrimRight(name[:54], "-") + "-" + hash
	}
	return name, nil
}

func sha256Short(value string) uint32 {
	var hash uint32 = 2166136261
	for _, char := range []byte(value) {
		hash ^= uint32(char)
		hash *= 16777619
	}
	return hash
}

func FixtureNames() []string {
	result := make([]string, 0, len(allowedFixtures))
	for fixture := range allowedFixtures {
		result = append(result, fixture)
	}
	sort.Strings(result)
	return result
}
