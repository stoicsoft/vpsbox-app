package app

import (
	"fmt"
	"sort"

	"github.com/stoicsoft/vpsbox/internal/explain"
	"github.com/spf13/cobra"
)

func newExplainCommand(_ *Manager) *cobra.Command {
	return &cobra.Command{
		Use:   "explain [command]",
		Short: "Plain-English explainer for common Linux commands you'll meet in tutorials",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				printExplainList()
				return nil
			}
			entry, ok := explain.Entries[args[0]]
			if !ok {
				fmt.Printf("vpsbox doesn't have a friendly explanation for %q yet.\n", args[0])
				fmt.Println()
				fmt.Printf("Try the system docs:   man %s\n", args[0])
				fmt.Printf("Or the cheat-sheet:    tldr %s\n", args[0])
				return nil
			}
			renderExplain(entry)
			return nil
		},
	}
}

func printExplainList() {
	names := make([]string, 0, len(explain.Entries))
	for name := range explain.Entries {
		names = append(names, name)
	}
	sort.Strings(names)
	fmt.Println("Commands vpsbox can explain in plain English:")
	fmt.Println()
	for _, n := range names {
		fmt.Printf("  %-12s %s\n", n, explain.Entries[n].Summary)
	}
	fmt.Println()
	fmt.Println("Try: vpsbox explain chmod")
}

func renderExplain(e explain.Entry) {
	fmt.Printf("# %s\n\n", e.Command)
	fmt.Printf("%s\n\n", e.Summary)
	fmt.Println("What it does:")
	fmt.Printf("  %s\n\n", e.WhatItDoes)
	if e.Pitfalls != "" {
		fmt.Println("Watch out for:")
		fmt.Printf("  %s\n\n", e.Pitfalls)
	}
	if len(e.Examples) > 0 {
		fmt.Println("Examples:")
		for _, ex := range e.Examples {
			fmt.Printf("  $ %s\n", ex)
		}
		fmt.Println()
	}
}
