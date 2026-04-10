package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/stoicsoft/vpsbox/internal/registry"
	"github.com/spf13/cobra"
)

func Execute(ctx context.Context) error {
	manager, err := NewManager(ctx)
	if err != nil {
		return err
	}

	root := &cobra.Command{
		Use:   "vpsbox",
		Short: "Local VPS sandbox for trying deploy tools without renting a server",
	}

	root.AddCommand(newUpCommand(ctx, manager))
	root.AddCommand(newDownCommand(ctx, manager))
	root.AddCommand(newDestroyCommand(ctx, manager))
	root.AddCommand(newListCommand(ctx, manager))
	root.AddCommand(newInfoCommand(ctx, manager))
	root.AddCommand(newSSHCommand(ctx, manager))
	root.AddCommand(newSnapshotCommand(ctx, manager))
	root.AddCommand(newResetCommand(ctx, manager))
	root.AddCommand(newSnapshotsCommand(ctx, manager))
	root.AddCommand(newExportCommand(ctx, manager))
	root.AddCommand(newDoctorCommand(ctx, manager))
	root.AddCommand(newUICommand(ctx, manager))
	root.AddCommand(newShareCommand(ctx, manager))
	root.AddCommand(newSharesCommand(ctx, manager))
	root.AddCommand(newUnshareCommand(manager))
	root.AddCommand(newLoginCommand(manager))
	root.AddCommand(newLogoutCommand(manager))
	root.AddCommand(newUpgradeCommand(ctx, manager))
	root.AddCommand(newVersionCommand(manager))

	// Beginner-friendly commands.
	root.AddCommand(newCheckpointCommand(ctx, manager))
	root.AddCommand(newUndoCommand(ctx, manager))
	root.AddCommand(newPanicCommand(ctx, manager))
	root.AddCommand(newDiffCommand(ctx, manager))
	root.AddCommand(newExplainCommand(manager))
	root.AddCommand(newLogsCommand(ctx, manager))
	root.AddCommand(newDeployCommand(ctx, manager))
	root.AddCommand(newTourCommand(ctx, manager))
	root.AddCommand(newLearnCommand(ctx, manager))

	return root.Execute()
}

func newUpCommand(ctx context.Context, manager *Manager) *cobra.Command {
	opts := UpOptions{}

	cmd := &cobra.Command{
		Use:   "up",
		Short: "Create and start a sandbox VPS",
		RunE: func(cmd *cobra.Command, args []string) error {
			instance, err := manager.Up(ctx, opts)
			if err != nil {
				return err
			}

			host := instance.Hostname
			if host == "" {
				host = instance.Host
			}

			fmt.Printf("✓ Sandbox %q is ready\n\n", instance.Name)
			fmt.Printf("Name:      %s\n", instance.Name)
			fmt.Printf("Backend:   %s\n", instance.Backend)
			fmt.Printf("IP:        %s\n", instance.Host)
			fmt.Printf("SSH:       ssh %s@%s -i %s\n", instance.Username, host, instance.PrivateKeyPath)
			fmt.Printf("Domain:    %s\n", instance.Hostname)
			fmt.Printf("Cert:      %s\n\n", instance.CertPath)

			printFirstRunCheatsheet(instance.Name)
			return nil
		},
	}

	cmd.Flags().StringVar(&opts.Name, "name", "", "sandbox instance name")
	cmd.Flags().IntVar(&opts.CPUs, "cpus", 2, "number of vCPUs")
	cmd.Flags().IntVar(&opts.MemoryGB, "memory", 2, "memory in GB")
	cmd.Flags().IntVar(&opts.DiskGB, "disk", 10, "disk in GB")
	cmd.Flags().StringVar(&opts.Image, "image", "24.04", "Ubuntu image release")
	cmd.Flags().StringVar(&opts.User, "user", "root", "instance SSH user")
	cmd.Flags().BoolVar(&opts.SelfSigned, "self-signed", false, "skip mkcert and generate a self-signed certificate")
	return cmd
}

