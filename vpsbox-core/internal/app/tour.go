package app

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

type tourStep struct {
	title     string
	narration string
	command   string
}

var tourSteps = []tourStep{
	{
		title:     "Who are you on this machine?",
		narration: "You SSHed into a Linux machine. Every shell session runs as some user. `whoami` prints which one.",
		command:   "whoami",
	},
	{
		title:     "Where are you in the filesystem?",
		narration: "Linux organises everything as a tree of folders. Your 'working directory' is where commands run by default. `pwd` prints it.",
		command:   "pwd",
	},
	{
		title:     "What's in your home folder?",
		narration: "Every user has a home directory at /home/<username>. This is where your stuff lives. -la shows hidden files and details.",
		command:   "ls -la /root",
	},
	{
		title:     "What does the whole filesystem look like?",
		narration: "The root of the tree is /. /etc holds config files, /var holds variable data (logs, databases), /usr holds installed software, /home holds user files.",
		command:   "ls /",
	},
	{
		title:     "How much disk space do you have?",
		narration: "`df` shows disk usage per filesystem. -h makes it human-readable (GB instead of bytes).",
		command:   "df -h /",
	},
	{
		title:     "How much memory?",
		narration: "`free` shows RAM usage. Linux uses spare RAM as a disk cache so 'used' often looks high — that's normal. The number that matters is 'available'.",
		command:   "free -h",
	},
	{
		title:     "What's running in the background?",
		narration: "systemd manages background services. `systemctl list-units --type=service --state=running` shows them all. We'll show the first 15.",
		command:   "systemctl list-units --type=service --state=running --no-pager --no-legend --plain | head -15",
	},
	{
		title:     "Who else is logged in?",
		narration: "On a real VPS this would show every active SSH session. On a fresh sandbox it's just you.",
		command:   "who",
	},
	{
		title:     "What's your IP address?",
		narration: "Your VM has an IP on the host's private network. This is how vpsbox reaches it.",
		command:   "ip -4 addr show | grep -E 'inet ' | awk '{print $2, $NF}'",
	},
	{
		title:     "What's the system uptime and load?",
		narration: "`uptime` is the most concise health check on Linux: how long it's been up, how many users, and the 1/5/15-minute load averages.",
		command:   "uptime",
	},
	{
		title:     "You're done with the tour.",
		narration: "You now know how to log in, look around, check resources, and see what's running. Try `vpsbox learn` for hands-on missions, or `vpsbox explain <command>` whenever a tutorial uses something unfamiliar.",
		command:   "",
	},
}

// Tour walks the user through the sandbox by running a curated sequence of
// commands and printing friendly narration between each step.
func (m *Manager) Tour(ctx context.Context, name string, interactive bool) error {
	instance, err := m.Info(ctx, name)
	if err != nil {
		return err
	}

	fmt.Printf("\n  vpsbox tour — a guided walk around %s\n", instance.Name)
	fmt.Println("  ────────────────────────────────────────")
	fmt.Println()

	reader := bufio.NewReader(os.Stdin)
	for i, step := range tourSteps {
		fmt.Printf("[%d/%d] %s\n", i+1, len(tourSteps), step.title)
		fmt.Printf("       %s\n", wrapText(step.narration, 70, "       "))
		if step.command != "" {
			fmt.Printf("\n       $ %s\n", step.command)
			stdout, stderr, runErr := m.runRemoteOn(ctx, instance, step.command)
			output := strings.TrimRight(stdout, "\n")
			if output == "" && stderr != "" {
				output = strings.TrimRight(stderr, "\n")
			}
			for _, line := range strings.Split(output, "\n") {
				if line == "" {
					continue
				}
				fmt.Printf("       │ %s\n", line)
			}
			if runErr != nil {
				fmt.Printf("       (command exited with error: %v)\n", runErr)
			}
		}
		fmt.Println()
		if interactive && i < len(tourSteps)-1 {
			fmt.Print("       press Enter to continue (q to quit) ")
			line, _ := reader.ReadString('\n')
			if strings.TrimSpace(strings.ToLower(line)) == "q" {
				fmt.Println()
				return nil
			}
			fmt.Println()
		}
	}
	return nil
}

// wrapText is a tiny word-wrapper for narration. It assumes the first line
// already has the right indent printed by the caller.
func wrapText(text string, width int, indent string) string {
	var b strings.Builder
	line := 0
	for i, word := range strings.Fields(text) {
		if i == 0 {
			b.WriteString(word)
			line = len(word)
			continue
		}
		if line+1+len(word) > width {
			b.WriteByte('\n')
			b.WriteString(indent)
			b.WriteString(word)
			line = len(word)
			continue
		}
		b.WriteByte(' ')
		b.WriteString(word)
		line += 1 + len(word)
	}
	return b.String()
}

func newTourCommand(ctx context.Context, manager *Manager) *cobra.Command {
	var noPause bool
	cmd := &cobra.Command{
		Use:   "tour [name]",
		Short: "Guided walkthrough — vpsbox runs commands inside your sandbox and explains what they do",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return manager.Tour(ctx, firstArg(args), !noPause)
		},
	}
	cmd.Flags().BoolVar(&noPause, "no-pause", false, "don't wait for Enter between steps")
	return cmd
}
