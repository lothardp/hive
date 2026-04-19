package cell

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

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
	CellRepo      *state.CellRepository
	CloneMgr      *clone.Manager
	TmuxMgr       *tmux.Manager
	HiveDir       string
	MulticellsDir string
	DB            *sql.DB
}

// CreateOpts holds the parameters for creating a new cell.
type CreateOpts struct {
	Project    string // e.g. "my-api"
	Name       string // e.g. "work"
	RepoPath   string // e.g. ~/side_projects/my-api
	OnProgress func(string)
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
	OnProgress func(string)      // optional; called with short step labels
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
	emit := func(line string) {
		if opts.OnProgress != nil {
			opts.OnProgress(line)
		}
	}

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
	projectLabel := opts.Project
	if projectLabel == "" {
		projectLabel = opts.CellName
	}
	emit("cloning " + projectLabel)
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
		emit("allocating ports")
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
		onHook := func(idx, total int, cmd string) {
			preview := cmd
			if len(preview) > 60 {
				preview = preview[:57] + "..."
			}
			emit(fmt.Sprintf("running hook %d/%d: %s", idx, total, preview))
		}
		result := runner.Run(ctx, opts.TargetPath, projectCfg.Hooks, envVars, onHook)
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

	emit := func(line string) {
		if opts.OnProgress != nil {
			opts.OnProgress(line)
		}
	}

	targetPath := filepath.Join(s.CloneMgr.CellsDir, opts.Project, opts.Name)

	// Clone + ports + env + hooks. Rolls back on any failure.
	prov, err := s.provisionClone(ctx, ProvisionOpts{
		Project:    opts.Project,
		SourceRepo: opts.RepoPath,
		TargetPath: targetPath,
		CellName:   cellName,
		AllocPorts: true,
		OnProgress: opts.OnProgress,
	})
	if err != nil {
		return nil, err
	}

	// Kill any orphaned tmux session with this name.
	_ = s.TmuxMgr.KillSession(ctx, cellName)

	// Create tmux session.
	emit("creating tmux session")
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
		emit("applying layout")
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
	emit("saving cell record")
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

// ProvisionChildOpts describes a single multicell child to provision.
type ProvisionChildOpts struct {
	Project    string // e.g. "api"
	SourceRepo string // git source path/URL
	ParentName string // coordinator multicell name, e.g. "auth-overhaul"
	ParentDir  string // absolute path to the parent dir
	OnProgress func(string)
}

// ProvisionChildResult is the outcome for one successful child.
type ProvisionChildResult struct {
	CellName  string
	ClonePath string
	Ports     map[string]int
	HookLog   string
	LayoutLog string
}

// provisionChildCell provisions a single first-class multicell child end-to-end:
// provisionClone (clone + ports + hooks) → kill orphan tmux → create session →
// apply default layout → insert TypeMultiChild cells row.
//
// On failure, rolls back the child's tmux session and clone dir only. Does not
// touch the parent dir or sibling cells.
func (s *Service) provisionChildCell(ctx context.Context, opts ProvisionChildOpts) (*ProvisionChildResult, error) {
	cellName := opts.Project + "-" + opts.ParentName
	target := filepath.Join(opts.ParentDir, cellName)

	existing, err := s.CellRepo.GetByName(ctx, cellName)
	if err != nil {
		return nil, fmt.Errorf("checking child cell existence: %w", err)
	}
	if existing != nil {
		return nil, fmt.Errorf("child cell %q already exists", cellName)
	}

	emit := func(line string) {
		if opts.OnProgress != nil {
			opts.OnProgress(line)
		}
	}

	prov, err := s.provisionClone(ctx, ProvisionOpts{
		Project:    opts.Project,
		SourceRepo: opts.SourceRepo,
		TargetPath: target,
		CellName:   cellName,
		AllocPorts: true,
		ExtraEnv: map[string]string{
			"HIVE_MULTICELL":     opts.ParentName,
			"HIVE_MULTICELL_DIR": opts.ParentDir,
			"HIVE_PROJECT":       opts.Project,
		},
		OnProgress: opts.OnProgress,
	})
	if err != nil {
		return nil, err
	}

	_ = s.TmuxMgr.KillSession(ctx, cellName)

	emit("creating tmux session")
	if err := s.TmuxMgr.CreateSession(ctx, cellName, prov.ClonePath, prov.Env); err != nil {
		if rmErr := os.RemoveAll(prov.ClonePath); rmErr != nil {
			slog.Warn("rollback: failed to remove child clone", "path", prov.ClonePath, "error", rmErr)
		}
		return nil, fmt.Errorf("creating tmux session: %w", err)
	}

	rollback := func() {
		if killErr := s.TmuxMgr.KillSession(ctx, cellName); killErr != nil {
			slog.Warn("rollback: failed to kill child tmux session", "name", cellName, "error", killErr)
		}
		if rmErr := os.RemoveAll(prov.ClonePath); rmErr != nil {
			slog.Warn("rollback: failed to remove child clone", "path", prov.ClonePath, "error", rmErr)
		}
	}

	layoutLog := ""
	if lyt, ok := prov.Config.Layouts["default"]; ok {
		emit("applying layout")
		if err := layout.Apply(ctx, cellName, prov.ClonePath, lyt); err != nil {
			layoutLog = fmt.Sprintf("layout error: %v", err)
			slog.Warn("failed to apply default layout", "cell", cellName, "error", err)
		} else {
			layoutLog = "default layout applied"
		}
	}

	portsJSON := "{}"
	if len(prov.Ports) > 0 {
		data, err := json.Marshal(prov.Ports)
		if err != nil {
			rollback()
			return nil, fmt.Errorf("marshaling ports: %w", err)
		}
		portsJSON = string(data)
	}

	emit("saving cell record")
	child := &state.Cell{
		Name:      cellName,
		Project:   opts.Project,
		ClonePath: prov.ClonePath,
		Status:    state.StatusRunning,
		Ports:     portsJSON,
		Type:      state.TypeMultiChild,
		Parent:    opts.ParentName,
	}
	if err := s.CellRepo.Create(ctx, child); err != nil {
		rollback()
		return nil, fmt.Errorf("saving child cell %q: %w", cellName, err)
	}

	return &ProvisionChildResult{
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

	// Kill this cell's tmux session (best-effort).
	if err := s.TmuxMgr.KillSession(ctx, cellName); err != nil {
		slog.Warn("failed to kill tmux session", "name", cellName, "error", err)
	}

	switch cell.Type {
	case state.TypeMulti:
		// Kill each child's tmux session; their dirs will vanish with the parent
		// dir below, but their sessions won't self-destruct.
		children, err := s.CellRepo.ListChildren(ctx, cellName)
		if err != nil {
			slog.Warn("listing multicell children", "name", cellName, "error", err)
		}
		for _, child := range children {
			if err := s.TmuxMgr.KillSession(ctx, child.Name); err != nil {
				slog.Warn("failed to kill child tmux session", "name", child.Name, "error", err)
			}
		}
		if cell.ClonePath != "" {
			if err := s.removeMulticellDir(cell.ClonePath); err != nil {
				slog.Warn("failed to remove multicell dir", "path", cell.ClonePath, "error", err)
			}
		}
		// FK ON DELETE CASCADE removes child cell rows when we delete the
		// coordinator row below.

	case state.TypeMultiChild:
		if cell.ClonePath != "" {
			if err := s.removeMulticellDir(cell.ClonePath); err != nil {
				slog.Warn("failed to remove multicell child dir", "path", cell.ClonePath, "error", err)
			}
		}

	case state.TypeHeadless:
		// No clone to remove.

	default:
		if cell.ClonePath != "" {
			if err := s.CloneMgr.Remove(cell.ClonePath); err != nil {
				slog.Warn("failed to remove clone directory", "path", cell.ClonePath, "error", err)
			}
		}
	}

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

// MultiOpts holds the parameters for creating a multicell.
type MultiOpts struct {
	Name       string                     // e.g. "auth-overhaul"
	Projects   []config.DiscoveredProject // selected projects with Name + Path
	OnProgress func(string)
}

// MultiResult holds the outcome of a successful multicell creation.
type MultiResult struct {
	Name       string            // "auth-overhaul"
	ParentDir  string            // "/Users/me/hive/multicells/auth-overhaul"
	Projects   []string          // ["api", "web", "mobile"]
	ClonePaths []string          // absolute paths, same order as Projects
	HookLogs   map[string]string // project name → hook log summary
}

// CreateMulti provisions a multicell: parent dir + coordinator tmux session +
// N first-class child cells (each with its own clone, hooks, ports, tmux
// session, layout, and DB row). On failure at any step, already-created state
// is rolled back.
func (s *Service) CreateMulti(ctx context.Context, opts MultiOpts) (*MultiResult, error) {
	slog.Info("creating multicell", "name", opts.Name, "projects", len(opts.Projects))

	// 1. Name uniqueness across all cell types.
	existing, err := s.CellRepo.GetByName(ctx, opts.Name)
	if err != nil {
		return nil, fmt.Errorf("checking cell existence: %w", err)
	}
	if existing != nil {
		return nil, fmt.Errorf("cell %q already exists", opts.Name)
	}

	// 2. At least one project.
	if len(opts.Projects) == 0 {
		return nil, fmt.Errorf("multicell requires at least one project")
	}

	// 3. No duplicate project names.
	seen := make(map[string]bool, len(opts.Projects))
	for _, p := range opts.Projects {
		if seen[p.Name] {
			return nil, fmt.Errorf("duplicate project %q in multicell", p.Name)
		}
		seen[p.Name] = true
	}

	// 4. Every prospective child cell name must be free too.
	for _, p := range opts.Projects {
		childName := p.Name + "-" + opts.Name
		c, err := s.CellRepo.GetByName(ctx, childName)
		if err != nil {
			return nil, fmt.Errorf("checking child cell existence: %w", err)
		}
		if c != nil {
			return nil, fmt.Errorf("child cell %q would collide with existing cell", childName)
		}
	}

	// 5. Compute parent dir.
	if s.MulticellsDir == "" {
		return nil, fmt.Errorf("multicells directory not configured")
	}
	parentDir := filepath.Join(s.MulticellsDir, opts.Name)

	// 6. Kill any orphaned coordinator tmux session BEFORE the rmdir.
	// A stale session with cwd inside parentDir keeps file handles and races
	// against the wipe.
	_ = s.TmuxMgr.KillSession(ctx, opts.Name)

	// 7. Orphan cleanup + mkdir.
	if err := os.RemoveAll(parentDir); err != nil {
		return nil, fmt.Errorf("removing orphaned multicell dir %q: %w", parentDir, err)
	}
	if err := os.MkdirAll(parentDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating multicell dir %q: %w", parentDir, err)
	}

	emit := func(line string) {
		if opts.OnProgress != nil {
			opts.OnProgress(line)
		}
	}

	// 8. Create coordinator tmux session rooted at parentDir (plain shell,
	// no ports/hooks/layout).
	coordEnv := map[string]string{
		"HIVE_CELL":          opts.Name,
		"HIVE_MULTICELL":     opts.Name,
		"HIVE_MULTICELL_DIR": parentDir,
	}
	emit("creating coordinator session")
	if err := s.TmuxMgr.CreateSession(ctx, opts.Name, parentDir, coordEnv); err != nil {
		if rmErr := os.RemoveAll(parentDir); rmErr != nil {
			slog.Warn("rollback: failed to remove multicell dir", "path", parentDir, "error", rmErr)
		}
		return nil, fmt.Errorf("creating coordinator tmux session: %w", err)
	}

	rollbackCoord := func() {
		if killErr := s.TmuxMgr.KillSession(ctx, opts.Name); killErr != nil {
			slog.Warn("rollback: failed to kill coordinator tmux session", "name", opts.Name, "error", killErr)
		}
		if rmErr := os.RemoveAll(parentDir); rmErr != nil {
			slog.Warn("rollback: failed to remove multicell dir", "path", parentDir, "error", rmErr)
		}
	}

	// 9. Insert coordinator cells row (needed before children for FK).
	coord := &state.Cell{
		Name:      opts.Name,
		Project:   "",
		ClonePath: parentDir,
		Status:    state.StatusRunning,
		Ports:     "{}",
		Type:      state.TypeMulti,
	}
	if err := s.CellRepo.Create(ctx, coord); err != nil {
		rollbackCoord()
		return nil, fmt.Errorf("saving coordinator cell: %w", err)
	}

	// 10. Provision each child cell sequentially. On any failure, tear down
	// already-created children first, then the coordinator.
	projectNames := make([]string, 0, len(opts.Projects))
	clonePaths := make([]string, 0, len(opts.Projects))
	hookLogs := make(map[string]string, len(opts.Projects))
	createdChildNames := make([]string, 0, len(opts.Projects))

	rollbackAll := func() {
		for _, name := range createdChildNames {
			if err := s.TmuxMgr.KillSession(ctx, name); err != nil {
				slog.Warn("rollback: failed to kill child tmux session", "name", name, "error", err)
			}
			if err := s.CellRepo.Delete(ctx, name); err != nil {
				slog.Warn("rollback: failed to delete child cell row", "name", name, "error", err)
			}
		}
		if err := s.CellRepo.Delete(ctx, opts.Name); err != nil {
			slog.Warn("rollback: failed to delete coordinator row", "name", opts.Name, "error", err)
		}
		rollbackCoord()
	}

	for _, p := range opts.Projects {
		var childProgress func(string)
		if opts.OnProgress != nil {
			prefix := "[" + p.Name + "] "
			childProgress = func(s string) { opts.OnProgress(prefix + s) }
		}
		childRes, err := s.provisionChildCell(ctx, ProvisionChildOpts{
			Project:    p.Name,
			SourceRepo: p.Path,
			ParentName: opts.Name,
			ParentDir:  parentDir,
			OnProgress: childProgress,
		})
		if err != nil {
			rollbackAll()
			return nil, fmt.Errorf("provisioning %q: %w", p.Name, err)
		}

		projectNames = append(projectNames, p.Name)
		clonePaths = append(clonePaths, childRes.ClonePath)
		hookLogs[p.Name] = childRes.HookLog
		createdChildNames = append(createdChildNames, childRes.CellName)
	}

	slog.Info("multicell created", "name", opts.Name, "dir", parentDir, "projects", projectNames)

	return &MultiResult{
		Name:       opts.Name,
		ParentDir:  parentDir,
		Projects:   projectNames,
		ClonePaths: clonePaths,
		HookLogs:   hookLogs,
	}, nil
}

// ListMultiChildren returns the first-class child cells registered under a
// multicell coordinator.
func (s *Service) ListMultiChildren(ctx context.Context, multicellName string) ([]state.Cell, error) {
	return s.CellRepo.ListChildren(ctx, multicellName)
}

// removeMulticellDir deletes a path under the multicells dir, refusing to
// touch any path outside of the configured MulticellsDir.
func (s *Service) removeMulticellDir(path string) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolving path: %w", err)
	}
	absRoot, err := filepath.Abs(s.MulticellsDir)
	if err != nil {
		return fmt.Errorf("resolving multicells dir: %w", err)
	}
	if !strings.HasPrefix(absPath, absRoot+string(filepath.Separator)) {
		return fmt.Errorf("refusing to remove %q: not under multicells dir %q", absPath, absRoot)
	}
	return os.RemoveAll(absPath)
}