func newDownCommand(ctx context.Context, manager *Manager) *cobra.Command {
	return &cobra.Command{
		Use:   "down [name]",
		Short: "Stop a sandbox VPS and preserve its disk",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := firstArg(args)
			instance, err := manager.Down(ctx, name)
			if err != nil {
				return err
			}
			fmt.Printf("✓ Stopped %s\n", instance.Name)
			return nil
		},
	}
}

func newDestroyCommand(ctx context.Context, manager *Manager) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "destroy [name]",
		Short: "Permanently delete an instance and its snapshots",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := firstArg(args)
			if err := manager.Destroy(ctx, name, force); err != nil {
				return err
			}
			if name == "" {
				fmt.Println("✓ Destroyed sandbox")
				return nil
			}
			fmt.Printf("✓ Destroyed %s\n", name)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "force deletion even if snapshots exist")
	return cmd
}

func newListCommand(ctx context.Context, manager *Manager) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all sandbox instances",
		RunE: func(cmd *cobra.Command, args []string) error {
			instances, err := manager.List(ctx)
			if err != nil {
				return err
			}
			if asJSON {
				return printJSON(instances)
			}
			tw := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
			manager.PrintTable(tw, []string{"NAME", "STATUS", "HOST", "USER", "BACKEND", "CREATED"}, buildInstanceRows(instances))
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return cmd
}

func newInfoCommand(ctx context.Context, manager *Manager) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "info [name]",
		Short: "Show full connection details for an instance",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			instance, err := manager.Info(ctx, firstArg(args))
			if err != nil {
				return err
			}
			if asJSON {
				return printJSON(instance)
			}
			fmt.Printf("Name:          %s\n", instance.Name)
			fmt.Printf("Status:        %s\n", instance.Status)
			fmt.Printf("Host:          %s\n", instance.Host)
			fmt.Printf("Hostname:      %s\n", instance.Hostname)
			fmt.Printf("Username:      %s\n", instance.Username)
			fmt.Printf("Private key:   %s\n", instance.PrivateKeyPath)
			fmt.Printf("Image:         %s\n", instance.Image)
			fmt.Printf("Cert path:     %s\n", instance.CertPath)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return cmd
}

func newSSHCommand(ctx context.Context, manager *Manager) *cobra.Command {
	return &cobra.Command{
		Use:   "ssh [name] [-- command]",
		Short: "Open an SSH shell or run a one-shot command",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := ""
			remote := []string{}
			if len(args) > 0 {
				name = args[0]
			}
			if len(args) > 1 {
				remote = args[1:]
			}
			return manager.SSH(ctx, name, remote)
		},
	}
}

func newSnapshotCommand(ctx context.Context, manager *Manager) *cobra.Command {
	var snapshotName string
	var comment string
	cmd := &cobra.Command{
		Use:   "snapshot [name]",
		Short: "Save a snapshot of the current VM state",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := manager.Snapshot(ctx, firstArg(args), snapshotName, comment); err != nil {
				return err
			}
			if snapshotName == "" {
				snapshotName = "latest"
			}
			fmt.Printf("✓ Snapshot saved for %s\n", firstArg(args))
			return nil
		},
	}
	cmd.Flags().StringVar(&snapshotName, "name", "", "snapshot name")
	cmd.Flags().StringVar(&comment, "comment", "", "snapshot comment")
	return cmd
}

func newResetCommand(ctx context.Context, manager *Manager) *cobra.Command {
	var snapshotName string
	cmd := &cobra.Command{
		Use:   "reset [name]",
		Short: "Restore an instance from a snapshot",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			instance, err := manager.Reset(ctx, firstArg(args), snapshotName)
			if err != nil {
				return err
			}
			fmt.Printf("✓ Restored %s\n", instance.Name)
			return nil
		},
	}
	cmd.Flags().StringVar(&snapshotName, "snapshot", "", "snapshot name")
	return cmd
}

