package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

var switchCmd = &cobra.Command{
	Use:   "switch",
	Short: "Switch between cells using fzf",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		cells, err := app.Repo.List(ctx)
		if err != nil {
			return fmt.Errorf("listing cells: %w", err)
		}

		if len(cells) == 0 {
			fmt.Println("No cells. Create one with: hive cell <name>")
			return nil
		}

		// Build fzf input: "name (branch) [project]"
		var lines []string
		for _, c := range cells {
			lines = append(lines, fmt.Sprintf("%s\t(%s)\t[%s]", c.Name, c.Branch, c.Project))
		}
		input := strings.Join(lines, "\n")

		// Run fzf
		fzf := exec.CommandContext(ctx, "fzf",
			"--height=~50%",
			"--layout=reverse",
			"--border",
			"--prompt=cell> ",
			"--header=Switch to cell",
			"--tabstop=4",
		)
		fzf.Stdin = strings.NewReader(input)
		fzf.Stderr = os.Stderr
		out, err := fzf.Output()
		if err != nil {
			// User pressed escape / ctrl-c
			return nil
		}

		// Extract cell name (first field before tab)
		selected := strings.TrimSpace(string(out))
		name, _, _ := strings.Cut(selected, "\t")
		name = strings.TrimSpace(name)

		if name == "" {
			return nil
		}

		// Ensure tmux session exists
		cell, err := app.Repo.GetByName(ctx, name)
		if err != nil {
			return fmt.Errorf("looking up cell: %w", err)
		}
		if cell == nil {
			return fmt.Errorf("cell %q not found", name)
		}

		exists, err := app.TmuxMgr.SessionExists(ctx, name)
		if err != nil {
			return fmt.Errorf("checking tmux session: %w", err)
		}
		if !exists {
			if err := app.TmuxMgr.CreateSession(ctx, name, cell.WorktreePath); err != nil {
				return fmt.Errorf("creating tmux session: %w", err)
			}
		}

		return app.TmuxMgr.JoinSession(name)
	},
}

func init() {
	rootCmd.AddCommand(switchCmd)
}
