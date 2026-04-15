package cmd

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/lothardp/hive/internal/shell"
	"github.com/spf13/cobra"
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Launch the Hive dashboard",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		// Get path to current hive binary
		hiveBin, err := os.Executable()
		if err != nil {
			return fmt.Errorf("finding hive binary: %w", err)
		}

		// Check if hive dashboard session exists
		exists, err := app.TmuxMgr.SessionExists(ctx, "hive")
		if err != nil {
			return fmt.Errorf("checking tmux session: %w", err)
		}

		if !exists {
			// Create tmux session running `hive dashboard`
			slog.Info("creating dashboard session", "binary", hiveBin)
			home, _ := os.UserHomeDir()
			res, err := shell.Run(ctx, "tmux", "new-session", "-d", "-s", "hive", "-c", home, hiveBin, "dashboard")
			if err != nil {
				return fmt.Errorf("creating dashboard session: %w", err)
			}
			if res.ExitCode != 0 {
				return fmt.Errorf("creating dashboard session: %s", res.Stderr)
			}
		}

		// Attach or switch to the session
		slog.Info("joining dashboard session")
		return app.TmuxMgr.JoinSession("hive")
	},
}

func init() {
	rootCmd.AddCommand(startCmd)
}