func newSnapshotsCommand(ctx context.Context, manager *Manager) *cobra.Command {
	return &cobra.Command{
		Use:   "snapshots [name]",
		Short: "List saved snapshots for an instance",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			snapshots, err := manager.ListSnapshots(ctx, firstArg(args))
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
			rows := make([][]string, 0, len(snapshots))
			for _, snap := range snapshots {
				rows = append(rows, []string{snap.Name, snap.Parent, snap.Comment})
			}
			manager.PrintTable(tw, []string{"NAME", "PARENT", "COMMENT"}, rows)
			return nil
		},
	}
}

func newExportCommand(ctx context.Context, manager *Manager) *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "export [name]",
		Short: "Export connection details for Server Compass or shell use",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out, err := manager.Export(ctx, firstArg(args), format)
			if err != nil {
				return err
			}
			fmt.Println(out)
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "json", "output format: json|sc|env")
	return cmd
}

func newDoctorCommand(ctx context.Context, manager *Manager) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose the host environment before booting a sandbox",
		RunE: func(cmd *cobra.Command, args []string) error {
			checks := manager.Doctor(ctx)
			if asJSON {
				return printJSON(checks)
			}
			tw := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
			rows := make([][]string, 0, len(checks))
			for _, check := range checks {
				rows = append(rows, []string{strings.ToUpper(string(check.Status)), check.Name, check.Details})
			}
			manager.PrintTable(tw, []string{"STATUS", "CHECK", "DETAILS"}, rows)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return cmd
}

func newUICommand(ctx context.Context, manager *Manager) *cobra.Command {
	var port int
	var open bool
	cmd := &cobra.Command{
		Use:   "ui",
		Short: "Open a local dashboard in the browser",
		RunE: func(cmd *cobra.Command, args []string) error {
			server := NewServer(manager)
			return server.Run(ctx, port, open)
		},
	}
	cmd.Flags().IntVar(&port, "port", 7878, "dashboard port")
	cmd.Flags().BoolVar(&open, "open", true, "open the dashboard in the default browser")
	return cmd
}

func newShareCommand(ctx context.Context, manager *Manager) *cobra.Command {
	var ttl time.Duration
	var name string
	cmd := &cobra.Command{
		Use:   "share URL",
		Short: "Expose a local URL publicly with a Cloudflare quick tunnel",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			share, err := manager.CreateShare(ctx, args[0], ttl, name)
			if err != nil {
				return err
			}
			fmt.Printf("✓ Public link ready\n\n%s\n", share.URL)
			if share.ExpiresAt != nil {
				fmt.Printf("Expires: %s\n", share.ExpiresAt.Local().Format(time.RFC1123))
			}
			return nil
		},
	}
	cmd.Flags().DurationVar(&ttl, "ttl", 4*time.Hour, "share lifetime")
	cmd.Flags().StringVar(&name, "name", "", "preferred share name")
	return cmd
}

func newSharesCommand(ctx context.Context, manager *Manager) *cobra.Command {
	return &cobra.Command{
		Use:   "shares",
		Short: "List active public shares",
		RunE: func(cmd *cobra.Command, args []string) error {
			shares, err := manager.Shares()
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
			rows := make([][]string, 0, len(shares))
			for _, share := range shares {
				expires := "never"
				if share.ExpiresAt != nil {
					expires = share.ExpiresAt.Local().Format(time.RFC822)
				}
				rows = append(rows, []string{share.Name, share.URL, share.TargetURL, expires})
			}
			manager.PrintTable(tw, []string{"NAME", "URL", "TARGET", "EXPIRES"}, rows)
			return nil
		},
	}
}

func newUnshareCommand(manager *Manager) *cobra.Command {
	return &cobra.Command{
		Use:   "unshare NAME",
		Short: "Tear down a share tunnel immediately",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := manager.Unshare(args[0]); err != nil {
				return err
			}
			fmt.Printf("✓ Share %s removed\n", args[0])
			return nil
		},
	}
}

