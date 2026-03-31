package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/lothardp/hive/internal/shell"
	"github.com/lothardp/hive/internal/state"
	"github.com/spf13/cobra"
)

var killCurrent bool

var killCmd = &cobra.Command{
	Use:               "kill [name]",
	Short:             "Tear down everything: containers, proxy, worktree, tmux session",
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: completeCellNames,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		var name string
		if killCurrent {
			name = os.Getenv("HIVE_CELL")
			if name == "" {
				return fmt.Errorf("not in a Hive cell (HIVE_CELL not set)")
			}
			// Confirm interactively
			fmt.Printf("Kill cell %q? [y/N] ", name)
			reader := bufio.NewReader(os.Stdin)
			answer, _ := reader.ReadString('\n')
			if strings.TrimSpace(strings.ToLower(answer)) != "y" {
				fmt.Println("Cancelled")
				return nil
			}
		} else {
			if len(args) == 0 {
				return fmt.Errorf("cell name required (or use --current)")
			}
			name = args[0]
		}

		cell, err := app.Repo.GetByName(ctx, name)
		if err != nil {
			return fmt.Errorf("looking up cell: %w", err)
		}
		if cell == nil {
			return fmt.Errorf("cell %q not found", name)
		}

		// When killing current cell, switch to queen first so user lands somewhere safe
		if killCurrent {
			queenName := os.Getenv("HIVE_QUEEN")
			if queenName != "" && name != queenName {
				_, _ = shell.Run(ctx, "tmux", "switch-client", "-t", queenName)
			}
		}

		// Queen: refuse to kill if other cells exist for the project
		if cell.Type == state.TypeQueen {
			others, err := app.Repo.CountByProject(ctx, cell.Project, state.TypeQueen)
			if err != nil {
				return fmt.Errorf("checking project cells: %w", err)
			}
			if others > 0 {
				return fmt.Errorf("cannot kill queen %q — %d other cell(s) still exist for project %q; kill them first", name, others, cell.Project)
			}

			if err := app.TmuxMgr.KillSession(ctx, name); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: failed to kill tmux session: %v\n", err)
			}
			if err := app.Repo.Delete(ctx, name); err != nil {
				return fmt.Errorf("deleting cell record: %w", err)
			}
			fmt.Printf("Queen %q killed\n", name)
			return nil
		}

		if cell.Type == state.TypeHeadless {
			if err := app.TmuxMgr.KillSession(ctx, name); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: failed to kill tmux session: %v\n", err)
			}
			if err := app.Repo.Delete(ctx, name); err != nil {
				return fmt.Errorf("deleting cell record: %w", err)
			}
			fmt.Printf("Headless cell %q killed\n", name)
			return nil
		}

		// Normal cell path — requires git repo
		if app.RepoDir == "" {
			return fmt.Errorf("not in a git repository — run this from inside a project")
		}

		// TODO: Phase 3 — docker compose down -v
		// TODO: Phase 4 — remove proxy route

		// Kill tmux session
		if err := app.TmuxMgr.KillSession(ctx, name); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: failed to kill tmux session: %v\n", err)
		}

		// Remove worktree
		if err := app.WtMgr.Remove(ctx, app.RepoDir, cell.WorktreePath); err != nil {
			return fmt.Errorf("removing worktree: %w", err)
		}

		// Delete branch (best-effort — skip if it's the current branch or default branch)
		if err := app.WtMgr.DeleteBranch(ctx, app.RepoDir, cell.Branch); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: failed to delete branch %q: %v\n", cell.Branch, err)
		}

		// Delete from state
		if err := app.Repo.Delete(ctx, name); err != nil {
			return fmt.Errorf("deleting cell record: %w", err)
		}

		fmt.Printf("Cell %q killed\n", name)
		return nil
	},
}

func init() {
	killCmd.Flags().BoolVar(&killCurrent, "current", false, "Kill the current cell (reads HIVE_CELL from env)")
	rootCmd.AddCommand(killCmd)
}
