package cmd

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/lothar/hive/internal/config"
	"github.com/lothar/hive/internal/state"
	"github.com/lothar/hive/internal/tmux"
	"github.com/lothar/hive/internal/worktree"
	"github.com/spf13/cobra"
)

type App struct {
	DB       *sql.DB
	Repo     *state.CellRepository
	Config   *config.ProjectConfig
	HiveDir  string
	RepoDir  string
	Project  string
	Verbose  bool
	WtMgr    *worktree.Manager
	TmuxMgr  *tmux.Manager
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

		// Detect git repo — not required for all commands
		ctx := cmd.Context()
		repoDir, err := worktree.DetectRepoRoot(ctx)
		if err == nil {
			app.RepoDir = repoDir
			app.Config = config.LoadOrDefault(repoDir)

			project, err := worktree.DetectProject(ctx, repoDir)
			if err == nil {
				app.Project = project
			}
		}

		// Set up worktree manager
		baseDir, err := worktree.DefaultBaseDir()
		if err != nil {
			return fmt.Errorf("getting workspace base dir: %w", err)
		}
		app.WtMgr = worktree.NewManager(baseDir)
		app.TmuxMgr = tmux.NewManager()

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
