package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lothardp/hive/internal/config"
	"github.com/lothardp/hive/internal/envars"
	"github.com/lothardp/hive/internal/hooks"
	"github.com/lothardp/hive/internal/layout"
	"github.com/lothardp/hive/internal/ports"
	"github.com/lothardp/hive/internal/state"
	"github.com/spf13/cobra"
)

var (
	cellBranch   string
	cellHeadless bool
)

var cellCmd = &cobra.Command{
	Use:   "cell <name> [dir]",
	Short: "Create a new cell (worktree + tmux session)",
	Args:  cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		name := args[0]

		// Check if cell already exists
		existing, err := app.Repo.GetByName(ctx, name)
		if err != nil {
			return fmt.Errorf("checking existing cell: %w", err)
		}
		if existing != nil {
			return fmt.Errorf("cell %q already exists (status: %s)", name, existing.Status)
		}

		// Headless path — no worktree, no git repo required
		if cellHeadless {
			if cellBranch != "" {
				return fmt.Errorf("--headless and --branch cannot be used together")
			}
			return createHeadlessCell(ctx, cmd, name, args)
		}

		// Normal cell path — requires git repo
		if !cellHeadless && len(args) > 1 {
			return fmt.Errorf("unexpected argument %q — did you mean --headless?", args[1])
		}

		if app.RepoDir == "" {
			return fmt.Errorf("not in a git repository — run this from inside a project")
		}

		// Auto-create queen session if needed
		if err := createQueen(ctx, cmd); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: failed to create queen session: %v\n", err)
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
			Type:         state.TypeNormal,
		}
		if err := app.Repo.Create(ctx, cell); err != nil {
			_ = app.WtMgr.Remove(ctx, app.RepoDir, wtPath)
			return fmt.Errorf("recording cell: %w", err)
		}

		// Allocate ports
		var allocatedPorts map[string]int
		if app.Config != nil && len(app.Config.PortVars) > 0 {
			allocator := ports.NewAllocator(app.DB)
			allocatedPorts, err = allocator.Allocate(ctx, app.Config.PortVars)
			if err != nil {
				_ = app.Repo.Delete(ctx, name)
				_ = app.WtMgr.Remove(ctx, app.RepoDir, wtPath)
				return fmt.Errorf("allocating ports: %w", err)
			}

			portsJSON, _ := json.Marshal(allocatedPorts)
			cell.Ports = string(portsJSON)
			if err := app.Repo.UpdatePorts(ctx, name, cell.Ports); err != nil {
				_ = app.Repo.Delete(ctx, name)
				_ = app.WtMgr.Remove(ctx, app.RepoDir, wtPath)
				return fmt.Errorf("saving port allocations: %w", err)
			}
		}

		// Build env vars for tmux session
		var staticEnv map[string]string
		if app.Config != nil {
			staticEnv = app.Config.Env
		}
		envVars := envars.BuildVars(allocatedPorts, staticEnv)
		envVars["HIVE_CELL"] = name

		// Inject HIVE_QUEEN_DIR if queen exists
		queen, err := app.Repo.GetQueen(ctx, app.Project)
		if err == nil && queen != nil {
			envVars["HIVE_QUEEN_DIR"] = queen.WorktreePath
		}

		// Create tmux session with env vars baked in
		if err := app.TmuxMgr.CreateSession(ctx, name, wtPath, envVars); err != nil {
			_ = app.Repo.Delete(ctx, name)
			_ = app.WtMgr.Remove(ctx, app.RepoDir, wtPath)
			return fmt.Errorf("creating tmux session: %w", err)
		}

		// Run setup hooks
		var hookSummary string
		if app.Config != nil && len(app.Config.Hooks) > 0 {
			// Build hook env — same as tmux env, plus queen dir
			hookEnv := make(map[string]string)
			for k, v := range envVars {
				hookEnv[k] = v
			}

			runner := hooks.NewRunner()
			result := runner.Run(ctx, wtPath, app.Config.Hooks, hookEnv)
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
		if len(allocatedPorts) > 0 {
			fmt.Printf("  Ports:    %s\n", formatPorts(allocatedPorts))
		}
		if hookSummary != "" {
			fmt.Printf("  Hooks:    %s\n", hookSummary)
		}
		if layoutSummary != "" {
			fmt.Printf("  Layout:   %s\n", layoutSummary)
		}
		return nil
	},
}

