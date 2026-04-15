package cmd

import (
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"os"

	"path/filepath"

	"github.com/lothardp/hive/internal/clone"
	"github.com/lothardp/hive/internal/config"
	"github.com/lothardp/hive/internal/state"
	"github.com/lothardp/hive/internal/tmux"
	"github.com/spf13/cobra"
)

type App struct {
	DB        *sql.DB
	CellRepo  *state.CellRepository
	NotifRepo *state.NotificationRepository
	Config    *config.GlobalConfig
	HiveDir   string
	Verbose   bool
	LogFile   *os.File
	CloneMgr  *clone.Manager
	TmuxMgr   *tmux.Manager
}

var app App

var rootCmd = &cobra.Command{
	Use:   "hive",
	Short: "Spawn isolated, parallel development environments",
	Long:  "Hive is a CLI tool for managing isolated dev environments using Git clones and tmux.",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("getting home directory: %w", err)
		}
		app.HiveDir = filepath.Join(home, ".hive")
		if err := os.MkdirAll(app.HiveDir, 0o755); err != nil {
			return fmt.Errorf("creating hive directory: %w", err)
		}

		// Set up file logging
		logPath := filepath.Join(app.HiveDir, "hive.log")
		logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return fmt.Errorf("opening log file: %w", err)
		}
		app.LogFile = logFile

		var logWriter io.Writer = logFile
		logLevel := slog.LevelInfo
		if app.Verbose {
			logWriter = io.MultiWriter(logFile, os.Stderr)
			logLevel = slog.LevelDebug
		}
		slog.SetDefault(slog.New(slog.NewTextHandler(logWriter, &slog.HandlerOptions{Level: logLevel})))

		// Open state DB
		dbPath := filepath.Join(app.HiveDir, "state.db")
		db, err := state.Open(dbPath)
		if err != nil {
			return fmt.Errorf("opening state database: %w", err)
		}
		app.DB = db
		app.CellRepo = state.NewCellRepository(db)
		app.NotifRepo = state.NewNotificationRepository(db)

		// Load global config
		app.Config = config.LoadGlobalOrDefault(app.HiveDir)
		slog.Info("config loaded", "cells_dir", app.Config.CellsDir, "project_dirs", app.Config.ProjectDirs)

		// Init managers
		cellsDir := app.Config.ResolveCellsDir()
		app.CloneMgr = clone.NewManager(cellsDir)
		app.TmuxMgr = tmux.NewManager()

		return nil
	},
	PersistentPostRunE: func(cmd *cobra.Command, args []string) error {
		if app.LogFile != nil {
			app.LogFile.Close()
		}
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
