package app

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/stoicsoft/vpsbox/internal/workspace"
)

func newWorkspaceCommand(ctx context.Context, manager *Manager) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "workspace",
		Aliases: []string{"ws"},
		Short:   "Private networks that connect a group of sandboxes and isolate them from the rest",
		Long: "A workspace is a private network, the way a cloud provider means it: every\n" +
			"member gets a second, stable address on a subnet of its own, members reach\n" +
			"each other by name, and members of different workspaces cannot reach each\n" +
			"other at all.\n\n" +
			"  vpsbox workspace create alpha    allocate a private network\n" +
			"  vpsbox workspace add alpha db    put a sandbox on it\n" +
			"  vpsbox workspace show alpha      see members and check reachability\n\n" +
			"Inside a member, the others answer to their sandbox name:\n\n" +
			"  psql -h db -U postgres\n" +
			"  curl http://api:3000/health\n\n" +
			"Networks are carved out of " + workspace.BaseNetwork + ", one /24 each.\n\n" +
			"The isolation is a firewall inside each sandbox, not something the host\n" +
			"enforces — a sandbox is root on itself and can undo it. That is also true of\n" +
			"a real private network at a cloud provider.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkspaceList(ctx, manager)
		},
	}

	cmd.AddCommand(newWorkspaceCreateCommand(ctx, manager))
	cmd.AddCommand(newWorkspaceListCommand(ctx, manager))
	cmd.AddCommand(newWorkspaceShowCommand(ctx, manager))
	cmd.AddCommand(newWorkspaceAddCommand(ctx, manager))
	cmd.AddCommand(newWorkspaceRemoveCommand(ctx, manager))
	cmd.AddCommand(newWorkspaceSyncCommand(ctx, manager))
	cmd.AddCommand(newWorkspaceDestroyCommand(ctx, manager))
	return cmd
}

func newWorkspaceCreateCommand(ctx context.Context, manager *Manager) *cobra.Command {
	return &cobra.Command{
		Use:   "create <name>",
		Short: "Allocate a private network",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			info, err := manager.CreateWorkspace(ctx, args[0])
			if err != nil {
				return err
			}
			fmt.Printf("\n✓ Workspace %s created on %s\n\n", info.Name, info.CIDR)
			fmt.Printf("  Members will be given addresses from %s\n", info.CIDR)
			fmt.Println()
			fmt.Printf("Put a sandbox on it with: vpsbox workspace add %s <sandbox>\n", info.Name)
			return nil
		},
	}
}

func newWorkspaceListCommand(ctx context.Context, manager *Manager) *cobra.Command {
	return &cobra.Command{
		Use:     "ls",
		Aliases: []string{"list"},
		Short:   "List private networks",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkspaceList(ctx, manager)
		},
	}
}

func runWorkspaceList(ctx context.Context, manager *Manager) error {
	workspaces, err := manager.ListWorkspaces(ctx)
	if err != nil {
		return err
	}

	fmt.Println("Workspaces:")
	fmt.Println()
	if len(workspaces) == 0 {
		fmt.Println("  None yet — create one with `vpsbox workspace create <name>`.")
		return nil
	}

	fmt.Printf("  %-18s %-18s %s\n", "NAME", "NETWORK", "MEMBERS")
	for _, info := range workspaces {
		members := "none"
		if len(info.Members) > 0 {
			members = fmt.Sprintf("%d", len(info.Members))
		}
		fmt.Printf("  %-18s %-18s %s\n", truncate(info.Name, 18), info.CIDR, members)
	}
	fmt.Println()
	fmt.Println("See one in detail with: vpsbox workspace show <name>")
	return nil
}

