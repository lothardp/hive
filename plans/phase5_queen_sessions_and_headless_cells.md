# Phase 5: Queen Sessions & Headless Cells

## Overview

Two new cell types that extend the basic cell concept:
1. **Queen sessions** — the repo's own working directory, always checked out on the default branch. Auto-created on first cell for a repo. No worktree — the queen IS the original repo checkout.
2. **Headless cells** — tmux sessions in arbitrary directories with no git worktree. Quick scratch workspaces that still show up in `hive status` and `hive switch`.

Both are tracked in the `cells` table with a new `type` field to distinguish them from normal cells.

---

## Part 1: Data Model Changes

### Cell type

Add a `Type` field to the `Cell` struct to distinguish normal cells, queen sessions, and headless cells:

```go
type CellType string

const (
	TypeNormal   CellType = "normal"
	TypeQueen    CellType = "queen"
	TypeHeadless CellType = "headless"
)

type Cell struct {
	ID           int64
	Name         string
	Project      string
	Branch       string
	WorktreePath string
	Status       CellStatus
	Ports        string
	Type         CellType   // NEW
	CreatedAt    time.Time
	UpdatedAt    time.Time
}
```

### Schema migration

Add the `type` column to the `cells` table. Existing cells default to `"normal"`:

```sql
ALTER TABLE cells ADD COLUMN type TEXT NOT NULL DEFAULT 'normal';
```

This goes in `internal/state/db.go` as a migration step after the initial schema creation:

```go
func runMigrations(db *sql.DB) {
	stmts := []string{
		`ALTER TABLE cells ADD COLUMN type TEXT NOT NULL DEFAULT 'normal'`,
	}
	for _, stmt := range stmts {
		_, _ = db.Exec(stmt) // ignore "duplicate column" errors
	}
}
```

Call `runMigrations(db)` in `Open()` right after the initial schema `CREATE TABLE IF NOT EXISTS` block.

### Repository changes

Update all `scanCell` / `scanCells` queries to include `type`:

```sql
SELECT id, name, project, branch, worktree_path, status, ports, type, created_at, updated_at
FROM cells ...
```

Add a new query method:

