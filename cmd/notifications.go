package cmd

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lothardp/hive/internal/shell"
	"github.com/lothardp/hive/internal/tui"
	"github.com/spf13/cobra"
)

var notificationsCmd = &cobra.Command{
	Use:   "notifications",
	Short: "Browse notifications and jump to source",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		m := tui.NewNotifsPickerModel(app.NotifRepo)
		p := tea.NewProgram(m, tea.WithAltScreen())
		result, err := p.Run()
		if err != nil {
			return fmt.Errorf("running notifications: %w", err)
		}

		sel := result.(tui.NotifsPickerModel).Result()
		if sel == nil {
			return nil
		}

		// Switch to the selected pane/session (same logic as switchToPane)
		if os.Getenv("TMUX") != "" {
			if sel.PaneID != "" {
				res, err := shell.Run(ctx, "tmux", "switch-client", "-t", sel.PaneID)
				if err == nil && res.ExitCode == 0 {
					return nil
				}
			}
			res, err := shell.Run(ctx, "tmux", "switch-client", "-t", sel.CellName)
			if err != nil {
				return fmt.Errorf("switching to cell: %w", err)
			}
			if res.ExitCode != 0 {
				return fmt.Errorf("switching to cell: %s", strings.TrimSpace(res.Stderr))
			}
			return nil
		}

		return app.TmuxMgr.JoinSession(sel.CellName)
	},
}

func init() {
	rootCmd.AddCommand(notificationsCmd)
}
