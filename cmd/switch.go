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

var switchCmd = &cobra.Command{
	Use:   "switch",
	Short: "Fuzzy-find and switch to a cell",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		cells, err := app.CellRepo.List(ctx)
		if err != nil {
			return fmt.Errorf("listing cells: %w", err)
		}
		if len(cells) == 0 {
			fmt.Println("No cells.")
			return nil
		}

		items := make([]tui.SwitchItem, len(cells))
		for i, c := range cells {
			alive, _ := app.TmuxMgr.SessionExists(ctx, c.Name)
			items[i] = tui.SwitchItem{Cell: c, TmuxAlive: alive}
		}

		m := tui.NewSwitcherModel(items)
		p := tea.NewProgram(m)
		result, err := p.Run()
		if err != nil {
			return fmt.Errorf("running switcher: %w", err)
		}

		final := result.(tui.SwitcherModel)
		name := final.Selected()
		if name == "" {
			return nil
		}

		// Inside tmux: use switch-client (popup-safe)
		if os.Getenv("TMUX") != "" {
			res, err := shell.Run(ctx, "tmux", "switch-client", "-t", name)
			if err != nil {
				return fmt.Errorf("switching to cell: %w", err)
			}
			if res.ExitCode != 0 {
				return fmt.Errorf("switching to cell: %s", strings.TrimSpace(res.Stderr))
			}
			return nil
		}

		// Outside tmux: attach
		return app.TmuxMgr.JoinSession(name)
	},
}

func init() {
	rootCmd.AddCommand(switchCmd)
}