```go
func (r *CellRepository) GetQueen(ctx context.Context, project string) (*Cell, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, name, project, branch, worktree_path, status, ports, type, created_at, updated_at
		 FROM cells WHERE project = ? AND type = 'queen'`, project,
	)
	return scanCell(row)
}
```

---

## Part 2: Queen Sessions

### What they are

A queen is the repo's original working directory — not a worktree. For example, if the repo lives at `~/side_projects/hive`, that's the queen's path. It must always be checked out on the default branch (e.g., `main`).

A queen cell:
- Points at the repo's root directory (`app.RepoDir`), not a worktree
- Is always on the default branch — Hive enforces this
- Is auto-created on first `hive cell` for a repo
- Has the name `<project>-queen` (e.g., `hive-queen`)
- Has `Type = "queen"` in the DB
- Gets a tmux session named `<project>-queen`
- Does NOT create a worktree — the repo itself is the working directory
- Does NOT run setup hooks or apply layouts
- Does NOT allocate ports

### Why it exists

1. **Non-intrusive main branch access** — the repo dir stays on main. `hive join hive-queen` takes you there.
2. **Symlink source for hooks** — hooks like `ln -s ../../hive-queen/node_modules` can reference the queen's stable checkout.
3. **Branch integrity guard** — Hive verifies the queen is on the default branch before any operation. If someone accidentally switched the branch, Hive refuses to proceed.

### Queen branch guard

Every hive command that touches a repo (any command that runs in `PersistentPreRunE` with a detected repo) must verify the queen is on the correct branch. This goes in `cmd/root.go` after repo detection:

```go
// Verify queen branch integrity
queen, err := app.Repo.GetQueen(ctx, app.Project)
if err == nil && queen != nil {
	currentBranch, err := queenCurrentBranch(ctx, queen.WorktreePath)
	if err == nil && currentBranch != queen.Branch {
		return fmt.Errorf(
			"queen %q is on branch %q but should be on %q — switch it back before using Hive",
			queen.Name, currentBranch, queen.Branch,
		)
	}
}
```

The helper function:

```go
func queenCurrentBranch(ctx context.Context, dir string) (string, error) {
	res, err := shell.RunInDir(ctx, dir, "git", "symbolic-ref", "--short", "HEAD")
	if err != nil || res.ExitCode != 0 {
		return "", fmt.Errorf("detecting branch: %w", err)
	}
	return strings.TrimSpace(res.Stdout), nil
}
```

This check is skipped when:
- No queen exists for the project (queen hasn't been created yet)
- The command is `hive kill` targeting the queen itself (let the user destroy it)
- The current directory is not a git repo (`app.Project == ""`)

### Auto-creation flow

During `hive cell <name>`, before creating the requested cell:

```
1. Check if queen exists for this project    ← NEW
2. If not, create queen                      ← NEW
3. Create worktree                           (existing)
4. Record in DB                              (existing)
5. Allocate ports                            (existing)
6. Build env vars                            (existing)
7. Create tmux session with env vars         (existing)
8. Run setup hooks                           (existing)
9. Apply layout                              (existing)
10. Print summary                            (existing)
```

Queen creation:

```go
func createQueen(ctx context.Context, cmd *cobra.Command) error {
	queenName := app.Project + "-queen"

	// Already exists?
	existing, err := app.Repo.GetQueen(ctx, app.Project)
	if err != nil {
		return fmt.Errorf("checking queen: %w", err)
	}
	if existing != nil {
		return nil // queen already exists, nothing to do
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
```

### Queen creation failure

If queen creation fails, print a warning and continue with normal cell creation. The queen is a convenience, not a requirement:

```go
if err := createQueen(ctx, cmd); err != nil {
	fmt.Fprintf(cmd.ErrOrStderr(), "warning: failed to create queen session: %v\n", err)
}
```

### Default branch detection

The default branch comes from the registered repo record (`app.RepoRecord.DefaultBranch`). If the repo isn't registered (no `hive setup` was run), fall back to `"main"`.

### Killing queens

`hive kill <project>-queen` kills the tmux session and removes the DB record. It does NOT:
- Remove the repo directory (it's not a worktree Hive created)
- Delete any branch
- Touch the filesystem at all

```go
// In kill.go, after looking up the cell:
if cell.Type == state.TypeQueen {
	if err := app.TmuxMgr.KillSession(ctx, name); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: failed to kill tmux session: %v\n", err)
	}
	if err := app.Repo.Delete(ctx, name); err != nil {
		return fmt.Errorf("deleting cell record: %w", err)
	}
	fmt.Printf("Queen %q killed\n", name)
	return nil
}
```

### Queen in `hive status`

Queens show up in the normal status table with a `[queen]` marker in the NAME column:

```
NAME                PROJECT  BRANCH   STATUS   TMUX   PORTS  AGE
hive-queen [queen]  hive     main     stopped  alive  -      2h
my-feature          hive     my-feat  stopped  alive  3001   1h
```

### Queen in `hive switch`

Queens appear in the fzf list like any other cell. No special treatment needed.

---

## Part 3: `HIVE_QUEEN_DIR` and Hook Environment

### The problem

Hooks run via `shell.RunInDir` which spawns a subprocess. These subprocesses don't inherit tmux session env vars. So even though `HIVE_CELL` and port vars are injected into tmux, hooks can't see them.

### Solution

Pass env vars to the hook runner so hooks can reference them. The most useful new var is `HIVE_QUEEN_DIR` — the path to the queen's working directory (the repo root), so hooks can copy or symlink files from the stable main checkout:

```yaml
hooks:
  - cp $HIVE_QUEEN_DIR/.env.local .env.local
  - ln -s $HIVE_QUEEN_DIR/node_modules node_modules
```

### Changes to hook runner

Add an `env` parameter to `Runner.Run`:

```go
func (r *Runner) Run(ctx context.Context, workDir string, hooks []string, env map[string]string) *Result
```

Update `internal/shell/exec.go` to support passing extra env vars. Add a new function:

```go
func RunInDirWithEnv(ctx context.Context, dir string, env map[string]string, name string, args ...string) (*RunResult, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if len(env) > 0 {
		cmd.Env = os.Environ()
		for k, v := range env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	result := &RunResult{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}

	if exitErr, ok := err.(*exec.ExitError); ok {
		result.ExitCode = exitErr.ExitCode()
		return result, nil
	}
	if err != nil {
		return nil, fmt.Errorf("executing %s: %w", name, err)
	}
	return result, nil
}
```

The hook runner uses `RunInDirWithEnv` instead of `RunInDir`:

```go
res, err := shell.RunInDirWithEnv(ctx, workDir, env, "sh", "-c", cmd)
```

### What gets passed to hooks

In `cmd/cell.go`, build the hook env from the same vars injected into tmux, plus `HIVE_QUEEN_DIR`:

```go
// Build hook env — same as tmux env, plus queen dir
hookEnv := make(map[string]string)
for k, v := range envVars {
	hookEnv[k] = v
}
queen, err := app.Repo.GetQueen(ctx, app.Project)
if err == nil && queen != nil {
	hookEnv["HIVE_QUEEN_DIR"] = queen.WorktreePath
}

// Run setup hooks with env
runner := hooks.NewRunner()
result := runner.Run(ctx, wtPath, app.Config.Hooks, hookEnv)
```

Also inject `HIVE_QUEEN_DIR` into the tmux session env vars so it's available in the terminal too:

```go
if queen != nil {
	envVars["HIVE_QUEEN_DIR"] = queen.WorktreePath
}
```

### Env vars available in hooks and tmux

| Variable | Source | Example |
|---|---|---|
| `HIVE_CELL` | Always injected | `my-feature` |
| `HIVE_QUEEN_DIR` | Queen's WorktreePath | `/Users/lothar/side_projects/hive` |
| `PORT`, `DB_PORT`, etc. | Port allocator | `3001`, `5433` |
| `NODE_ENV`, etc. | Static env from config | `development` |

---

## Part 4: Headless Cells

### What they are

A headless cell is a tmux session in any directory — no git worktree, no branch. Use cases:
- Quick scratch workspace for experimenting
- Running a tool or server in a non-git directory
- Monitoring scripts, log tailing, etc.

### CLI

```
hive cell --headless <name> [dir]
```

- `<name>` — cell name (required)
- `[dir]` — working directory (optional, defaults to current directory)
- `--branch` flag is rejected when `--headless` is used

### Creation flow

Headless cells skip worktree creation entirely:

```
1. Validate name doesn't exist              (existing)
2. Resolve working directory                 ← NEW (use [dir] or cwd)
3. Record in DB                              (existing, with Type=headless)
4. Build env vars                            (existing — static env + HIVE_CELL only, no ports)
5. Create tmux session with env vars         (existing)
6. Print summary                             (existing, simplified)
```

No hooks, no layout, no port allocation — headless cells are intentionally minimal.

### Data model

Headless cells are stored in the `cells` table with:
- `Type = "headless"`
- `Project = ""` (or the current project if inside a git repo — doesn't matter)
- `Branch = ""` (no branch)
- `WorktreePath = <dir>` (the working directory, used for tmux session start dir)
- `Ports = "{}"`

### Implementation in `cmd/cell.go`

Add `--headless` flag:

```go
var cellHeadless bool

func init() {
	cellCmd.Flags().StringVarP(&cellBranch, "branch", "b", "", "Git branch name (defaults to cell name)")
	cellCmd.Flags().BoolVar(&cellHeadless, "headless", false, "Create a tmux session without a git worktree")
	rootCmd.AddCommand(cellCmd)
}
```

Early in the `RunE` function, branch on headless vs normal:

```go
if cellHeadless {
	if cellBranch != "" {
		return fmt.Errorf("--headless and --branch cannot be used together")
	}
	return createHeadlessCell(ctx, cmd, name, args)
}
// ... existing normal cell creation code
```

The headless creation function:

```go
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
```

### Args change

The cell command currently uses `cobra.ExactArgs(1)`. With headless cells accepting an optional `[dir]`, change to:

```go
Args: cobra.RangeArgs(1, 2),
```

For normal (non-headless) cells, if a second arg is provided, return an error:

```go
if !cellHeadless && len(args) > 1 {
	return fmt.Errorf("unexpected argument %q — did you mean --headless?", args[1])
}
```

### Killing headless cells

`hive kill <name>` handles headless cells differently — no worktree to remove, no branch to delete:

```go
// In kill.go, after looking up the cell:
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
```

### Headless cells don't require a git repo

Normal `hive cell` requires `app.RepoDir != ""`. Headless cells skip this check — they work from any directory. The `not in a git repository` guard must move inside the normal cell path, not block the headless path.

---

## Part 5: CLI Output

### Normal cell creation (with queen auto-created)

```
$ hive cell my-feature
Queen session "hive-queen" created on branch "main"
Cell "my-feature" created
  Branch:   my-feature
  Worktree: ~/.hive/cells/hive/my-feature
  Tmux:     my-feature
  Ports:    3001 (PORT), 5433 (DB_PORT)
  Hooks:    3/3 passed
  Layout:   applied
```

### Second cell (queen already exists)

```
$ hive cell another-feature
Cell "another-feature" created
  Branch:   another-feature
  Worktree: ~/.hive/cells/hive/another-feature
  Tmux:     another-feature
  Ports:    3003 (PORT), 5435 (DB_PORT)
  Hooks:    3/3 passed
  Layout:   applied
```

### Queen branch guard failure

```
$ hive cell my-feature
Error: queen "hive-queen" is on branch "some-other-branch" but should be on "main" — switch it back before using Hive
```

### Headless cell creation

```
$ hive cell --headless scratch /tmp/experiments
Headless cell "scratch" created
  Dir:   /tmp/experiments
  Tmux:  scratch
```

### Headless cell in current directory

```
$ hive cell --headless notes
Headless cell "notes" created
  Dir:   /Users/lothar/Documents/notes
  Tmux:  notes
```

### Headless/branch conflict

```
$ hive cell --headless --branch feat scratch
Error: --headless and --branch cannot be used together
```

### Status with all cell types

```
$ hive status
NAME                 PROJECT  BRANCH       STATUS   TMUX   PORTS       AGE
hive-queen [queen]   hive     main         stopped  alive  -           2h
my-feature           hive     my-feature   stopped  alive  3001,5433   1h
scratch [headless]   -        -            stopped  alive  -           10m
```

### Killing a headless cell

```
$ hive kill scratch
Headless cell "scratch" killed
```

### Killing a queen

```
$ hive kill hive-queen
Queen "hive-queen" killed
```

---

## Part 6: `hive status` Display Changes

Update the NAME column to append type markers:

```go
nameDisplay := c.Name
switch c.Type {
case state.TypeQueen:
	nameDisplay += " [queen]"
case state.TypeHeadless:
	nameDisplay += " [headless]"
}
```

For headless cells with no project or branch, display `-`:

```go
project := c.Project
if project == "" {
	project = "-"
}
branch := c.Branch
if branch == "" {
	branch = "-"
}
```

---

## Implementation Order

### Step 1: Data model — cell type
- Add `CellType` constants to `internal/state/models.go`
- Add `Type` field to `Cell` struct
- Add `runMigrations()` in `internal/state/db.go` — `ALTER TABLE cells ADD COLUMN type`
- Update `scanCell` / `scanCells` to include `type`
- Update all SELECT queries in `repo.go` to include `type`
- Update `Create` to write `type`
- Add `GetQueen(ctx, project)` method
- Update existing tests to set `Type: state.TypeNormal`

### Step 2: Queen sessions
- Add `createQueen()` function in `cmd/cell.go`
- Add `queenCurrentBranch()` helper in `cmd/root.go`
- Wire queen auto-creation into cell creation flow (check + create before normal cell)
- Queen creation failure is a warning, not fatal
- Default branch from `app.RepoRecord.DefaultBranch` or `"main"`
- Add queen branch guard in `PersistentPreRunE` (skip for commands that don't need it)

### Step 3: Hook environment and `HIVE_QUEEN_DIR`
- Add `shell.RunInDirWithEnv()` to `internal/shell/exec.go`
- Update `hooks.Runner.Run()` to accept `env map[string]string` and use `RunInDirWithEnv`
- In `cmd/cell.go`, build hook env from tmux env vars + `HIVE_QUEEN_DIR`
- Inject `HIVE_QUEEN_DIR` into tmux session env vars too

### Step 4: Update `hive kill` for queen and headless
- Check `cell.Type` before attempting worktree/branch removal
- Queen: kill tmux + delete DB record only (never touch repo dir or branch)
- Headless: kill tmux + delete DB record only (no worktree to remove)

### Step 5: Headless cells
- Add `--headless` flag to `cellCmd`
- Change `Args` to `cobra.RangeArgs(1, 2)`
- Add `createHeadlessCell()` function in `cmd/cell.go`
- Move the `app.RepoDir == ""` check inside the normal path
- Validate `--headless` + `--branch` conflict
- Validate extra arg without `--headless`

### Step 6: Update `hive status`
- Append `[queen]` / `[headless]` to name column
- Show `-` for empty project/branch fields

### Step 7: Tests
- State layer: create cells with different types, verify `GetQueen`, verify type persists through create/scan cycle
- Migration: open DB twice (simulates upgrade), verify `type` column exists and defaults to `"normal"`
- Queen branch guard: verify error when queen is on wrong branch
- Headless creation: verify no worktree created, DB record has correct type/path
- Kill: verify queen/headless kill skips worktree removal
