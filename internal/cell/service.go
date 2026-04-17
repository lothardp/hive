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

// ProvisionOpts holds parameters for the tmux-independent portion of cell setup.
type ProvisionOpts struct {
	Project    string            // for config loading and logging; may be "" for ad-hoc clones
	SourceRepo string            // git source path/URL
	TargetPath string            // absolute path where the clone should land
	CellName   string            // value of HIVE_CELL in the hook env
	AllocPorts bool              // whether to allocate ports per the loaded project config
	ExtraEnv   map[string]string // additional env merged into the hook env (overrides nothing)
}

// ProvisionResult describes a successfully provisioned working directory.
type ProvisionResult struct {
	ClonePath string
	Ports     map[string]int
	Env       map[string]string      // full hook env: static + ports + extra + HIVE_CELL + HIVE_REPO_DIR
	HookLog   string
	Config    *config.ProjectConfig // loaded project config (Layouts etc. for the caller)
}

// provisionClone runs the tmux-independent portion of cell setup:
//
//	orphan cleanup → clone → allocate ports → build env → run hooks.
//
// On any failure, partial state (clone dir, any files hooks created) is rolled
// back before returning the error. Hook failures are fatal here — callers get
// a usable-or-nothing guarantee.
func (s *Service) provisionClone(ctx context.Context, opts ProvisionOpts) (*ProvisionResult, error) {
	// Remove any orphaned clone directory at the target path.
	if _, err := os.Stat(opts.TargetPath); err == nil {
		slog.Info("removing orphaned clone directory", "path", opts.TargetPath)
		if err := os.RemoveAll(opts.TargetPath); err != nil {
			return nil, fmt.Errorf("removing orphaned clone at %q: %w", opts.TargetPath, err)
		}
	}

	// Load project config (may be empty for unknown projects).
	projectCfg := config.LoadProjectOrDefault(s.HiveDir, opts.Project)

	// Clone.
	slog.Info("cloning repo", "cell", opts.CellName, "from", opts.SourceRepo, "to", opts.TargetPath)
	if err := s.CloneMgr.CloneInto(ctx, opts.SourceRepo, opts.TargetPath); err != nil {
		return nil, fmt.Errorf("cloning repo: %w", err)
	}
	slog.Info("clone complete", "cell", opts.CellName, "path", opts.TargetPath)

	rollback := func() {
		if err := os.RemoveAll(opts.TargetPath); err != nil {
			slog.Warn("rollback: failed to remove clone", "path", opts.TargetPath, "error", err)
		}
	}

	// Allocate ports if requested and configured.
	var allocatedPorts map[string]int
	if opts.AllocPorts && len(projectCfg.PortVars) > 0 {
		allocator := ports.NewAllocator(s.DB)
		p, err := allocator.Allocate(ctx, projectCfg.PortVars)
		if err != nil {
			rollback()
			return nil, fmt.Errorf("allocating ports: %w", err)
		}
		allocatedPorts = p
		slog.Info("ports allocated", "cell", opts.CellName, "ports", allocatedPorts)
	}

	// Build environment.
	envVars := envars.BuildVars(allocatedPorts, projectCfg.Env)
	envVars["HIVE_CELL"] = opts.CellName
	envVars["HIVE_REPO_DIR"] = opts.SourceRepo
	for k, v := range opts.ExtraEnv {
		envVars[k] = v
	}

	// Run hooks — fatal on failure.
	hookLog := ""
	if len(projectCfg.Hooks) > 0 {
		slog.Info("running hooks", "cell", opts.CellName, "count", len(projectCfg.Hooks))
		runner := hooks.NewRunner()
		result := runner.Run(ctx, opts.TargetPath, projectCfg.Hooks, envVars)
		hookLog = fmt.Sprintf("hooks: %d/%d ran", result.Ran, result.Total)
		if result.Failed != nil {
			slog.Warn("hook failed", "cell", opts.CellName, "hook", result.Failed.Index, "error", result.Failed.Error())
			rollback()
			return nil, fmt.Errorf("running hooks: %s (see %s/hook_results.txt)",
				result.Failed.Error(), opts.TargetPath)
		}
		slog.Info("hooks complete", "cell", opts.CellName, "ran", result.Ran, "total", result.Total)
	}

	return &ProvisionResult{
		ClonePath: opts.TargetPath,
		Ports:     allocatedPorts,
		Env:       envVars,
		HookLog:   hookLog,
		Config:    projectCfg,
	}, nil
}

// Create provisions a new cell: clone, hooks, tmux session, layout, DB record.
// Hooks run before the tmux session is created, so a failed hook tears down
// the clone cleanly (no half-configured session left behind).
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

	targetPath := filepath.Join(s.CloneMgr.CellsDir, opts.Project, opts.Name)

	// Clone + ports + env + hooks. Rolls back on any failure.
	prov, err := s.provisionClone(ctx, ProvisionOpts{
		Project:    opts.Project,
		SourceRepo: opts.RepoPath,
		TargetPath: targetPath,
		CellName:   cellName,
		AllocPorts: true,
	})
	if err != nil {
		return nil, err
	}

	// Kill any orphaned tmux session with this name.
	_ = s.TmuxMgr.KillSession(ctx, cellName)

	// Create tmux session.
	if err := s.TmuxMgr.CreateSession(ctx, cellName, prov.ClonePath, prov.Env); err != nil {
		if rmErr := os.RemoveAll(prov.ClonePath); rmErr != nil {
			slog.Warn("rollback: failed to remove clone", "path", prov.ClonePath, "error", rmErr)
		}
		return nil, fmt.Errorf("creating tmux session: %w", err)
	}

	rollbackTmuxAndClone := func() {
		if killErr := s.TmuxMgr.KillSession(ctx, cellName); killErr != nil {
			slog.Warn("rollback: failed to kill tmux session", "name", cellName, "error", killErr)
		}
		if rmErr := os.RemoveAll(prov.ClonePath); rmErr != nil {
			slog.Warn("rollback: failed to remove clone", "path", prov.ClonePath, "error", rmErr)
		}
	}

	// Apply default layout if it exists.
	layoutLog := ""
	if lyt, ok := prov.Config.Layouts["default"]; ok {
		if err := layout.Apply(ctx, cellName, prov.ClonePath, lyt); err != nil {
			layoutLog = fmt.Sprintf("layout error: %v", err)
			slog.Warn("failed to apply default layout", "cell", cellName, "error", err)
		} else {
			layoutLog = "default layout applied"
		}
	}

	// Marshal ports to JSON.
	portsJSON := "{}"
	if len(prov.Ports) > 0 {
		data, err := json.Marshal(prov.Ports)
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
		ClonePath: prov.ClonePath,
		Status:    state.StatusRunning,
		Ports:     portsJSON,
		Type:      state.TypeNormal,
	}
	if err := s.CellRepo.Create(ctx, cell); err != nil {
		rollbackTmuxAndClone()
		return nil, fmt.Errorf("saving cell to database: %w", err)
	}

	slog.Info("cell created", "name", cellName, "path", prov.ClonePath)

	return &CreateResult{
		CellName:  cellName,
		ClonePath: prov.ClonePath,
		Ports:     prov.Ports,
		HookLog:   prov.HookLog,
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
