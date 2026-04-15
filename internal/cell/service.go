package cell

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/lothardp/hive/internal/clone"
	"github.com/lothardp/hive/internal/config"
	"github.com/lothardp/hive/internal/envars"
	"github.com/lothardp/hive/internal/hooks"
	"github.com/lothardp/hive/internal/layout"
	"github.com/lothardp/hive/internal/ports"
	"github.com/lothardp/hive/internal/state"
	"github.com/lothardp/hive/internal/tmux"
)

// Service encapsulates cell lifecycle operations (create, kill, list).
type Service struct {
	CellRepo *state.CellRepository
	CloneMgr *clone.Manager
	TmuxMgr  *tmux.Manager
	HiveDir  string
	DB       *sql.DB
}

// CreateOpts holds the parameters for creating a new cell.
type CreateOpts struct {
	Project  string // e.g. "my-api"
	Name     string // e.g. "work"
	RepoPath string // e.g. ~/side_projects/my-api
}

// CreateResult holds the outcome of a successful cell creation.
type CreateResult struct {
	CellName  string         // "my-api-work"
	ClonePath string         // absolute path to clone directory
	Ports     map[string]int // allocated ports (may be nil)
	HookLog   string         // summary of hook execution
	LayoutLog string         // summary of layout application
}

// Create provisions a new cell: clone, tmux session, hooks, layout, DB record.
// On failure at any step, everything created so far is rolled back.
func (s *Service) Create(ctx context.Context, opts CreateOpts) (*CreateResult, error) {
	cellName := opts.Project + "-" + opts.Name
	slog.Info("creating cell", "name", cellName, "project", opts.Project, "repo", opts.RepoPath)

	// Check cell doesn't already exist.
	existing, err := s.CellRepo.GetByName(ctx, cellName)
	if err != nil {
		return nil, fmt.Errorf("checking cell existence: %w", err)
	}
	if existing != nil {
		return nil, fmt.Errorf("cell %q already exists", cellName)
	}

	// Load project config.
	projectCfg := config.LoadProjectOrDefault(s.HiveDir, opts.Project)

	// Remove any orphaned clone directory (e.g. from a previous failed kill).
	orphanPath := filepath.Join(s.CloneMgr.CellsDir, opts.Project, opts.Name)
	if s.CloneMgr.Exists(orphanPath) {
		_ = s.CloneMgr.Remove(orphanPath)
	}

	// Clone the repo.
	slog.Info("cloning repo", "cell", cellName, "from", opts.RepoPath)
	clonePath, err := s.CloneMgr.Clone(ctx, opts.RepoPath, opts.Project, opts.Name)
	if err != nil {
		return nil, fmt.Errorf("cloning repo: %w", err)
	}
	slog.Info("clone complete", "cell", cellName, "path", clonePath)

	// From here on, rollback clone on failure.
	rollbackClone := func() {
		if rmErr := s.CloneMgr.Remove(clonePath); rmErr != nil {
			slog.Warn("rollback: failed to remove clone", "path", clonePath, "error", rmErr)
		}
	}

	// Allocate ports if configured.
	var allocatedPorts map[string]int
	if len(projectCfg.PortVars) > 0 {
		allocator := ports.NewAllocator(s.DB)
		allocatedPorts, err = allocator.Allocate(ctx, projectCfg.PortVars)
		if err != nil {
			rollbackClone()
			return nil, fmt.Errorf("allocating ports: %w", err)
		}
		slog.Info("ports allocated", "cell", cellName, "ports", allocatedPorts)
	}

	// Build environment variables.
	envVars := envars.BuildVars(allocatedPorts, projectCfg.Env)
	envVars["HIVE_CELL"] = cellName
	envVars["HIVE_REPO_DIR"] = opts.RepoPath

	// Kill any orphaned tmux session with this name (e.g. from a previous failed kill).
	_ = s.TmuxMgr.KillSession(ctx, cellName)

	// Create tmux session.
	if err := s.TmuxMgr.CreateSession(ctx, cellName, clonePath, envVars); err != nil {
		rollbackClone()
		return nil, fmt.Errorf("creating tmux session: %w", err)
	}

	// From here on, rollback tmux + clone on failure.
	rollbackTmuxAndClone := func() {
		if killErr := s.TmuxMgr.KillSession(ctx, cellName); killErr != nil {
			slog.Warn("rollback: failed to kill tmux session", "name", cellName, "error", killErr)
		}
		rollbackClone()
	}

	// Run hooks if any.
	var hookLog string
	if len(projectCfg.Hooks) > 0 {
		slog.Info("running hooks", "cell", cellName, "count", len(projectCfg.Hooks))
		runner := hooks.NewRunner()
		result := runner.Run(ctx, clonePath, projectCfg.Hooks, envVars)
		hookLog = fmt.Sprintf("hooks: %d/%d ran", result.Ran, result.Total)
		if result.Failed != nil {
			hookLog += fmt.Sprintf(", failed: %s", result.Failed.Error())
			slog.Warn("hook failed", "cell", cellName, "hook", result.Failed.Index, "error", result.Failed.Error())
		} else {
			slog.Info("hooks complete", "cell", cellName, "ran", result.Ran, "total", result.Total)
		}
	}

	// Apply default layout if it exists.
	var layoutLog string
	if lyt, ok := projectCfg.Layouts["default"]; ok {
		if err := layout.Apply(ctx, cellName, clonePath, lyt); err != nil {
			layoutLog = fmt.Sprintf("layout error: %v", err)
			slog.Warn("failed to apply default layout", "cell", cellName, "error", err)
		} else {
			layoutLog = "default layout applied"
		}
	}

	// Marshal ports to JSON.
	portsJSON := "{}"
	if len(allocatedPorts) > 0 {
		data, err := json.Marshal(allocatedPorts)
		if err != nil {
			rollbackTmuxAndClone()
			return nil, fmt.Errorf("marshaling ports: %w", err)
		}
		portsJSON = string(data)
	}

	// Create DB record.
	cell := &state.Cell{
		Name:      cellName,
		Project:   opts.Project,
		ClonePath: clonePath,
		Status:    state.StatusRunning,
		Ports:     portsJSON,
		Type:      state.TypeNormal,
	}
	if err := s.CellRepo.Create(ctx, cell); err != nil {
		rollbackTmuxAndClone()
		return nil, fmt.Errorf("saving cell to database: %w", err)
	}

	slog.Info("cell created", "name", cellName, "path", clonePath)

	return &CreateResult{
		CellName:  cellName,
		ClonePath: clonePath,
		Ports:     allocatedPorts,
		HookLog:   hookLog,
		LayoutLog: layoutLog,
	}, nil
}

