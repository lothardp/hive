package cell

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/lothardp/hive/internal/config"
	"github.com/lothardp/hive/internal/envars"
	"github.com/lothardp/hive/internal/layout"
	"github.com/lothardp/hive/internal/state"
)

// ResumeSummary describes the outcome of ResumeAll.
type ResumeSummary struct {
	Resumed []string
	Skipped []SkippedCell
	Failed  []FailedCell
}

type SkippedCell struct {
	Name   string
	Reason string
}

type FailedCell struct {
	Name  string
	Error error
}

// Recreate rehydrates the tmux session for a single cell. Uses stored ports,
// skips hooks, re-applies the default layout. Any existing session with the
// same name is killed first, so Recreate is safe whether the session is alive
// or dead.
func (s *Service) Recreate(ctx context.Context, name string) error {
	c, err := s.CellRepo.GetByName(ctx, name)
	if err != nil {
		return fmt.Errorf("looking up cell: %w", err)
	}
	if c == nil {
		return fmt.Errorf("cell %q not found", name)
	}

	summary := &ResumeSummary{}
	s.recreateOne(ctx, *c, summary)

	if len(summary.Failed) > 0 {
		return summary.Failed[0].Error
	}
	if len(summary.Skipped) > 0 {
		return fmt.Errorf("cannot recreate %q: %s", name, summary.Skipped[0].Reason)
	}
	return nil
}

// recreateOne dispatches a single cell to the type-specific resume helper.
func (s *Service) recreateOne(ctx context.Context, c state.Cell, summary *ResumeSummary) {
	switch c.Type {
	case state.TypeHeadless:
		s.resumeHeadless(ctx, c, summary)
	case state.TypeMulti:
		s.resumeMulti(ctx, c, summary)
	case state.TypeMultiChild:
		s.resumeMultiChild(ctx, c, summary)
	default:
		s.resumeNormal(ctx, c, summary)
	}
}

// ResumeAll recreates tmux sessions for cells in the DB that aren't currently
// running. Uses stored ports, skips hooks, re-applies the default layout for
// cells that have a clone. Per-cell failures are collected into the summary;
// only top-level tmux/DB errors abort.
func (s *Service) ResumeAll(ctx context.Context) (*ResumeSummary, error) {
	sessions, err := s.TmuxMgr.ListSessions(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing tmux sessions: %w", err)
	}
	tmuxSet := make(map[string]bool, len(sessions))
	for _, name := range sessions {
		if name == "hive" {
			continue
		}
		tmuxSet[name] = true
	}

	cells, err := s.CellRepo.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing cells: %w", err)
	}

	summary := &ResumeSummary{}

	for _, c := range cells {
		if tmuxSet[c.Name] {
			summary.Skipped = append(summary.Skipped, SkippedCell{Name: c.Name, Reason: "already running"})
			continue
		}

		s.recreateOne(ctx, c, summary)
	}

	slog.Info("resume complete",
		"resumed", len(summary.Resumed),
		"skipped", len(summary.Skipped),
		"failed", len(summary.Failed),
	)
	return summary, nil
}

func (s *Service) resumeNormal(ctx context.Context, c state.Cell, summary *ResumeSummary) {
	s.resumeClonedCell(ctx, c, nil, summary)
}

func (s *Service) resumeMultiChild(ctx context.Context, c state.Cell, summary *ResumeSummary) {
	parentDir := filepath.Dir(c.ClonePath)
	extra := map[string]string{
		"HIVE_MULTICELL":     c.Parent,
		"HIVE_MULTICELL_DIR": parentDir,
		"HIVE_PROJECT":       c.Project,
	}
	s.resumeClonedCell(ctx, c, extra, summary)
}