func newLoginCommand(manager *Manager) *cobra.Command {
	var email string
	var token string
	var apiBase string
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Store the email/token pair used by the hosted share relay",
		RunE: func(cmd *cobra.Command, args []string) error {
			session, err := manager.Login(email, token, apiBase)
			if err != nil {
				return err
			}
			fmt.Printf("✓ Saved session for %s (%s)\n", session.Email, session.APIBase)
			if session.Token == "" {
				fmt.Println("Hosted relay auth is not configured yet; quick tunnels still work without login.")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&email, "email", "", "account email")
	cmd.Flags().StringVar(&token, "token", "", "relay API token")
	cmd.Flags().StringVar(&apiBase, "api-base", "", "relay API base URL")
	_ = cmd.MarkFlagRequired("email")
	return cmd
}

func newLogoutCommand(manager *Manager) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Forget the stored hosted-relay session",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := manager.Logout(); err != nil {
				return err
			}
			fmt.Println("✓ Logged out")
			return nil
		},
	}
}

func newUpgradeCommand(ctx context.Context, manager *Manager) *cobra.Command {
	return &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade vpsbox through the detected package manager",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := manager.Upgrade(ctx); err != nil {
				return err
			}
			fmt.Println("✓ Upgrade finished")
			return nil
		},
	}
}

func newVersionCommand(manager *Manager) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version and host details",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("vpsbox %s\n", Version)
			fmt.Printf("backend: %s\n", manager.BackendName())
			fmt.Printf("home:    %s\n", manager.Paths().BaseDir)
			fmt.Printf("goos:    %s\n", runtime.GOOS)
			fmt.Printf("goarch:  %s\n", runtime.GOARCH)
			return nil
		},
	}
}

// printFirstRunCheatsheet prints the friendly post-`up` welcome aimed at
// users who have never logged into a VPS before. The goal is to give them
// a short, copy-pasteable map of "what now?" without dumping man pages.
func printFirstRunCheatsheet(name string) {
	fmt.Println("First time using a VPS? Here's your map:")
	fmt.Println()
	fmt.Println("  Connect")
	fmt.Println("    vpsbox ssh " + name + "                # open a shell inside the VM")
	fmt.Println("    vpsbox tour " + name + "               # guided 10-step walkthrough")
	fmt.Println()
	fmt.Println("  Learn safely (sandbox = you can't break anything)")
	fmt.Println("    vpsbox learn                       # hands-on missions")
	fmt.Println("    vpsbox explain chmod               # plain-English command help")
	fmt.Println("    vpsbox deploy --list               # one-shot starter apps")
	fmt.Println()
	fmt.Println("  Save and roll back")
	fmt.Println("    vpsbox checkpoint                  # save a 'known good' point")
	fmt.Println("    vpsbox diff                        # see what changed since")
	fmt.Println("    vpsbox undo                        # roll back to last checkpoint")
	fmt.Println("    vpsbox panic                       # 'I broke it' button (== undo)")
	fmt.Println()
	fmt.Println("  Look around")
	fmt.Println("    vpsbox logs " + name + " -f             # tail system logs live")
	fmt.Println("    vpsbox info " + name + "                # connection details")
	fmt.Println("    vpsbox ui                          # local web dashboard")
	fmt.Println()
	fmt.Println("  When you're done")
	fmt.Println("    vpsbox down " + name + "                # stop (keeps the disk)")
	fmt.Println("    vpsbox destroy " + name + " --force     # delete forever")
	fmt.Println()
}

func buildInstanceRows(instances []registry.Instance) [][]string {
	rows := make([][]string, 0, len(instances))
	for _, instance := range instances {
		created := "-"
		if !instance.CreatedAt.IsZero() {
			created = instance.CreatedAt.Local().Format(time.RFC822)
		}
		rows = append(rows, []string{
			instance.Name,
			instance.Status,
			instance.Host,
			instance.Username,
			instance.Backend,
			created,
		})
	}
	return rows
}

func printJSON(value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

func firstArg(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}
