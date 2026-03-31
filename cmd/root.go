package cmd

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/lothardp/hive/internal/config"
	"github.com/lothardp/hive/internal/shell"
	"github.com/lothardp/hive/internal/state"
	"github.com/lothardp/hive/internal/tmux"
	"github.com/lothardp/hive/internal/worktree"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

type App struct {
	DB         *sql.DB
	Repo       *state.CellRepository
	ConfigRepo *state.ConfigRepository
	RepoRepo   *state.RepoRepository
	NotifRepo  *state.NotificationRepository
	Config     *config.ProjectConfig
	RepoRecord *state.Repo // registered repo for current dir, or nil
	HiveDir    string
	RepoDir    string
	Project    string
	Verbose    bool
	WtMgr      *worktree.Manager
	TmuxMgr    *tmux.Manager
}

var app App

var rootCmd = &cobra.Command{
	Use:   "hive",
	Short: "Spawn isolated, parallel development environments",
	Long:  "Hive is a CLI tool for managing isolated dev environments using Git Worktrees, Docker Compose, and Caddy reverse proxy.",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if app.Verbose {
			slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})))
		}

		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("getting home directory: %w", err)
		}
		app.HiveDir = filepath.Join(home, ".hive")
		if err := os.MkdirAll(app.HiveDir, 0o755); err != nil {
			return fmt.Errorf("creating hive directory: %w", err)
		}

		dbPath := filepath.Join(app.HiveDir, "state.db")
		db, err := state.Open(dbPath)
		if err != nil {
			return fmt.Errorf("opening state database: %w", err)
		}
		app.DB = db
		app.Repo = state.NewCellRepository(db)
		app.ConfigRepo = state.NewConfigRepository(db)
		app.RepoRepo = state.NewRepoRepository(db)
		app.NotifRepo = state.NewNotificationRepository(db)

		// Detect git repo — not required for all commands
		ctx := cmd.Context()
		repoDir, err := worktree.DetectRepoRoot(ctx)
		if err == nil {
			// If inside a child cell, use the queen's dir for repo/config lookup
			// so that DB-first config resolves to the registered repo path.
			// TODO: For worktrees accessed outside of Hive tmux sessions (where
			// HIVE_QUEEN_DIR is not set), consider using git rev-parse
			// --git-common-dir to resolve back to the main repo root.
			if queenDir := os.Getenv("HIVE_QUEEN_DIR"); queenDir != "" {
				repoDir = queenDir
			}
			app.RepoDir = repoDir

			project, err := worktree.DetectProject(ctx, repoDir)
			if err == nil {
				app.Project = project
			}

			// DB-first config: look up registered repo, fall back to .hive.yaml
			repoRecord, err := app.RepoRepo.GetByPath(ctx, repoDir)
			if err == nil && repoRecord != nil {
				app.RepoRecord = repoRecord
				cfg, err := config.ProjectConfigFromJSON(repoRecord.Config)
				if err == nil {
					app.Config = cfg
				} else {
					slog.Warn("failed to parse repo config from DB, using defaults", "error", err)
					app.Config = config.LoadOrDefault(repoDir)
				}
			} else {
				app.Config = config.LoadOrDefault(repoDir)
			}
		}

		// Set up worktree manager — use cells_dir from DB if set
		baseDir, err := worktree.DefaultBaseDir()
		if err != nil {
			return fmt.Errorf("getting cells base dir: %w", err)
		}
		if cellsDir, err := app.ConfigRepo.Get(ctx, "cells_dir"); err == nil && cellsDir != "" {
			baseDir = cellsDir
		}
		app.WtMgr = worktree.NewManager(baseDir)
		app.TmuxMgr = tmux.NewManager()

		// Verify queen branch integrity (skip if no repo detected or killing a queen)
		if app.Project != "" {
			queen, err := app.Repo.GetQueen(ctx, app.Project)
			if err == nil && queen != nil {
				// Skip the check for "kill" targeting the queen itself
				isKillingQueen := cmd.Name() == "kill" && len(args) > 0 && args[0] == queen.Name
				if !isKillingQueen {
					currentBranch, err := queenCurrentBranch(ctx, queen.WorktreePath)
					if err == nil && currentBranch != queen.Branch {
						return fmt.Errorf(
							"queen %q is on branch %q but should be on %q — switch it back before using Hive",
							queen.Name, currentBranch, queen.Branch,
						)
					}
				}
			}
		}

		return nil
	},
	PersistentPostRunE: func(cmd *cobra.Command, args []string) error {
		if app.DB != nil {
			return app.DB.Close()
		}
		return nil
	},
}

func init() {
	rootCmd.PersistentFlags().BoolVarP(&app.Verbose, "verbose", "v", false, "Enable verbose logging")
}

func Execute() error {
	return rootCmd.Execute()
}

// waitForKeypress puts the terminal in raw mode and waits for a single keypress.
func waitForKeypress() {
	fmt.Println("\nPress any key to close")
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		// Fallback: just wait for Enter
		buf := make([]byte, 1)
		_, _ = os.Stdin.Read(buf)
		return
	}
	defer term.Restore(int(os.Stdin.Fd()), oldState)
	buf := make([]byte, 1)
	_, _ = os.Stdin.Read(buf)
}

func queenCurrentBranch(ctx context.Context, dir string) (string, error) {
	res, err := shell.RunInDir(ctx, dir, "git", "symbolic-ref", "--short", "HEAD")
	if err != nil || res.ExitCode != 0 {
		return "", fmt.Errorf("detecting branch: %w", err)
	}
	return strings.TrimSpace(res.Stdout), nil
}
