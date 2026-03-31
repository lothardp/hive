package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/lothardp/hive/internal/keybindings"
	"github.com/lothardp/hive/internal/shell"
	"github.com/spf13/cobra"
)

var keybindingsCmd = &cobra.Command{
	Use:   "keybindings",
	Short: "Regenerate tmux keybindings and reload",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		leader, _ := app.ConfigRepo.Get(ctx, "keybinding_leader")
		if leader == "" {
			leader = keybindings.DefaultLeader
		}

		tmuxVer, _ := keybindings.TmuxVersion(ctx)
		content := keybindings.Generate(leader, tmuxVer)

		tmuxConfPath := filepath.Join(app.HiveDir, "tmux.conf")
		if err := os.WriteFile(tmuxConfPath, []byte(content), 0o644); err != nil {
			return fmt.Errorf("writing tmux.conf: %w", err)
		}
		fmt.Printf("Wrote %s\n", tmuxConfPath)

		// Best-effort reload into tmux
		if res, err := shell.Run(ctx, "tmux", "source-file", tmuxConfPath); err == nil && res.ExitCode == 0 {
			fmt.Println("Keybindings reloaded into tmux")
		}

		fmt.Println()
		for _, b := range keybindings.Bindings(leader) {
			fmt.Printf("  %s\n", b)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(keybindingsCmd)
}
