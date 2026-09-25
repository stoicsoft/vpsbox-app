package app

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

func newLabCommand(ctx context.Context, manager *Manager) *cobra.Command {
	root := &cobra.Command{
		Use:   "lab",
		Short: "Manage reproducible multi-VPS test labs",
	}
	root.AddCommand(newLabValidateCommand(manager))
	root.AddCommand(newLabApplyCommand(ctx, manager))
	root.AddCommand(newLabStatusCommand(manager))
	root.AddCommand(newLabSnapshotCommand(ctx, manager))
	root.AddCommand(newLabResetCommand(ctx, manager))
	root.AddCommand(newLabExportCommand(ctx, manager))
	root.AddCommand(newLabDestroyCommand(ctx, manager))
	return root
}

func newLabValidateCommand(manager *Manager) *cobra.Command {
	var file string
	command := &cobra.Command{
		Use:   "validate",
		Short: "Validate a lab manifest without creating VPSes",
		RunE: func(cmd *cobra.Command, args []string) error {
			manifest, err := manager.ValidateLab(file)
			if err != nil {
				return err
			}
			fmt.Printf("✓ Lab manifest %q is valid (%d instance(s))\n", manifest.Metadata.Name, len(manifest.Spec.Instances))
			return nil
		},
	}
	command.Flags().StringVar(&file, "file", "", "path to a versioned lab YAML manifest")
	_ = command.MarkFlagRequired("file")
	return command
}

func newLabApplyCommand(ctx context.Context, manager *Manager) *cobra.Command {
	var file string
	var runID string
	command := &cobra.Command{
		Use:   "apply",
		Short: "Create or resume a lab run",
		RunE: func(cmd *cobra.Command, args []string) error {
			state, err := manager.ApplyLab(ctx, file, runID, func(message string) {
				fmt.Printf("  %s\n", message)
			})
			if err != nil {
				return err
			}
			fmt.Printf("✓ Lab %s is %s with %d instance(s)\n", state.RunID, state.Status, len(state.Instances))
			return nil
		},
	}
	command.Flags().StringVar(&file, "file", "", "path to a versioned lab YAML manifest")
	command.Flags().StringVar(&runID, "run-id", "", "exact lowercase lab run identifier")
	_ = command.MarkFlagRequired("file")
	_ = command.MarkFlagRequired("run-id")
	return command
}

func newLabStatusCommand(manager *Manager) *cobra.Command {
	return &cobra.Command{
		Use:   "status RUN_ID",
		Short: "Show persisted lab status as JSON",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			state, err := manager.LabStatus(args[0])
			if err != nil {
				return err
			}
			return printJSON(state)
		},
	}
}

func newLabSnapshotCommand(ctx context.Context, manager *Manager) *cobra.Command {
	var name string
	command := &cobra.Command{
		Use:   "snapshot RUN_ID",
		Short: "Snapshot every VPS owned by a lab run",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := manager.SnapshotLab(ctx, args[0], name)
			return err
		},
	}
	command.Flags().StringVar(&name, "name", "", "snapshot name")
	_ = command.MarkFlagRequired("name")
	return command
}

func newLabResetCommand(ctx context.Context, manager *Manager) *cobra.Command {
	var snapshot string
	command := &cobra.Command{
		Use:   "reset RUN_ID",
		Short: "Restore every VPS in a lab from one checkpoint",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := manager.ResetLab(ctx, args[0], snapshot)
			return err
		},
	}
	command.Flags().StringVar(&snapshot, "snapshot", "", "snapshot name")
	_ = command.MarkFlagRequired("snapshot")
	return command
}

func newLabExportCommand(ctx context.Context, manager *Manager) *cobra.Command {
	return &cobra.Command{
		Use:   "export RUN_ID",
		Short: "Export all lab SSH contracts for Server Compass",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			output, err := manager.ExportLab(ctx, args[0])
			if err != nil {
				return err
			}
			fmt.Println(output)
			return nil
		},
	}
}

func newLabDestroyCommand(ctx context.Context, manager *Manager) *cobra.Command {
	var force bool
	command := &cobra.Command{
		Use:   "destroy RUN_ID",
		Short: "Destroy only VPSes recorded as owned by an exact lab run",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := manager.DestroyLab(ctx, args[0], force); err != nil {
				return err
			}
			fmt.Printf("✓ Lab %s was destroyed\n", args[0])
			return nil
		},
	}
	command.Flags().BoolVar(&force, "force", false, "confirm exact-scope VM and artifact deletion")
	return command
}