func createQueen(ctx context.Context, cmd *cobra.Command) error {
	queenName := app.Project + "-queen"

	// Already exists?
	existing, err := app.Repo.GetQueen(ctx, app.Project)
	if err != nil {
		return fmt.Errorf("checking queen: %w", err)
	}
	if existing != nil {
		return nil // queen already exists
	}

	// Determine default branch
	defaultBranch := "main"
	if app.RepoRecord != nil && app.RepoRecord.DefaultBranch != "" {
		defaultBranch = app.RepoRecord.DefaultBranch
	}

	// Verify the repo dir is currently on the default branch
	currentBranch, err := queenCurrentBranch(ctx, app.RepoDir)
	if err != nil {
		return fmt.Errorf("detecting current branch: %w", err)
	}
	if currentBranch != defaultBranch {
		return fmt.Errorf(
			"repo is on branch %q but queen requires %q — checkout %q first",
			currentBranch, defaultBranch, defaultBranch,
		)
	}

	// Record in DB — WorktreePath is the repo dir itself
	cell := &state.Cell{
		Name:         queenName,
		Project:      app.Project,
		Branch:       defaultBranch,
		WorktreePath: app.RepoDir,
		Status:       state.StatusStopped,
		Ports:        "{}",
		Type:         state.TypeQueen,
	}
	if err := app.Repo.Create(ctx, cell); err != nil {
		return fmt.Errorf("recording queen: %w", err)
	}

	// Create tmux session pointing at the repo dir
	env := map[string]string{"HIVE_CELL": queenName}
	if err := app.TmuxMgr.CreateSession(ctx, queenName, app.RepoDir, env); err != nil {
		_ = app.Repo.Delete(ctx, queenName)
		return fmt.Errorf("creating queen tmux session: %w", err)
	}

	fmt.Printf("Queen session %q created on branch %q\n", queenName, defaultBranch)
	return nil
}

func createHeadlessCell(ctx context.Context, cmd *cobra.Command, name string, args []string) error {
	// Resolve working directory — optional second positional arg
	dir := "."
	if len(args) > 1 {
		dir = args[1]
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolving directory: %w", err)
	}

	// Verify directory exists
	info, err := os.Stat(absDir)
	if err != nil {
		return fmt.Errorf("directory %q: %w", absDir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%q is not a directory", absDir)
	}

	// Record in DB
	cell := &state.Cell{
		Name:         name,
		Project:      app.Project, // may be empty if not in a git repo
		Branch:       "",
		WorktreePath: absDir,
		Status:       state.StatusStopped,
		Ports:        "{}",
		Type:         state.TypeHeadless,
	}
	if err := app.Repo.Create(ctx, cell); err != nil {
		return fmt.Errorf("recording cell: %w", err)
	}

	// Build env vars (static env only, no ports)
	envVars := map[string]string{"HIVE_CELL": name}
	if app.Config != nil {
		for k, v := range app.Config.Env {
			envVars[k] = v
		}
	}

	// Create tmux session
	if err := app.TmuxMgr.CreateSession(ctx, name, absDir, envVars); err != nil {
		_ = app.Repo.Delete(ctx, name)
		return fmt.Errorf("creating tmux session: %w", err)
	}

	fmt.Printf("Headless cell %q created\n", name)
	fmt.Printf("  Dir:   %s\n", absDir)
	fmt.Printf("  Tmux:  %s\n", name)
	return nil
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

// formatPorts returns a display string like "3001 (PORT), 5433 (DB_PORT)".
func formatPorts(ports map[string]int) string {
	type entry struct {
		name string
		port int
	}
	entries := make([]entry, 0, len(ports))
	for k, v := range ports {
		entries = append(entries, entry{k, v})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].port < entries[j].port })

	parts := make([]string, len(entries))
	for i, e := range entries {
		parts[i] = fmt.Sprintf("%d (%s)", e.port, e.name)
	}
	return strings.Join(parts, ", ")
}

func init() {
	cellCmd.Flags().StringVarP(&cellBranch, "branch", "b", "", "Git branch name (defaults to cell name)")
	cellCmd.Flags().BoolVar(&cellHeadless, "headless", false, "Create a tmux session without a git worktree")
	rootCmd.AddCommand(cellCmd)
}
