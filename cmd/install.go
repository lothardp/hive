package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lothardp/hive/internal/keybindings"
	"github.com/lothardp/hive/internal/shell"
	"github.com/spf13/cobra"
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "One-time Hive setup for this machine",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		reader := bufio.NewReader(os.Stdin)

		// Check if already installed
		installedAt, _ := app.ConfigRepo.Get(ctx, "installed_at")
		if installedAt != "" {
			fmt.Printf("Hive was already installed on %s\n", installedAt)
			fmt.Print("Re-run setup? [y/N] ")
			answer, _ := reader.ReadString('\n')
			if strings.TrimSpace(strings.ToLower(answer)) != "y" {
				return nil
			}
		}

		fmt.Printf("Hive directory: %s\n", app.HiveDir)

		// Generate tmux.conf with keybindings
		tmuxConfPath := filepath.Join(app.HiveDir, "tmux.conf")
		leader, _ := app.ConfigRepo.Get(ctx, "keybinding_leader")
		if leader == "" {
			leader = keybindings.DefaultLeader
		}
		direct := false
		if saved, _ := app.ConfigRepo.Get(ctx, "keybinding_direct"); saved == "true" {
			direct = true
		}
		tmuxVer, _ := keybindings.TmuxVersion(ctx)
		tmuxConf := keybindings.Generate(leader, tmuxVer, direct)
		if err := os.WriteFile(tmuxConfPath, []byte(tmuxConf), 0o644); err != nil {
			return fmt.Errorf("writing tmux.conf: %w", err)
		}
		fmt.Printf("Wrote %s\n", tmuxConfPath)

		// Best-effort reload into tmux
		if res, err := shell.Run(ctx, "tmux", "source-file", tmuxConfPath); err == nil && res.ExitCode == 0 {
			fmt.Println("Keybindings loaded into tmux")
		}

		// Check if tmux.conf is sourced
		printTmuxInstructions(tmuxConfPath)

		home, _ := os.UserHomeDir()

		// Prompt for cells directory (where worktrees are created)
		defaultCellsDir := filepath.Join(home, ".hive", "cells")
		existingCellsDir, _ := app.ConfigRepo.Get(ctx, "cells_dir")
		if existingCellsDir != "" {
			defaultCellsDir = existingCellsDir
		}

		fmt.Printf("\nCells directory (where worktrees are created) [%s]: ", defaultCellsDir)
		cellsInput, _ := reader.ReadString('\n')
		cellsInput = strings.TrimSpace(cellsInput)
		if cellsInput == "" {
			cellsInput = defaultCellsDir
		}
		if strings.HasPrefix(cellsInput, "~/") {
			cellsInput = filepath.Join(home, cellsInput[2:])
		}

		if _, err := os.Stat(cellsInput); os.IsNotExist(err) {
			fmt.Printf("Directory %s does not exist. Create it? [Y/n] ", cellsInput)
			answer, _ := reader.ReadString('\n')
			answer = strings.TrimSpace(strings.ToLower(answer))
			if answer == "" || answer == "y" {
				if err := os.MkdirAll(cellsInput, 0o755); err != nil {
					return fmt.Errorf("creating cells directory: %w", err)
				}
				fmt.Printf("Created %s\n", cellsInput)
			}
		}

		if err := app.ConfigRepo.Set(ctx, "cells_dir", cellsInput); err != nil {
			return fmt.Errorf("saving cells directory: %w", err)
		}

		// Prompt for projects directory (hint for repo discovery)
		defaultDir := filepath.Join(home, "side_projects")
		existing, _ := app.ConfigRepo.Get(ctx, "projects_dir")
		if existing != "" {
			defaultDir = existing
		}

		fmt.Printf("Projects directory (where your repos live) [%s]: ", defaultDir)
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)
		if input == "" {
			input = defaultDir
		}
		if strings.HasPrefix(input, "~/") {
			input = filepath.Join(home, input[2:])
		}

		if err := app.ConfigRepo.Set(ctx, "projects_dir", input); err != nil {
			return fmt.Errorf("saving projects directory: %w", err)
		}

		// Mark as installed
		if err := app.ConfigRepo.Set(ctx, "installed_at", time.Now().Format(time.RFC3339)); err != nil {
			return fmt.Errorf("saving install timestamp: %w", err)
		}

		fmt.Println("\nHive installed successfully!")
		fmt.Printf("  Config:   %s/state.db\n", app.HiveDir)
		fmt.Printf("  Cells:    %s\n", cellsInput)
		fmt.Printf("  Projects: %s\n", input)
		return nil
	},
}

func printTmuxInstructions(tmuxConfPath string) {
	home, _ := os.UserHomeDir()
	userTmuxConf := filepath.Join(home, ".tmux.conf")
	sourceLine := fmt.Sprintf("source-file %s", tmuxConfPath)

	data, err := os.ReadFile(userTmuxConf)
	if err == nil && strings.Contains(string(data), tmuxConfPath) {
		fmt.Println("Tmux integration: already configured")
		return
	}

	fmt.Println("\nTo enable tmux integration, add this line to your ~/.tmux.conf:")
	fmt.Printf("  %s\n", sourceLine)
}

func init() {
	rootCmd.AddCommand(installCmd)
}