// resumeClonedCell rehydrates a cell whose state is rooted in a clone dir:
// normal cells and multicell children. Both types reuse stored ports, skip
// hooks, and re-apply the default layout. extraEnv overlays any type-specific
// env vars (e.g. HIVE_MULTICELL*) on top of the standard set.
func (s *Service) resumeClonedCell(ctx context.Context, c state.Cell, extraEnv map[string]string, summary *ResumeSummary) {
	if c.ClonePath == "" {
		summary.Skipped = append(summary.Skipped, SkippedCell{Name: c.Name, Reason: "no clone dir"})
		slog.Info("skipping resume: empty clone path", "cell", c.Name)
		return
	}
	if _, err := os.Stat(c.ClonePath); err != nil {
		summary.Skipped = append(summary.Skipped, SkippedCell{Name: c.Name, Reason: "no clone dir"})
		slog.Info("skipping resume: clone dir missing", "cell", c.Name, "path", c.ClonePath)
		return
	}

	ports := map[string]int{}
	if c.Ports != "" && c.Ports != "{}" {
		if err := json.Unmarshal([]byte(c.Ports), &ports); err != nil {
			summary.Failed = append(summary.Failed, FailedCell{Name: c.Name, Error: fmt.Errorf("parsing ports: %w", err)})
			return
		}
	}

	projectCfg := config.LoadProjectOrDefault(s.HiveDir, c.Project)

	env := envars.BuildVars(ports, projectCfg.Env)
	env["HIVE_CELL"] = c.Name
	if projectCfg.RepoPath != "" {
		env["HIVE_REPO_DIR"] = config.ExpandTilde(projectCfg.RepoPath)
	}
	for k, v := range extraEnv {
		env[k] = v
	}

	_ = s.TmuxMgr.KillSession(ctx, c.Name)
	if err := s.TmuxMgr.CreateSession(ctx, c.Name, c.ClonePath, env); err != nil {
		summary.Failed = append(summary.Failed, FailedCell{Name: c.Name, Error: fmt.Errorf("creating tmux session: %w", err)})
		return
	}

	if lyt, ok := projectCfg.Layouts["default"]; ok {
		if err := layout.Apply(ctx, c.Name, c.ClonePath, lyt); err != nil {
			slog.Warn("failed to apply default layout on resume", "cell", c.Name, "error", err)
		}
	}

	summary.Resumed = append(summary.Resumed, c.Name)
	slog.Info("resumed cell", "name", c.Name, "type", c.Type)
}

func (s *Service) resumeHeadless(ctx context.Context, c state.Cell, summary *ResumeSummary) {
	dir := c.ClonePath
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			summary.Failed = append(summary.Failed, FailedCell{Name: c.Name, Error: fmt.Errorf("resolving home directory: %w", err)})
			return
		}
		dir = home
	}

	env := map[string]string{"HIVE_CELL": c.Name}

	_ = s.TmuxMgr.KillSession(ctx, c.Name)
	if err := s.TmuxMgr.CreateSession(ctx, c.Name, dir, env); err != nil {
		summary.Failed = append(summary.Failed, FailedCell{Name: c.Name, Error: fmt.Errorf("creating tmux session: %w", err)})
		return
	}

	summary.Resumed = append(summary.Resumed, c.Name)
	slog.Info("resumed cell", "name", c.Name, "type", c.Type)
}

func (s *Service) resumeMulti(ctx context.Context, c state.Cell, summary *ResumeSummary) {
	if c.ClonePath == "" {
		summary.Skipped = append(summary.Skipped, SkippedCell{Name: c.Name, Reason: "no parent dir"})
		slog.Info("skipping resume: empty parent dir", "cell", c.Name)
		return
	}
	if _, err := os.Stat(c.ClonePath); err != nil {
		summary.Skipped = append(summary.Skipped, SkippedCell{Name: c.Name, Reason: "no parent dir"})
		slog.Info("skipping resume: parent dir missing", "cell", c.Name, "path", c.ClonePath)
		return
	}

	env := map[string]string{
		"HIVE_CELL":          c.Name,
		"HIVE_MULTICELL":     c.Name,
		"HIVE_MULTICELL_DIR": c.ClonePath,
	}

	_ = s.TmuxMgr.KillSession(ctx, c.Name)
	if err := s.TmuxMgr.CreateSession(ctx, c.Name, c.ClonePath, env); err != nil {
		summary.Failed = append(summary.Failed, FailedCell{Name: c.Name, Error: fmt.Errorf("creating tmux session: %w", err)})
		return
	}

	summary.Resumed = append(summary.Resumed, c.Name)
	slog.Info("resumed cell", "name", c.Name, "type", c.Type)
}
