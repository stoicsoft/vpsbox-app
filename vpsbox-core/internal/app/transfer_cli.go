package app

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/stoicsoft/vpsbox/internal/migrate"
)

func newPushCommand(ctx context.Context, manager *Manager) *cobra.Command {
	var to, keyPath string
	var port int
	var dryRun, yes bool

	cmd := &cobra.Command{
		Use:   "push [name]",
		Short: "Copy a sandbox's packages, files, and apps onto a real VPS",
		Long: "Replicate the work you did in a sandbox onto a real server over SSH: packages\n" +
			"you installed, files under /root, /home, /opt, /srv, /var/www and service\n" +
			"configs, docker volume data, enabled services, and running compose projects.\n" +
			"Machine identity never moves — SSH server settings, host keys, .ssh\n" +
			"directories, network and firewall config all stay put, so a push cannot lock\n" +
			"you out of the server.\n\n" +
			"The plan is shown and confirmed before anything is touched. Your machine\n" +
			"relays the data, so the sandbox and the VPS never need to reach each other.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target, err := migrate.ParseTarget(to)
			if err != nil {
				return err
			}
			if port > 0 {
				target.Port = port
			}
			target.KeyPath = expandKeyPath(keyPath)

			opts := PushOptions{
				Name:   firstArg(args),
				Target: target,
				DryRun: dryRun,
				Status: func(s string) { fmt.Println("  " + s) },
				Output: func(s string) { fmt.Println("  │ " + s) },
			}
			if !yes {
				opts.Confirm = func(plan *migrate.Plan) bool {
					fmt.Println()
					renderTransferPlan(plan)
					fmt.Printf("Push to %s? This installs packages and overwrites files there. [y/N] ", plan.Dest)
					reader := bufio.NewReader(os.Stdin)
					answer, _ := reader.ReadString('\n')
					answer = strings.ToLower(strings.TrimSpace(answer))
					return answer == "y" || answer == "yes"
				}
			}

			plan, err := manager.Push(ctx, opts)
			if errors.Is(err, ErrMigrationCancelled) {
				fmt.Println("Cancelled — nothing was changed.")
				return nil
			}
			if err != nil {
				return err
			}
			if dryRun {
				renderTransferPlan(plan)
				fmt.Println("Dry run — nothing was changed. Rerun without --dry-run to push.")
				return nil
			}
			fmt.Printf("\n✓ Pushed to %s\n", plan.Dest)
			return nil
		},
	}

	cmd.Flags().StringVar(&to, "to", "", "destination, e.g. root@203.0.113.10 (required)")
	cmd.Flags().StringVar(&keyPath, "key", "", "SSH private key for the destination (default: your SSH agent and default keys)")
	cmd.Flags().IntVar(&port, "port", 22, "destination SSH port")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show the migration plan and exit without changing anything")
	cmd.Flags().BoolVar(&yes, "yes", false, "skip the confirmation prompt")
	_ = cmd.MarkFlagRequired("to")
	return cmd
}

