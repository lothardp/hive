package cmd

import (
	"fmt"

	"github.com/lothardp/hive/internal/state"
	"github.com/spf13/cobra"
)

var cellBranch string

var cellCmd = &cobra.Command{
	Use:   "cell <name>",
	Short: "Create a new cell (worktree + tmux session)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		name := args[0]

		if app.RepoDir == "" {
			return fmt.Errorf("not in a git repository — run this from inside a project")
		}

		// Check if cell already exists
		existing, err := app.Repo.GetByName(ctx, name)
		if err != nil {
			return fmt.Errorf("checking existing cell: %w", err)
		}
		if existing != nil {
			return fmt.Errorf("cell %q already exists (status: %s)", name, existing.Status)
		}

		branch := cellBranch
		if branch == "" {
			branch = name
		}

		// Create worktree
		wtPath, err := app.WtMgr.Create(ctx, app.RepoDir, app.Project, name, branch)
		if err != nil {
			return fmt.Errorf("creating worktree: %w", err)
		}

		// Record in state DB
		cell := &state.Cell{
			Name:         name,
			Project:      app.Project,
			Branch:       branch,
			WorktreePath: wtPath,
			Status:       state.StatusStopped,
			Ports:        "{}",
		}
		if err := app.Repo.Create(ctx, cell); err != nil {
			_ = app.WtMgr.Remove(ctx, app.RepoDir, wtPath)
			return fmt.Errorf("recording cell: %w", err)
		}

		// Create tmux session
		if err := app.TmuxMgr.CreateSession(ctx, name, wtPath); err != nil {
			_ = app.Repo.Delete(ctx, name)
			_ = app.WtMgr.Remove(ctx, app.RepoDir, wtPath)
			return fmt.Errorf("creating tmux session: %w", err)
		}

		fmt.Printf("Cell %q created\n", name)
		fmt.Printf("  Branch:   %s\n", branch)
		fmt.Printf("  Worktree: %s\n", wtPath)
		fmt.Printf("  Tmux:     %s\n", name)
		return nil
	},
}

func init() {
	cellCmd.Flags().StringVarP(&cellBranch, "branch", "b", "", "Git branch name (defaults to cell name)")
	rootCmd.AddCommand(cellCmd)
}
