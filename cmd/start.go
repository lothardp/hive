package cmd

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/lothardp/hive/internal/cell"
	"github.com/lothardp/hive/internal/shell"
	"github.com/spf13/cobra"
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Launch the Hive dashboard",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		hiveBin, err := os.Executable()
		if err != nil {
			return fmt.Errorf("finding hive binary: %w", err)
		}

		exists, err := app.TmuxMgr.SessionExists(ctx, "hive")
		if err != nil {
			return fmt.Errorf("checking tmux session: %w", err)
		}

		if !exists {
			svc := app.NewCellService()
			summary, err := svc.ResumeAll(ctx)
			if err != nil {
				slog.Warn("resume failed", "error", err)
			} else if line := formatResumeSummary(summary); line != "" {
				fmt.Println(line)
			}

			slog.Info("creating dashboard session", "binary", hiveBin)
			home, _ := os.UserHomeDir()
			res, err := shell.Run(ctx, "tmux", "new-session", "-d", "-s", "hive", "-c", home, hiveBin, "dashboard")
			if err != nil {
				return fmt.Errorf("creating dashboard session: %w", err)
			}
			if res.ExitCode != 0 {
				return fmt.Errorf("creating dashboard session: %s", res.Stderr)
			}
		}

		slog.Info("joining dashboard session")
		return app.TmuxMgr.JoinSession("hive")
	},
}

func formatResumeSummary(s *cell.ResumeSummary) string {
	if s == nil {
		return ""
	}
	if len(s.Resumed) == 0 && len(s.Failed) == 0 {
		return ""
	}

	var b strings.Builder
	switch {
	case len(s.Resumed) > 0 && len(s.Failed) == 0:
		fmt.Fprintf(&b, "resumed %d %s: %s",
			len(s.Resumed), cellsWord(len(s.Resumed)), strings.Join(s.Resumed, ", "))
	case len(s.Resumed) > 0:
		fmt.Fprintf(&b, "resumed %d %s (%d failed): %s",
			len(s.Resumed), cellsWord(len(s.Resumed)), len(s.Failed), strings.Join(s.Resumed, ", "))
	default:
		fmt.Fprintf(&b, "resume: 0 succeeded, %d failed", len(s.Failed))
	}
	for _, f := range s.Failed {
		fmt.Fprintf(&b, "\n  %s: %v", f.Name, f.Error)
	}
	return b.String()
}

func cellsWord(n int) string {
	if n == 1 {
		return "cell"
	}
	return "cells"
}

func init() {
	rootCmd.AddCommand(startCmd)
}
