package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/lothardp/hive/internal/keybindings"
	"github.com/lothardp/hive/internal/shell"
	"github.com/spf13/cobra"
)

var keybindingsDirect bool

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

		// Resolve direct mode: flag overrides saved preference
		direct := keybindingsDirect
		if cmd.Flags().Changed("direct") {
			// Persist the preference
			val := "false"
			if direct {
				val = "true"
			}
			if err := app.ConfigRepo.Set(ctx, "keybinding_direct", val); err != nil {
				return fmt.Errorf("saving direct preference: %w", err)
			}
		} else {
			// Load saved preference
			if saved, _ := app.ConfigRepo.Get(ctx, "keybinding_direct"); saved == "true" {
				direct = true
			}
		}

		tmuxVer, _ := keybindings.TmuxVersion(ctx)
		content := keybindings.Generate(leader, tmuxVer, direct)

		tmuxConfPath := filepath.Join(app.HiveDir, "tmux.conf")
		if err := os.WriteFile(tmuxConfPath, []byte(content), 0o644); err != nil {
			return fmt.Errorf("writing tmux.conf: %w", err)
		}
		fmt.Printf("Wrote %s\n", tmuxConfPath)

		// Best-effort reload into tmux
		if res, err := shell.Run(ctx, "tmux", "source-file", tmuxConfPath); err == nil && res.ExitCode == 0 {
			fmt.Println("Keybindings reloaded into tmux")
		}

		if direct {
			fmt.Println("\nMode: direct (single keystroke after prefix)")
		} else {
			fmt.Printf("\nMode: table (<prefix> %s, then key)\n", leader)
		}
		for _, b := range keybindings.Bindings(leader, direct) {
			fmt.Printf("  %s\n", b)
		}
		return nil
	},
}

func init() {
	keybindingsCmd.Flags().BoolVar(&keybindingsDirect, "direct", false, "Bind directly to <prefix> <key> instead of using a hive key table")
	rootCmd.AddCommand(keybindingsCmd)
}
