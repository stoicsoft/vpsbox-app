package app

import (
	"context"
	"errors"

	"github.com/spf13/cobra"
)

// Logs streams system logs from inside the sandbox. With follow=true it tails
// live; otherwise it prints the last 200 lines and exits.
func (m *Manager) Logs(ctx context.Context, name string, follow bool) error {
	instance, err := m.Info(ctx, name)
	if err != nil {
		return err
	}
	if instance.Host == "" {
		return errors.New("instance has no host yet — wait for it to come up")
	}

	// Use the existing interactive SSH path so the user can ctrl-c naturally.
	var script string
	if follow {
		script = `sudo journalctl -f -n 50 --no-hostname`
	} else {
		script = `sudo journalctl -n 200 --no-hostname --no-pager`
	}
	return m.SSH(ctx, name, []string{script})
}

func newLogsCommand(ctx context.Context, manager *Manager) *cobra.Command {
	var follow bool
	cmd := &cobra.Command{
		Use:   "logs [name]",
		Short: "Show recent system logs from inside the sandbox (-f to tail live)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return manager.Logs(ctx, firstArg(args), follow)
		},
	}
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "tail logs live (Ctrl-C to stop)")
	return cmd
}
