package backend

import (
	"context"
	"errors"
)

var ErrUnsupported = errors.New("backend unsupported")

type InstanceStatus string

const (
	StatusRunning InstanceStatus = "running"
	StatusStopped InstanceStatus = "stopped"
	StatusUnknown InstanceStatus = "unknown"
)

type CreateRequest struct {
	Name          string
	Image         string
	CPUs          int
	MemoryGB      int
	DiskGB        int
	CloudInitPath string
}

type InstanceInfo struct {
	Name       string
	Status     InstanceStatus
	IPv4       []string
	Release    string
	ImageHash  string
	CPUCount   int
	MemoryMiB  int
	DiskUsage  string
	Snapshots  int
	Backend    string
	Additional map[string]string
}

type SnapshotInfo struct {
	Instance string
	Name     string
	Parent   string
	Comment  string
}

type UpdateResourcesRequest struct {
	Name     string
	CPUs     int
	MemoryGB int
	DiskGB   int
}

type InstallSSHKeyRequest struct {
	Name      string
	Username  string
	PublicKey string
}

type Backend interface {
	Name() string
	Priority() int
	Available(context.Context) (bool, error)
	EnsureInstalled(context.Context) error
	Create(context.Context, CreateRequest) error
	Start(context.Context, string) error
	Stop(context.Context, string) error
	Delete(context.Context, string) error
	List(context.Context) ([]InstanceInfo, error)
	Info(context.Context, string) (*InstanceInfo, error)
	UpdateResources(context.Context, UpdateResourcesRequest) error
	InstallSSHKey(context.Context, InstallSSHKeyRequest) error
	Snapshot(context.Context, string, string, string) error
	Restore(context.Context, string, string) error
	ListSnapshots(context.Context, string) ([]SnapshotInfo, error)
}
