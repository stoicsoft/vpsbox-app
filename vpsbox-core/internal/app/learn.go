package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/stoicsoft/vpsbox/internal/missions"
	"github.com/spf13/cobra"
)

// LearnVerify SSHes into the sandbox and runs the mission's verify script.
// Pass = exit 0 AND non-empty stdout (the scripts echo OK on success).
func (m *Manager) LearnVerify(ctx context.Context, name, missionID string) (bool, string, error) {
	mission := missions.Find(missionID)
	if mission == nil {
		return false, "", fmt.Errorf("unknown mission %q", missionID)
	}
	instance, err := m.Info(ctx, name)
	if err != nil {
		return false, "", err
	}
	if instance.Host == "" {
		return false, "", errors.New("instance is not reachable yet")
	}
	stdout, _, runErr := m.runRemoteOn(ctx, instance, mission.Verify)
	passed := runErr == nil && strings.TrimSpace(stdout) != ""
	return passed, stdout, nil
}

func newLearnCommand(ctx context.Context, manager *Manager) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "learn [mission]",
		Short: "Hands-on missions to teach you how to use a Linux server",
		Long: "vpsbox learn ships with a set of small, hands-on missions. Each one teaches\n" +
			"a single VPS skill (deploy a static site, open a firewall port, write a\n" +
			"systemd service, …) and ends with a verify check you can run from your host:\n\n" +
			"  vpsbox learn list\n" +
			"  vpsbox learn first-shell\n" +
			"  vpsbox learn verify first-shell",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				printMissionList()
				return nil
			}
			mission := missions.Find(args[0])
			if mission == nil {
				return fmt.Errorf("unknown mission %q (try `vpsbox learn list`)", args[0])
			}
			renderMission(mission)
			return nil
		},
	}
	cmd.AddCommand(newLearnListCommand())
	cmd.AddCommand(newLearnVerifyCommand(ctx, manager))
	return cmd
}

func newLearnListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all available missions",
		RunE: func(cmd *cobra.Command, args []string) error {
			printMissionList()
			return nil
		},
	}
}

func newLearnVerifyCommand(ctx context.Context, manager *Manager) *cobra.Command {
	var instanceName string
	cmd := &cobra.Command{
		Use:   "verify [mission]",
		Short: "Run a mission's verify check inside the sandbox",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mission := missions.Find(args[0])
			if mission == nil {
				return fmt.Errorf("unknown mission %q", args[0])
			}
			passed, _, err := manager.LearnVerify(ctx, instanceName, args[0])
			if err != nil {
				return err
			}
			if passed {
				fmt.Printf("✓ %s — passed!\n\n", mission.Title)
				fmt.Println(mission.SuccessMsg)
				return nil
			}
			fmt.Printf("✗ %s — not yet.\n\n", mission.Title)
			fmt.Println("Hint:", mission.FailHint)
			return nil
		},
	}
	cmd.Flags().StringVar(&instanceName, "instance", "", "instance name (defaults to the only sandbox)")
	return cmd
}

func printMissionList() {
	fmt.Println("Missions to teach you VPS basics:")
	fmt.Println()
	for _, m := range missions.Missions {
		fmt.Printf("  %-18s %s\n", m.ID, m.Title)
	}
	fmt.Println()
	fmt.Println("Show a mission:    vpsbox learn first-shell")
	fmt.Println("Verify a mission:  vpsbox learn verify first-shell")
}

func renderMission(m *missions.Mission) {
	fmt.Printf("# %s\n\n", m.Title)
	fmt.Printf("%s\n\n", m.Intro)
	fmt.Println("Steps:")
	for i, step := range m.Steps {
		fmt.Printf("  %d. %s\n", i+1, step)
	}
	fmt.Println()
	fmt.Printf("When you think you're done, run:  vpsbox learn verify %s\n", m.ID)
}
