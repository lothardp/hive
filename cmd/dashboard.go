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
			app.CellRepo,
			app.NotifRepo,
			app.TmuxMgr,
			app.CloneMgr,
			app.Config,
			app.HiveDir,
			app.DB,
		)

		p := tea.NewProgram(m, tea.WithAltScreen())
		_, err := p.Run()
		if err != nil {
			return fmt.Errorf("running dashboard: %w", err)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(dashboardCmd)
}