func newImportCommand(ctx context.Context, manager *Manager) *cobra.Command {
	var keyPath, name string
	var port, cpus, memoryGB, diskGB int
	var selfSigned bool

	cmd := &cobra.Command{
		Use:   "import user@host",
		Short: "Clone a real VPS into a local sandbox you can break safely",
		Long: "Inspect a real server over SSH, create a local sandbox sized to hold what runs\n" +
			"there, and copy the work in: installed packages, files, docker volume data,\n" +
			"enabled services, and running compose projects. The server is only read from —\n" +
			"nothing on it changes. Once imported, you can rehearse upgrades and break\n" +
			"things locally before touching the real machine.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target, err := migrate.ParseTarget(args[0])
			if err != nil {
				return err
			}
			if port > 0 {
				target.Port = port
			}
			target.KeyPath = expandKeyPath(keyPath)

			instance, plan, err := manager.ImportVPS(ctx, ImportVPSOptions{
				Target:     target,
				Name:       name,
				CPUs:       cpus,
				MemoryGB:   memoryGB,
				DiskGB:     diskGB,
				SelfSigned: selfSigned,
				Confirm: func(plan *migrate.Plan) bool {
					fmt.Println()
					renderTransferPlan(plan)
					return true // the destination is a fresh sandbox — nothing to protect
				},
				Status: func(s string) { fmt.Println("  " + s) },
				Output: func(s string) { fmt.Println("  │ " + s) },
			})
			if err != nil {
				return err
			}

			fmt.Printf("\n✓ %s is a local copy of %s\n\n", instance.Name, plan.Source)
			fmt.Printf("  SSH into it:        vpsbox ssh %s\n", instance.Name)
			fmt.Printf("  Save a checkpoint:  vpsbox checkpoint %s\n", instance.Name)
			fmt.Println("\nBreak it as much as you like — the real server never noticed you were there.")
			return nil
		},
	}

	cmd.Flags().StringVar(&keyPath, "key", "", "SSH private key for the server (default: your SSH agent and default keys)")
	cmd.Flags().IntVar(&port, "port", 22, "server SSH port")
	cmd.Flags().StringVar(&name, "name", "", "sandbox name (default: vps-1, vps-2, …)")
	cmd.Flags().IntVar(&cpus, "cpus", 0, "sandbox vCPUs (default: sized from the server)")
	cmd.Flags().IntVar(&memoryGB, "memory", 0, "sandbox memory in GB (default: sized from the server)")
	cmd.Flags().IntVar(&diskGB, "disk", 0, "sandbox disk in GB (default: sized from the data to copy)")
	cmd.Flags().BoolVar(&selfSigned, "self-signed", false, "skip mkcert and generate a self-signed certificate")
	return cmd
}

// renderTransferPlan prints what a migration will do, in the order it happens.
func renderTransferPlan(plan *migrate.Plan) {
	fmt.Printf("Migration plan: %s → %s\n\n", plan.Source, plan.Dest)

	if len(plan.Packages) > 0 {
		fmt.Printf("  Packages to install (%d):  %s\n", len(plan.Packages), strings.Join(plan.Packages, ", "))
	}
	if len(plan.Users) > 0 {
		names := make([]string, 0, len(plan.Users))
		for _, user := range plan.Users {
			names = append(names, user.Name)
		}
		fmt.Printf("  Users to create:          %s\n", strings.Join(names, ", "))
	}
	if len(plan.Paths) > 0 {
		paths := make([]string, 0, len(plan.Paths))
		for _, path := range plan.Paths {
			paths = append(paths, path.Abs())
		}
		fmt.Printf("  Files to copy (~%s):  %s\n", humanKB(plan.EstimatedKB()), strings.Join(paths, ", "))
	}
	if len(plan.Volumes) > 0 {
		volumes := make([]string, 0, len(plan.Volumes))
		for _, volume := range plan.Volumes {
			volumes = append(volumes, fmt.Sprintf("%s (~%s)", volume.Name, humanKB(volume.SizeKB)))
		}
		fmt.Printf("  Docker volumes (%d):       %s\n", len(plan.Volumes), strings.Join(volumes, ", "))
	}
	if len(plan.Services) > 0 {
		fmt.Printf("  Services to enable (%d):   %s\n", len(plan.Services), strings.Join(plan.Services, ", "))
	}
	if len(plan.Compose) > 0 {
		projects := make([]string, 0, len(plan.Compose))
		for _, project := range plan.Compose {
			state := "will be started"
			if !project.Running {
				state = "files only, stays stopped"
			}
			projects = append(projects, fmt.Sprintf("%s (%s)", project.Name, state))
		}
		fmt.Printf("  Compose projects (%d):     %s\n", len(plan.Compose), strings.Join(projects, ", "))
	}
	if len(plan.Notes) > 0 {
		fmt.Println("\n  Not copied:")
		for _, note := range plan.Notes {
			fmt.Println("  • " + note)
		}
	}
	fmt.Println()
}

// expandKeyPath lets --key take ~/keys/id the way a shell would.
func expandKeyPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(path, "~"), "/"))
		}
	}
	return path
}
