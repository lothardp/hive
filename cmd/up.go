package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var upCmd = &cobra.Command{
	Use:   "up <name>",
	Short: "Start services for an existing cell (docker compose + proxy)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		name := args[0]

		cell, err := app.Repo.GetByName(ctx, name)
		if err != nil {
			return fmt.Errorf("looking up cell: %w", err)
		}
		if cell == nil {
			return fmt.Errorf("cell %q not found — create it first with: hive cell %s", name, name)
		}

		_ = ctx // TODO: Phase 3 — docker compose up
		// TODO: Phase 4 — register proxy route
		// TODO: update status to running

		fmt.Println("not implemented: up", name)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(upCmd)
}
