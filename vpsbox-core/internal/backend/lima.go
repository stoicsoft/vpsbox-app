package backend

import (
	"context"
	"fmt"
	"runtime"

	"github.com/stoicsoft/vpsbox/internal/executil"
)

type Lima struct{}

func NewLima() *Lima {
	return &Lima{}
}

func (l *Lima) Name() string {
	return "lima"
}

func (l *Lima) Priority() int {
	return 20
}

func (l *Lima) Available(context.Context) (bool, error) {
	return executil.LookPath("limactl"), nil
}

func (l *Lima) EnsureInstalled(ctx context.Context) error {
	ok, err := l.Available(ctx)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}

	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
		if executil.LookPath("brew") {
			_, err := executil.Run(ctx, "brew", "install", "lima")
			return err
		}
	}

	return fmt.Errorf("lima backend is not installed")
}

func (l *Lima) Create(context.Context, CreateRequest) error {
	return ErrUnsupported
}

func (l *Lima) Start(context.Context, string) error {
	return ErrUnsupported
}

func (l *Lima) Stop(context.Context, string) error {
	return ErrUnsupported
}

func (l *Lima) Delete(context.Context, string) error {
	return ErrUnsupported
}

func (l *Lima) List(context.Context) ([]InstanceInfo, error) {
	return nil, ErrUnsupported
}

func (l *Lima) Info(context.Context, string) (*InstanceInfo, error) {
	return nil, ErrUnsupported
}

func (l *Lima) UpdateResources(context.Context, UpdateResourcesRequest) error {
	return ErrUnsupported
}

func (l *Lima) InstallSSHKey(context.Context, InstallSSHKeyRequest) error {
	return ErrUnsupported
}

func (l *Lima) Snapshot(context.Context, string, string, string) error {
	return ErrUnsupported
}

func (l *Lima) Restore(context.Context, string, string) error {
	return ErrUnsupported
}

func (l *Lima) ListSnapshots(context.Context, string) ([]SnapshotInfo, error) {
	return nil, ErrUnsupported
}