// Kill tears down a cell: tmux session, clone directory, DB record.
func (s *Service) Kill(ctx context.Context, cellName string) error {
	slog.Info("killing cell", "name", cellName)
	cell, err := s.CellRepo.GetByName(ctx, cellName)
	if err != nil {
		return fmt.Errorf("looking up cell: %w", err)
	}
	if cell == nil {
		return fmt.Errorf("cell %q not found", cellName)
	}

	// Kill tmux session (best-effort).
	if err := s.TmuxMgr.KillSession(ctx, cellName); err != nil {
		slog.Warn("failed to kill tmux session", "name", cellName, "error", err)
	}

	// Remove clone directory for normal cells (best-effort).
	if cell.Type != state.TypeHeadless && cell.ClonePath != "" {
		if err := s.CloneMgr.Remove(cell.ClonePath); err != nil {
			slog.Warn("failed to remove clone directory", "path", cell.ClonePath, "error", err)
		}
	}

	// Delete DB record.
	if err := s.CellRepo.Delete(ctx, cellName); err != nil {
		return fmt.Errorf("deleting cell record: %w", err)
	}

	slog.Info("cell killed", "name", cellName)
	return nil
}

// HeadlessOpts holds the parameters for creating a headless cell.
type HeadlessOpts struct {
	Name    string // cell name
	Project string // optional project name (for grouping in DB)
	Dir     string // optional working directory (defaults to ~)
}

// CreateHeadless provisions a headless cell: tmux session + DB record, no clone.
func (s *Service) CreateHeadless(ctx context.Context, opts HeadlessOpts) (*CreateResult, error) {
	slog.Info("creating headless cell", "name", opts.Name)
	// Check cell doesn't already exist.
	existing, err := s.CellRepo.GetByName(ctx, opts.Name)
	if err != nil {
		return nil, fmt.Errorf("checking cell existence: %w", err)
	}
	if existing != nil {
		return nil, fmt.Errorf("cell %q already exists", opts.Name)
	}

	// Resolve working directory.
	dir := opts.Dir
	if dir == "" {
		dir, err = os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("getting home directory: %w", err)
		}
	}

	// Create tmux session.
	envVars := map[string]string{"HIVE_CELL": opts.Name}
	if err := s.TmuxMgr.CreateSession(ctx, opts.Name, dir, envVars); err != nil {
		return nil, fmt.Errorf("creating tmux session: %w", err)
	}

	// Create DB record.
	cell := &state.Cell{
		Name:      opts.Name,
		Project:   opts.Project,
		ClonePath: dir,
		Status:    state.StatusRunning,
		Ports:     "{}",
		Type:      state.TypeHeadless,
	}
	if err := s.CellRepo.Create(ctx, cell); err != nil {
		// Rollback tmux session.
		if killErr := s.TmuxMgr.KillSession(ctx, opts.Name); killErr != nil {
			slog.Warn("rollback: failed to kill tmux session", "name", opts.Name, "error", killErr)
		}
		return nil, fmt.Errorf("saving cell to database: %w", err)
	}

	return &CreateResult{
		CellName:  opts.Name,
		ClonePath: dir,
	}, nil
}

// List returns all cells from the database.
func (s *Service) List(ctx context.Context) ([]state.Cell, error) {
	return s.CellRepo.List(ctx)
}
