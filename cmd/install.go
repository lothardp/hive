package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

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

		// Generate tmux.conf
		tmuxConfPath := filepath.Join(app.HiveDir, "tmux.conf")
		if _, err := os.Stat(tmuxConfPath); os.IsNotExist(err) {
			tmuxConf := "# Hive tmux configuration\n# This file is managed by Hive. Manual edits may be overwritten.\n#\n# Keybindings will be added here by future Hive versions.\n"
			if err := os.WriteFile(tmuxConfPath, []byte(tmuxConf), 0o644); err != nil {
				return fmt.Errorf("writing tmux.conf: %w", err)
			}
			fmt.Printf("Created %s\n", tmuxConfPath)
		} else {
			fmt.Printf("Tmux config already exists: %s\n", tmuxConfPath)
		}

		// Check if tmux.conf is sourced
		printTmuxInstructions(tmuxConfPath)

		// Prompt for projects directory
		home, _ := os.UserHomeDir()
		defaultDir := filepath.Join(home, "side_projects")

		// Use existing value as default if set
		existing, _ := app.ConfigRepo.Get(ctx, "projects_dir")
		if existing != "" {
			defaultDir = existing
		}

		fmt.Printf("\nProjects directory [%s]: ", defaultDir)
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)
		if input == "" {
			input = defaultDir
		}

		// Expand ~ if present
		if strings.HasPrefix(input, "~/") {
			input = filepath.Join(home, input[2:])
		}

		// Create if it doesn't exist
		if _, err := os.Stat(input); os.IsNotExist(err) {
			fmt.Printf("Directory %s does not exist. Create it? [Y/n] ", input)
			answer, _ := reader.ReadString('\n')
			answer = strings.TrimSpace(strings.ToLower(answer))
			if answer == "" || answer == "y" {
				if err := os.MkdirAll(input, 0o755); err != nil {
					return fmt.Errorf("creating projects directory: %w", err)
				}
				fmt.Printf("Created %s\n", input)
			}
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
