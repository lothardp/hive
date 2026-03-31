package cmd

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/lothardp/hive/internal/config"
	"github.com/lothardp/hive/internal/hooks"
	"github.com/lothardp/hive/internal/layout"
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

		// Run setup hooks
		var hookSummary string
		if app.Config != nil && len(app.Config.Hooks) > 0 {
			runner := hooks.NewRunner()
			result := runner.Run(ctx, wtPath, app.Config.Hooks)
			if result.Failed != nil {
				hookSummary = fmt.Sprintf("%d/%d failed at hook %d (see hook_results.txt)", result.Ran, result.Total, result.Failed.Index)
			} else {
				hookSummary = fmt.Sprintf("%d/%d passed", result.Ran, result.Total)
			}
		}

		// Apply default layout (repo-level, then global)
		var layoutSummary string
		if lyt, ok := resolveLayout(ctx, app.Config); ok {
			if err := layout.Apply(ctx, name, wtPath, lyt); err != nil {
				layoutSummary = fmt.Sprintf("failed: %v", err)
			} else {
				layoutSummary = "applied"
			}
		}

		fmt.Printf("Cell %q created\n", name)
		fmt.Printf("  Branch:   %s\n", branch)
		fmt.Printf("  Worktree: %s\n", wtPath)
		fmt.Printf("  Tmux:     %s\n", name)
		if hookSummary != "" {
			fmt.Printf("  Hooks:    %s\n", hookSummary)
		}
		if layoutSummary != "" {
			fmt.Printf("  Layout:   %s\n", layoutSummary)
		}
		return nil
	},
}

// resolveLayout returns the "default" layout, checking repo config first then global config.
func resolveLayout(ctx context.Context, cfg *config.ProjectConfig) (config.Layout, bool) {
	// Check repo-level layouts
	if cfg != nil && cfg.Layouts != nil {
		if lyt, ok := cfg.Layouts["default"]; ok {
			return lyt, true
		}
	}
	// Check global layouts
	raw, err := app.ConfigRepo.Get(ctx, "layouts")
	if err != nil || raw == "" {
		return config.Layout{}, false
	}
	var globalLayouts map[string]config.Layout
	if err := json.Unmarshal([]byte(raw), &globalLayouts); err != nil {
		return config.Layout{}, false
	}
	if lyt, ok := globalLayouts["default"]; ok {
		return lyt, true
	}
	return config.Layout{}, false
}

func init() {
	cellCmd.Flags().StringVarP(&cellBranch, "branch", "b", "", "Git branch name (defaults to cell name)")
	rootCmd.AddCommand(cellCmd)
}
