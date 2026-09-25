package app

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

func newBucketCommand(ctx context.Context, manager *Manager) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bucket",
		Short: "S3-compatible object storage on your machine, for practising backups and uploads",
		Long: "Buckets live on your machine, not inside a sandbox, so they outlive the box\n" +
			"that wrote to them — which is what makes \"back up, wipe the server, restore\"\n" +
			"a thing you can actually rehearse locally.\n\n" +
			"  vpsbox bucket create backups     make a bucket\n" +
			"  vpsbox bucket attach             wire a sandbox up to reach it\n" +
			"  vpsbox bucket ls                 list buckets and show the endpoint\n\n" +
			"Once attached, the sandbox needs no flags:\n\n" +
			"  pg_dump mydb | s3 cp - s3://backups/mydb.sql\n" +
			"  s3 ls s3://backups/",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBucketList(ctx, manager)
		},
	}

	cmd.AddCommand(newBucketCreateCommand(ctx, manager))
	cmd.AddCommand(newBucketListCommand(ctx, manager))
	cmd.AddCommand(newBucketRemoveCommand(ctx, manager))
	cmd.AddCommand(newBucketAttachCommand(ctx, manager))
	cmd.AddCommand(newBucketEndpointCommand(ctx, manager))
	cmd.AddCommand(newBucketStopCommand(manager))
	return cmd
}

func newBucketCreateCommand(ctx context.Context, manager *Manager) *cobra.Command {
	return &cobra.Command{
		Use:   "create <name>",
		Short: "Create a bucket (starts the object store if it is not running)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			bucket, err := manager.CreateBucket(ctx, name, func(message string) {
				fmt.Println("  " + message)
			})
			if err != nil {
				return err
			}

			access, err := manager.BucketEndpoint(ctx)
			if err != nil {
				return err
			}

			fmt.Printf("\n✓ Bucket %s is ready\n\n", bucket.Name)
			fmt.Printf("  From this machine:  %s/%s\n", access.HostEndpoint, bucket.Name)
			fmt.Printf("  From a sandbox:     s3://%s\n", bucket.Name)
			fmt.Println()
			fmt.Println("Wire a sandbox up to it with: vpsbox bucket attach")
			return nil
		},
	}
}

func newBucketListCommand(ctx context.Context, manager *Manager) *cobra.Command {
	return &cobra.Command{
		Use:     "ls",
		Aliases: []string{"list"},
		Short:   "List buckets and show the endpoint",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBucketList(ctx, manager)
		},
	}
}

func runBucketList(ctx context.Context, manager *Manager) error {
	access, err := manager.BucketStatus(ctx)
	if err != nil {
		return err
	}

	status := "stopped"
	if access.Running {
		status = "running"
	}

	fmt.Println("Object store:")
	fmt.Println()
	fmt.Printf("  Status              %s\n", status)
	if access.HostEndpoint != "" {
		fmt.Printf("  From this machine   %s\n", access.HostEndpoint)
		fmt.Printf("  From a sandbox      %s\n", access.Endpoint)
	}
	fmt.Println()

	fmt.Println("Buckets:")
	fmt.Println()
	if len(access.Buckets) == 0 {
		fmt.Println("  None yet — make one with `vpsbox bucket create <name>`.")
		return nil
	}
	fmt.Printf("  %-24s %s\n", "NAME", "CREATED")
	for _, bucket := range access.Buckets {
		fmt.Printf("  %-24s %s\n", truncate(bucket.Name, 24), bucket.CreatedAt.Local().Format("2006-01-02 15:04"))
	}
	return nil
}

func newBucketRemoveCommand(ctx context.Context, manager *Manager) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:     "rm <name>",
		Aliases: []string{"delete", "destroy"},
		Short:   "Delete a bucket",
		Long: "Deletes a bucket. A bucket that still holds objects is refused unless you pass\n" +
			"--force: unlike a sandbox, there is no snapshot to restore a bucket from.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if err := manager.DestroyBucket(ctx, name, force); err != nil {
				return err
			}
			fmt.Printf("✓ Bucket %s deleted\n", name)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "delete the bucket even if it still holds objects")
	return cmd
}

