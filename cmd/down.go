package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var downCmd = &cobra.Command{
	Use:               "down <name>",
	Short:             "Stop and remove services for a cell (keeps worktree and tmux)",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeCellNames,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		name := args[0]

		cell, err := app.Repo.GetByName(ctx, name)
		if err != nil {
			return fmt.Errorf("looking up cell: %w", err)
		}
		if cell == nil {
			return fmt.Errorf("cell %q not found", name)
		}

		_ = ctx // TODO: Phase 3 — docker compose down
		// TODO: Phase 4 — remove proxy route
		// TODO: update status to stopped

		fmt.Println("not implemented: down", name)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(downCmd)
}
