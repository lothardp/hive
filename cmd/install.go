package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lothardp/hive/internal/config"
	"github.com/lothardp/hive/internal/keybindings"
	"github.com/spf13/cobra"
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "One-time Hive setup for this machine",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		reader := bufio.NewReader(os.Stdin)
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("getting home directory: %w", err)
		}
		hiveDir := filepath.Join(home, ".hive")

		// 1. Create ~/.hive/ if missing
		if err := os.MkdirAll(hiveDir, 0o755); err != nil {
			return fmt.Errorf("creating hive directory: %w", err)
		}
		fmt.Printf("Hive directory: %s\n", hiveDir)

		// 2. Create ~/.hive/config/ for per-project configs
		configDir := filepath.Join(hiveDir, "config")
		if err := os.MkdirAll(configDir, 0o755); err != nil {
			return fmt.Errorf("creating config directory: %w", err)
		}

		// 3. Prompt for project directories (comma-separated)
		defaultProjectDirs := filepath.Join(home, "side_projects")
		fmt.Printf("\nProject directories (comma-separated, where your repos live) [%s]: ", defaultProjectDirs)
		projInput, _ := reader.ReadString('\n')
		projInput = strings.TrimSpace(projInput)

		var projectDirs []string
		if projInput == "" {
			projectDirs = []string{defaultProjectDirs}
		} else {
			for _, d := range strings.Split(projInput, ",") {
				d = strings.TrimSpace(d)
				if d == "" {
					continue
				}
				if strings.HasPrefix(d, "~/") {
					d = filepath.Join(home, d[2:])
				}
				projectDirs = append(projectDirs, d)
			}
		}

		// 4. Prompt for cells directory
		defaultCellsDir := filepath.Join(home, "hive", "cells")
		fmt.Printf("Cells directory (where clones are stored) [%s]: ", defaultCellsDir)
		cellsInput, _ := reader.ReadString('\n')
		cellsInput = strings.TrimSpace(cellsInput)
		if cellsInput == "" {
			cellsInput = defaultCellsDir
		}
		if strings.HasPrefix(cellsInput, "~/") {
			cellsInput = filepath.Join(home, cellsInput[2:])
		}

		// 5. Prompt for editor
		defaultEditor := os.Getenv("EDITOR")
		if defaultEditor == "" {
			defaultEditor = "vim"
		}
		fmt.Printf("Editor [%s]: ", defaultEditor)
		editorInput, _ := reader.ReadString('\n')
		editorInput = strings.TrimSpace(editorInput)
		if editorInput == "" {
			editorInput = defaultEditor
		}

		// 6. Write ~/.hive/config.yaml using config.WriteDefaultGlobal
		cfg := &config.GlobalConfig{
			ProjectDirs: projectDirs,
			CellsDir:    cellsInput,
			Editor:       editorInput,
		}
		if err := config.WriteDefaultGlobal(hiveDir, cfg); err != nil {
			return fmt.Errorf("writing global config: %w", err)
		}
		fmt.Printf("Wrote %s\n", filepath.Join(hiveDir, "config.yaml"))

		// 7. Create cells directory if missing
		if _, err := os.Stat(cellsInput); os.IsNotExist(err) {
			if err := os.MkdirAll(cellsInput, 0o755); err != nil {
				return fmt.Errorf("creating cells directory: %w", err)
			}
			fmt.Printf("Created %s\n", cellsInput)
		}

		// 8. Generate ~/.hive/tmux.conf
		tmuxConfPath := filepath.Join(hiveDir, "tmux.conf")
		tmuxConf := keybindings.GenerateTmuxConf()
		if err := os.WriteFile(tmuxConfPath, []byte(tmuxConf), 0o644); err != nil {
			return fmt.Errorf("writing tmux.conf: %w", err)
		}
		fmt.Printf("Wrote %s\n", tmuxConfPath)

		// 9. Print instructions to source tmux.conf
		printTmuxInstructions(tmuxConfPath)

		fmt.Println("\nHive installed successfully!")
		fmt.Printf("  Config:   %s\n", filepath.Join(hiveDir, "config.yaml"))
		fmt.Printf("  Cells:    %s\n", cellsInput)
		fmt.Printf("  Projects: %s\n", strings.Join(projectDirs, ", "))
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
