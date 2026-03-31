package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var jumpCmd = &cobra.Command{
	Use:               "jump <cell>",
	Short:             "Mark notifications read and attach to a cell",
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

		count, err := app.NotifRepo.MarkReadByCell(ctx, name)
		if err != nil {
			return fmt.Errorf("marking notifications read: %w", err)
		}
		if count > 0 {
			fmt.Printf("Marked %d notification(s) as read\n", count)
		}

		exists, err := app.TmuxMgr.SessionExists(ctx, name)
		if err != nil {
			return fmt.Errorf("checking tmux session: %w", err)
		}
		if !exists {
			if err := app.TmuxMgr.CreateSession(ctx, name, cell.WorktreePath, nil); err != nil {
				return fmt.Errorf("creating tmux session: %w", err)
			}
		}

		return app.TmuxMgr.JoinSession(name)
	},
}

func init() {
	rootCmd.AddCommand(jumpCmd)
}
