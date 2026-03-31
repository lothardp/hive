package cmd

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lothardp/hive/internal/tui"
	"github.com/spf13/cobra"
)

var dashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Interactive TUI overview of all cells",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		m := tui.NewModel(
			app.Repo,
			app.NotifRepo,
			app.TmuxMgr,
			app.WtMgr,
			app.DB,
			app.RepoDir,
		)

		p := tea.NewProgram(m, tea.WithAltScreen())
		result, err := p.Run()
		if err != nil {
			return fmt.Errorf("running dashboard: %w", err)
		}

		// After TUI exits, check if user selected a cell to switch to
		final, ok := result.(tui.Model)
		if !ok {
			return nil
		}
		if final.SwitchTarget == "" {
			return nil
		}

		ctx := cmd.Context()

		// Recreate session if it's dead
		exists, _ := app.TmuxMgr.SessionExists(ctx, final.SwitchTarget)
		if !exists {
			cell, err := app.Repo.GetByName(ctx, final.SwitchTarget)
			if err == nil && cell != nil {
				_ = app.TmuxMgr.CreateSession(ctx, cell.Name, cell.WorktreePath, nil)
			}
		}

		return app.TmuxMgr.JoinSession(final.SwitchTarget)
	},
}

func init() {
	rootCmd.AddCommand(dashboardCmd)
}