func newWorkspaceShowCommand(ctx context.Context, manager *Manager) *cobra.Command {
	var check bool
	cmd := &cobra.Command{
		Use:   "show <name>",
		Short: "Show a private network's members",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			info, err := manager.GetWorkspace(ctx, args[0])
			if err != nil {
				return err
			}

			fmt.Printf("Workspace %s\n\n", info.Name)
			fmt.Printf("  Network   %s\n", info.CIDR)
			fmt.Printf("  Gateway   %s (reserved)\n", info.Gateway)
			fmt.Println()

			if len(info.Members) == 0 {
				fmt.Println("  No members yet — add one with `vpsbox workspace add " + info.Name + " <sandbox>`.")
				return nil
			}

			fmt.Printf("  %-16s %-14s %-10s %s\n", "SANDBOX", "PRIVATE IP", "STATUS", "REACHABLE AS")
			for _, member := range info.Members {
				fmt.Printf("  %-16s %-14s %-10s %s\n",
					truncate(member.Instance, 16), member.PrivateIP, member.Status, member.Hostname)
			}

			if !check {
				fmt.Println()
				fmt.Println("Check they can actually reach each other with: vpsbox workspace show " + info.Name + " --check")
				return nil
			}

			// Reachability is measured from the first running member, since a
			// stopped one cannot send anything.
			from := ""
			for _, member := range info.Members {
				if member.Status == "Running" || member.Status == "running" {
					from = member.Instance
					break
				}
			}
			if from == "" {
				fmt.Println()
				fmt.Println("  No member is running, so there is nothing to check from.")
				return nil
			}

			fmt.Println()
			fmt.Printf("Reachability from %s:\n\n", from)
			results, err := manager.WorkspaceReachability(ctx, info.Name, from)
			if err != nil {
				return err
			}
			if len(results) == 0 {
				fmt.Println("  Nothing to reach — it is the only member.")
				return nil
			}
			for _, member := range info.Members {
				reachable, tested := results[member.Instance]
				if !tested {
					continue
				}
				mark := "✗"
				if reachable {
					mark = "✓"
				}
				fmt.Printf("  %s %-16s %s\n", mark, truncate(member.Instance, 16), member.PrivateIP)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "ping every member from a running one to prove the network works")
	return cmd
}

func newWorkspaceAddCommand(ctx context.Context, manager *Manager) *cobra.Command {
	return &cobra.Command{
		Use:   "add <workspace> <sandbox>",
		Short: "Put a sandbox on a private network",
		Long: "Gives the sandbox a stable private address, teaches every member the others'\n" +
			"names, and applies the isolation rules. Safe to re-run.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, instanceName := args[0], args[1]
			info, err := manager.JoinWorkspace(ctx, name, instanceName,
				func(message string) { fmt.Println("  " + message) },
				func(line string) { fmt.Println("  │ " + line) },
			)
			if err != nil {
				return err
			}

			var joined *WorkspaceMemberInfo
			for i := range info.Members {
				if info.Members[i].Instance == instanceName {
					joined = &info.Members[i]
					break
				}
			}

			fmt.Printf("\n✓ %s is on the %s workspace\n\n", instanceName, info.Name)
			if joined != nil {
				fmt.Printf("  Private address   %s\n", joined.PrivateIP)
				fmt.Printf("  Reachable as      %s\n", joined.Hostname)
			}

			if len(info.Members) > 1 {
				fmt.Println()
				fmt.Println("  From another member, it now answers to its name:")
				fmt.Println()
				fmt.Printf("    ping %s\n", instanceName)
			} else {
				fmt.Println()
				fmt.Printf("  It is the only member so far. Add another with:\n")
				fmt.Printf("    vpsbox workspace add %s <sandbox>\n", info.Name)
			}
			return nil
		},
	}
}

func newWorkspaceRemoveCommand(ctx context.Context, manager *Manager) *cobra.Command {
	return &cobra.Command{
		Use:     "remove <workspace> <sandbox>",
		Aliases: []string{"rm"},
		Short:   "Take a sandbox off a private network",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, instanceName := args[0], args[1]
			if _, err := manager.LeaveWorkspace(ctx, name, instanceName,
				func(message string) { fmt.Println("  " + message) },
				func(line string) { fmt.Println("  │ " + line) },
			); err != nil {
				return err
			}
			fmt.Printf("\n✓ %s is off the %s workspace\n", instanceName, name)
			return nil
		},
	}
}

func newWorkspaceSyncCommand(ctx context.Context, manager *Manager) *cobra.Command {
	return &cobra.Command{
		Use:   "sync <name>",
		Short: "Re-apply a private network to every member",
		Long: "The repair command. Members that were stopped when something changed, or that\n" +
			"came back from a restart without their private address, are put right by this.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			info, err := manager.SyncWorkspace(ctx, args[0],
				func(message string) { fmt.Println("  " + message) },
				func(line string) { fmt.Println("  │ " + line) },
			)
			if err != nil {
				return err
			}
			fmt.Printf("\n✓ %s is in sync across %d member(s)\n", info.Name, len(info.Members))
			return nil
		},
	}
}

func newWorkspaceDestroyCommand(ctx context.Context, manager *Manager) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "destroy <name>",
		Short: "Remove a private network (the sandboxes themselves are untouched)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			info, err := manager.GetWorkspace(ctx, name)
			if err != nil {
				return err
			}
			if len(info.Members) > 0 && !force {
				return fmt.Errorf("workspace %q still has %d member(s) — pass --force to remove it and take them off the network",
					name, len(info.Members))
			}

			if err := manager.DestroyWorkspace(ctx, name,
				func(message string) { fmt.Println("  " + message) },
				func(line string) { fmt.Println("  │ " + line) },
			); err != nil {
				return err
			}
			fmt.Printf("\n✓ Workspace %s removed. The sandboxes are still there.\n", name)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "remove the workspace even if sandboxes are still on it")
	return cmd
}
