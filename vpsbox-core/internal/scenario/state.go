package scenario

import "time"

type LabStatus string

const (
	LabApplying LabStatus = "applying"
	LabReady    LabStatus = "ready"
	LabFailed   LabStatus = "failed"
)

type InstanceState struct {
	LogicalName  string `json:"logical_name"`
	InstanceName string `json:"instance_name"`
	Role         string `json:"role"`
	Fixture      string `json:"fixture"`
	Status       string `json:"status"`
	Host         string `json:"host,omitempty"`
	DomainBase   string `json:"domain_base,omitempty"`
	Error        string `json:"error,omitempty"`
}

type LabState struct {
	Version      int             `json:"version"`
	RunID        string          `json:"run_id"`
	ManifestName string          `json:"manifest_name"`
	ManifestPath string          `json:"manifest_path"`
	Status       LabStatus       `json:"status"`
	Instances    []InstanceState `json:"instances"`
	Error        string          `json:"error,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
}
