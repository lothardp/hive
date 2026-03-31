package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var joinCmd = &cobra.Command{
	Use:               "join <name>",
	Short:             "Attach to a cell's tmux session",
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

		// Ensure tmux session exists (re-create if it was lost)
		exists, err := app.TmuxMgr.SessionExists(ctx, name)
		if err != nil {
			return fmt.Errorf("checking tmux session: %w", err)
		}
		if !exists {
			if err := app.TmuxMgr.CreateSession(ctx, name, cell.WorktreePath, nil); err != nil {
				return fmt.Errorf("creating tmux session: %w", err)
			}
		}

		// This replaces the current process (switch if inside tmux, attach otherwise)
		return app.TmuxMgr.JoinSession(name)
	},
}

func init() {
	rootCmd.AddCommand(joinCmd)
}