func newBucketAttachCommand(ctx context.Context, manager *Manager) *cobra.Command {
	return &cobra.Command{
		Use:   "attach [sandbox]",
		Short: "Teach a sandbox how to reach the object store",
		Long: "Points the sandbox at this machine, writes S3 credentials where every client\n" +
			"looks for them, and installs an `s3` command that needs no --endpoint-url.\n\n" +
			"Safe to re-run — it is also how a sandbox picks up buckets created since the\n" +
			"last attach.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := ""
			if len(args) > 0 {
				name = args[0]
			}

			access, err := manager.AttachBuckets(ctx, name,
				func(message string) { fmt.Println("  " + message) },
				func(line string) { fmt.Println("  │ " + line) },
			)
			if err != nil {
				return err
			}

			instance, err := manager.requireInstance(name)
			if err != nil {
				return err
			}

			fmt.Printf("\n✓ %s can reach the object store at %s\n\n", instance.Name, access.Endpoint)
			fmt.Println("  Try it, inside the sandbox:")
			fmt.Println()
			fmt.Println("    s3 ls")
			if len(access.Buckets) > 0 {
				first := access.Buckets[0].Name
				fmt.Printf("    echo hello | s3 cp - s3://%s/hello.txt\n", first)
				fmt.Printf("    s3 ls s3://%s/\n", first)
			} else {
				fmt.Println()
				fmt.Println("  No buckets yet — make one with `vpsbox bucket create <name>`,")
				fmt.Println("  then re-run `vpsbox bucket attach`.")
			}
			return nil
		},
	}
}

func newBucketEndpointCommand(ctx context.Context, manager *Manager) *cobra.Command {
	var shell bool
	cmd := &cobra.Command{
		Use:   "endpoint",
		Short: "Print the object store endpoint and credentials",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			access, err := manager.BucketEndpoint(ctx)
			if err != nil {
				return err
			}

			if shell {
				fmt.Printf("export AWS_ENDPOINT_URL=%s\n", access.HostEndpoint)
				fmt.Printf("export AWS_ENDPOINT_URL_S3=%s\n", access.HostEndpoint)
				fmt.Printf("export AWS_ACCESS_KEY_ID=%s\n", access.AccessKey)
				fmt.Printf("export AWS_SECRET_ACCESS_KEY=%s\n", access.SecretKey)
				fmt.Printf("export AWS_DEFAULT_REGION=%s\n", access.Region)
				return nil
			}

			fmt.Println("Object store endpoint:")
			fmt.Println()
			fmt.Printf("  From this machine   %s\n", access.HostEndpoint)
			fmt.Printf("  From a sandbox      %s\n", access.Endpoint)
			fmt.Printf("  Region              %s\n", access.Region)
			fmt.Printf("  Access key          %s\n", access.AccessKey)
			fmt.Printf("  Secret key          %s\n", access.SecretKey)
			fmt.Println()
			fmt.Println("  Listening on all interfaces so sandboxes can reach it — these")
			fmt.Println("  credentials are the only thing protecting it on a shared network.")
			fmt.Println()
			fmt.Println("Eval it into your shell with: eval \"$(vpsbox bucket endpoint --shell)\"")
			return nil
		},
	}
	cmd.Flags().BoolVar(&shell, "shell", false, "print as shell exports for eval")
	return cmd
}

func newBucketStopCommand(manager *Manager) *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the object store server (bucket data is kept)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := manager.StopBuckets(); err != nil {
				return err
			}
			fmt.Println("✓ Object store stopped — your buckets and their contents are still on disk.")
			fmt.Println("  It starts again on the next `vpsbox bucket` command that needs it.")
			return nil
		},
	}
}
