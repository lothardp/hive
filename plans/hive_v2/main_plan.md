# Hive v2 — Implementation Plan

Reference: [hive_v2.md](/hive_v2.md)

This plan is ordered so that each phase produces something testable. Later phases build on earlier ones but earlier phases work standalone.

---

## Phase 1: Config System

Replace DB-based config with file-based config. This is the foundation — everything else reads from these files.

### 1.1 Rewrite `internal/config/config.go`

Two new structs, two new loaders:

```go
// GlobalConfig loaded from ~/.hive/config.yaml
type GlobalConfig struct {
    ProjectDirs []string `yaml:"project_dirs"`
    CellsDir    string   `yaml:"cells_dir"`
    Editor      string   `yaml:"editor"`
    TmuxLeader  string   `yaml:"tmux_leader"`
}

// ProjectConfig loaded from ~/.hive/config/{project}.yml
type ProjectConfig struct {
    RepoPath string            `yaml:"repo_path"`
    Hooks    []string          `yaml:"hooks"`
    Env      map[string]string `yaml:"env"`
    PortVars []string          `yaml:"port_vars"`
    Layouts  map[string]Layout `yaml:"layouts"`
}
```

Functions needed:
- `LoadGlobal(hiveDir string) (*GlobalConfig, error)` — read `~/.hive/config.yaml`
- `LoadGlobalOrDefault(hiveDir string) *GlobalConfig` — with sensible defaults
- `LoadProject(hiveDir, project string) (*ProjectConfig, error)` — read `~/.hive/config/{project}.yml`
- `LoadProjectOrDefault(hiveDir, project string) *ProjectConfig`
- `WriteDefaultGlobal(hiveDir string, cfg *GlobalConfig) error` — write initial config.yaml
- `WriteDefaultProject(hiveDir, project string, cfg *ProjectConfig) error` — write initial {project}.yml
- `GlobalConfig.ResolveProjectDirs() []string` — expand `~` in each entry
- `GlobalConfig.ResolveCellsDir() string` — expand `~`
- `GlobalConfig.ResolveEditor() string` — fall back to `$EDITOR`, then `vim`

Keep `Layout`, `Window`, `Pane` structs as-is — they're fine.

### 1.2 Delete `internal/config/merge.go` and its test

No longer needed.

### 1.3 Add project discovery to config package

```go
// DiscoverProjects scans project_dirs one level deep for git repos.
// Returns a list of {Name, Path} for each discovered project.
type DiscoveredProject struct {
    Name string // directory basename
    Path string // absolute path to the repo
}

func DiscoverProjects(projectDirs []string) ([]DiscoveredProject, error)
```

Logic: for each dir in `projectDirs`, list immediate subdirectories, check if each has a `.git` dir/file, return those that do. Sort alphabetically.

### 1.4 Tests

- Test `LoadGlobal` / `LoadProject` with fixture YAML files
- Test `DiscoverProjects` with a temp directory containing some git repos and some non-git dirs
- Test `~` expansion in `ResolveProjectDirs`

**Checkpoint**: Config loads from files. Project discovery works. Nothing else depends on this yet.

---

## Phase 2: Simplify State Layer

Trim the DB to only what v2 needs: cells and notifications.

### 2.1 Simplify `internal/state/db.go`

Remove `global_config` and `repos` tables from the schema. Update `runMigrations` if needed. The schema becomes:

```sql
CREATE TABLE IF NOT EXISTS cells (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    name          TEXT UNIQUE NOT NULL,
    project       TEXT NOT NULL,
    clone_path    TEXT NOT NULL,
    status        TEXT NOT NULL DEFAULT 'running',
    ports         TEXT NOT NULL DEFAULT '{}',
    type          TEXT NOT NULL DEFAULT 'normal',
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS notifications (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    cell_name  TEXT NOT NULL,
    title      TEXT NOT NULL DEFAULT '',
    message    TEXT NOT NULL,
    details    TEXT NOT NULL DEFAULT '',
    read       BOOLEAN NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (cell_name) REFERENCES cells(name) ON DELETE CASCADE
);
```

Key change: `worktree_path` → `clone_path`, `branch` column dropped.

### 2.2 Update `internal/state/models.go`

- Remove `Repo` struct
- Remove `branch` field from `Cell`
- Rename `WorktreePath` → `ClonePath`
- Drop `StatusProvisioning` — cells are either `running`, `stopped`, or `error`
- Keep `TypeNormal`, `TypeHeadless`. Replace `TypeQueen` with a constant for the dashboard session name (not stored in DB).

### 2.3 Update `internal/state/repo.go` (CellRepository)

Update all queries to match new schema (no `branch` column, `clone_path` instead of `worktree_path`).

### 2.4 Delete `internal/state/config_repo.go`, `repo_repo.go`, and their tests

These backed the `global_config` and `repos` tables which are gone.

### 2.5 Keep `internal/state/notification_repo.go` as-is

No changes needed.

### 2.6 Update tests

Fix `repo_test.go` for the schema changes.

**Checkpoint**: DB is leaner. Config comes from files, state comes from DB. Clean separation.

---

## Phase 3: Replace Worktree with Clone

Replace the worktree manager with a simple clone manager.

### 3.1 Delete `internal/worktree/` entirely

Remove `worktree.go` and `worktree_test.go`.

### 3.2 Create `internal/clone/clone.go`

```go
type Manager struct {
    CellsDir string // e.g. ~/hive/cells
}

func NewManager(cellsDir string) *Manager

// Clone runs `git clone <repoPath> <cellsDir>/<project>/<name>`.
// Returns the absolute path to the clone.
func (m *Manager) Clone(ctx context.Context, repoPath, project, name string) (string, error)

// Remove deletes the clone directory (rm -rf).
func (m *Manager) Remove(clonePath string) error

// Exists checks if the clone directory exists.
func (m *Manager) Exists(clonePath string) bool
```

`Clone` does:
1. Build target path: `filepath.Join(m.CellsDir, project, name)`
2. Verify target doesn't exist
3. `os.MkdirAll` parent dir
4. `shell.Run(ctx, "git", "clone", repoPath, targetPath)`
5. Return target path

`Remove` does:
1. Verify path is under `m.CellsDir` (safety check — never rm -rf outside cells dir)
2. `os.RemoveAll(clonePath)`

### 3.3 Tests

- Clone into a temp dir, verify `.git` exists in the clone
- Remove, verify dir is gone
- Safety check: refuse to remove a path that's not under CellsDir

**Checkpoint**: Can clone repos and clean them up. No worktree dependencies remain.

---

## Phase 4: Rewrite `cmd/root.go`

Slim down the App struct and PersistentPreRunE for v2.

### 4.1 New `App` struct

```go
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
```

Gone: `ConfigRepo`, `RepoRepo`, `RepoRecord`, `RepoDir`, `Project`, `WtMgr`. The app no longer needs to detect what repo you're in — it's dashboard-driven.

### 4.2 Simplified `PersistentPreRunE`

1. Resolve `~/.hive` dir
2. Set up file logging
3. Open SQLite
4. Init `CellRepo`, `NotifRepo`
5. Load global config from `~/.hive/config.yaml`
6. Init `CloneMgr` with `config.CellsDir`
7. Init `TmuxMgr`

No more: git repo detection, queen branch integrity check, config-from-DB loading, HIVE_QUEEN_DIR env var handling.

### 4.3 Remove `queenCurrentBranch` helper

No longer needed.

**Checkpoint**: App boots cleanly with the new config + state setup. Existing commands will be broken — that's fine, we're about to delete most of them.

---

## Phase 5: Gut the CLI

Delete all commands that are now dashboard-only. Keep the shell.

### 5.1 Delete these files

- `cmd/cell.go`
- `cmd/setup.go`
- `cmd/config.go`
- `cmd/join.go`
- `cmd/switch.go`
- `cmd/status.go`
- `cmd/kill.go`
- `cmd/keybindings.go`
- `cmd/notifications.go`
- `cmd/completion.go`
- `cmd/peek.go`
- `cmd/jump.go`
- `cmd/swarm.go`
- `cmd/up.go`
- `cmd/down.go`
- `cmd/stop.go`
- `cmd/initproxy.go`

### 5.2 Keep and update these files

- `cmd/root.go` — updated in Phase 4
- `cmd/dashboard.go` — will be rewritten in Phase 7
- `cmd/notify.go` — keep as-is (minor update: remove references to `app.Repo`, use `app.CellRepo`)
- `cmd/install.go` — rewrite in Phase 6
- `cmd/logs.go` — keep as convenience command, no changes needed
- `cmd/start.go` — create in Phase 6

### 5.3 Delete unused internal packages

- `internal/worktree/` (replaced in Phase 3)
- `internal/config/merge.go` + `merge_test.go` (deleted in Phase 1)
- `internal/keybindings/` — will be simplified in Phase 6

**Checkpoint**: `go build` succeeds. Only `hive start`, `hive install`, `hive dashboard`, `hive notify`, and `hive logs` exist. The dashboard doesn't work yet — just a placeholder.

---

## Phase 6: `hive install` + `hive start`

The two CLI entry points.

### 6.1 Rewrite `cmd/install.go`

New flow:
1. Create `~/.hive/` if missing
2. Create `~/.hive/config/` if missing
3. Prompt for project directories (comma or newline separated)
4. Prompt for cells directory (default `~/hive/cells`)
5. Prompt for editor (default `$EDITOR` or `vim`)
6. Write `~/.hive/config.yaml` with the answers
7. Create cells directory if missing
8. Generate `~/.hive/tmux.conf` — just the dashboard keybind (`bind-key h switch-client -t hive`)
9. Print instructions to source tmux.conf

### 6.2 Simplify `internal/keybindings/`

Strip it down to a single function:

```go
func GenerateTmuxConf(leader string) string
```

Output is just:
```tmux
# Hive — switch to dashboard
bind-key -T prefix h switch-client -t hive
```

No more table mode, popup commands, version checks, or multiple bindings. Delete the test and write a new minimal one.

### 6.3 Create `cmd/start.go`

The entry point. Logic:

```go
func startRun(cmd *cobra.Command, args []string) error {
    hiveBin, _ := os.Executable() // path to the hive binary

    // Check if hive tmux session exists
    exists, _ := app.TmuxMgr.SessionExists(ctx, "hive")

    if !exists {
        // Create the session, running `hive dashboard` as its command
        // tmux new-session -d -s hive -c ~ "hive dashboard"
        home, _ := os.UserHomeDir()
        shell.Run(ctx, "tmux", "new-session", "-d", "-s", "hive", "-c", home, hiveBin, "dashboard")
    }

    // Attach or switch
    return app.TmuxMgr.JoinSession("hive")
}
```

Key detail: the tmux session runs `hive dashboard` as its process. If the dashboard exits (q), the session dies. `hive start` recreates it.

### 6.4 Tests

- `hive install` writes correct config.yaml to a temp dir
- `hive start` logic: session exists → join; session missing → create + join

**Checkpoint**: `hive install` sets up the machine. `hive start` launches tmux with the dashboard. The dashboard itself is still a placeholder.

---

## Phase 7: Cell Service

Extract cell creation/killing logic into a reusable internal package. The dashboard will call this, not cmd/ code.

### 7.1 Create `internal/cell/service.go`

```go
type Service struct {
    CellRepo  *state.CellRepository
    NotifRepo *state.NotificationRepository
    CloneMgr  *clone.Manager
    TmuxMgr   *tmux.Manager
    HiveDir   string
    DB        *sql.DB
}

type CreateOpts struct {
    Project  string // e.g. "my-api"
    Name     string // e.g. "work"
    RepoPath string // e.g. ~/side_projects/my-api (from discovery)
}

type CreateResult struct {
    CellName  string // e.g. "my-api-work"
    ClonePath string
    Ports     map[string]int
    HookLog   string // summary of hook execution
}
```

Methods:

**`Create(ctx, opts, projectCfg) (*CreateResult, error)`**
1. Build cell name: `project + "-" + name`
2. Check cell doesn't already exist in DB
3. `CloneMgr.Clone(ctx, opts.RepoPath, opts.Project, opts.Name)`
4. Allocate ports (from `projectCfg.PortVars`)
5. Build env vars (ports + `projectCfg.Env` + `HIVE_CELL`)
6. Create tmux session
7. Run hooks in clone dir (from `projectCfg.Hooks`)
8. Apply layout (from `projectCfg.Layouts["default"]`)
9. Insert cell record in DB
10. On failure at any step: rollback previous steps

**`Kill(ctx, cellName string) error`**
1. Look up cell in DB
2. Kill tmux session
3. If normal cell: `CloneMgr.Remove(cell.ClonePath)`
4. Delete DB record

**`CreateHeadless(ctx, name string) error`**
1. Check name doesn't exist
2. Create tmux session in `~` with `HIVE_CELL` env var
3. Insert cell record as headless type

**`List(ctx) ([]state.Cell, error)`** — wrapper around repo

### 7.2 Tests

- Create cell with mocked clone/tmux (or test against real git clone in temp dir)
- Kill cell verifies cleanup order
- Rollback on failure: if hooks fail, clone dir is removed

**Checkpoint**: All cell lifecycle logic lives in one package. The dashboard can call `service.Create()` without knowing the implementation details.

---

## Phase 8: Dashboard Rewrite

The big one. Rewrite `internal/tui/dashboard.go` as a multi-tab TUI.

### 8.1 Architecture

Split the TUI into composable models:

```
internal/tui/
├── dashboard.go    # Root model: tab switching, global keybinds
├── cells.go        # Cells tab: list, navigate, kill
├── projects.go     # Projects tab: list, edit config
├── config.go       # Config tab: show global config, edit
├── create.go       # Create flow: project picker → name input → progress
└── styles.go       # Shared lipgloss styles
```

### 8.2 Root model (`dashboard.go`)

```go
type Model struct {
    activeTab  int // 0=cells, 1=projects, 2=config
    tabs       []string
    cellsTab   CellsModel
    projectsTab ProjectsModel
    configTab  ConfigModel

    // Create flow overlay (shown over current tab)
    creating   *CreateModel // nil when not active

    // Dependencies
    cellService *cell.Service
    globalCfg   *config.GlobalConfig
    hiveDir     string

    width, height int
    message       string
}
```

Tab switching via `Tab` key. Each tab is its own Bubble Tea model with `Update()` and `View()`.

The create flow is a modal overlay — when active, it captures all input until complete or cancelled.

### 8.3 Cells tab (`cells.go`)

Port the existing tree view from v1. Changes:
- Remove queen cell type handling (no more queens in cell list)
- Remove branch column (cells don't track branches)
- Keep: project grouping, tmux alive indicator, age, ports, unread notifications, unmanaged tmux sessions
- Actions: Enter (switch), x (kill with confirm), n (mark read), r (refresh)

On Enter (switch): set `SwitchTarget` on the root model, quit the TUI. The cmd layer reads `SwitchTarget` and calls `tmux switch-client`. The tmux session keeps running `hive dashboard` — it just loses focus.

Wait — that's v1's approach. In v2, the dashboard IS the tmux session process. We can't quit the TUI to switch. Instead:

**Navigate approach**: The dashboard uses `shell.Run(ctx, "tmux", "switch-client", "-t", cellName)` without quitting. The dashboard keeps running in its tmux session; the user's terminal switches to the cell's session. When they press `<leader>+h`, tmux switches them back to the dashboard session. The TUI never exits.

This means `SwitchTarget` and the quit-to-switch pattern goes away. Navigation is a fire-and-forget tmux command from within the TUI.

### 8.4 Projects tab (`projects.go`)

New view. Logic:
- On load: `config.DiscoverProjects(globalCfg.ProjectDirs)` to get the list
- For each project, check if `~/.hive/config/{project}.yml` exists → show checkmark
- On Enter/e: suspend the TUI (`tea.ExecProcess`), open `$EDITOR ~/.hive/config/{project}.yml`, resume. If file doesn't exist, write a default template first.
- After editor closes: reload config

### 8.5 Config tab (`config.go`)

Shows the contents of `~/.hive/config.yaml` as plain text.
- On `e`: suspend TUI, open `$EDITOR ~/.hive/config.yaml`, resume, reload config.

### 8.6 Create flow (`create.go`)

Modal overlay, triggered by `c` from cells tab:

**Step 1 — Project picker**: Filterable list of discovered projects. Arrow keys to navigate, type to filter, Enter to select, Esc to cancel.

**Step 2 — Name input**: Text input. Enter to confirm, Esc to cancel.

**Step 3 — Progress**: Show "Cloning...", "Running hooks...", "Creating session..." as each step completes. This runs as a `tea.Cmd` that calls `cellService.Create()`. On success: auto-navigate to the new cell. On failure: show error, allow retry or cancel.

### 8.7 Kill flow

From cells tab, `x` on a cell:
1. Show confirm prompt (inline, not a modal)
2. On `y`: run `cellService.Kill()` as a `tea.Cmd`
3. Refresh cell list on completion

Refuse to kill headless cells? Or just kill them (no clone to remove, just tmux session + DB record). Probably just kill them — they're cheap.

### 8.8 Notification display

In the cells tab, show unread count per cell (already exists in v1). Press `n` to mark read.

Could add: a notification detail panel or popup showing recent notifications for the selected cell. This is a nice-to-have — the basic unread count is sufficient for now.

### 8.9 Tests

TUI testing is hard to unit test. Focus on:
- `CreateModel` step transitions (unit test the state machine, not the rendering)
- Cell list grouping logic (extract into a helper, test that)
- Project discovery → view data transformation

**Checkpoint**: Full dashboard works. All three tabs functional. Create, navigate, and kill flows work end-to-end.

---

## Phase 9: Update `hive notify`

### 9.1 Update `cmd/notify.go`

Minimal changes:
- Replace `app.Repo` references with `app.CellRepo`
- Remove any references to `app.RepoRepo` or `app.ConfigRepo`
- The command still reads `HIVE_CELL` from env and inserts a notification into SQLite

### 9.2 Verify

Run `hive notify -m "test"` from inside a cell's tmux session. Check that the dashboard shows the unread count.

**Checkpoint**: Notifications flow from cells to dashboard.

---

## Phase 10: Update `hive install` for tmux.conf

### 10.1 Final tmux.conf

The generated `~/.hive/tmux.conf` should be minimal:

```tmux
# Hive dashboard keybinding
bind-key h switch-client -t hive
```

Just one binding. The leader key from config applies if the user has `set-option -g prefix` in their own tmux.conf — Hive doesn't override it.

### 10.2 Update install instructions

Print:
```
Add this to your ~/.tmux.conf:
  source-file ~/.hive/tmux.conf
```

**Checkpoint**: Full loop works. Install → start → create cell → navigate → come back → kill.

---

## Phase 11: Cleanup

### 11.1 Delete dead code

- `internal/keybindings/keybindings_test.go` (if not rewritten)
- Any remaining references to worktree, queen, branch in the codebase
- Old plan file `plans/hive_start_command.md` (superseded by this plan)

### 11.2 Update `CLAUDE.md`

Rewrite to reflect v2 architecture:
- New project layout
- New command list
- New config system
- Updated patterns and conventions

### 11.3 Update `README.md`

Match CLAUDE.md updates. New quick start guide:
```
hive install    # one-time setup
hive start      # launch the dashboard
```

### 11.4 Run full test suite

```bash
go test ./...
go build -o hive .
```

Fix any broken tests from the migration.

**Checkpoint**: Clean codebase, docs match reality, all tests pass.

---

## Implementation Order Summary

| Phase | What | Depends on | Risk |
|-------|------|------------|------|
| 1 | Config system | Nothing | Low — new code, no breakage |
| 2 | Simplify state | Nothing | Low — remove code |
| 3 | Clone manager | Nothing | Low — simple wrapper |
| 4 | Rewrite root.go | 1, 2, 3 | Medium — everything flows through here |
| 5 | Delete old commands | 4 | Low — just deleting files |
| 6 | install + start | 4 | Low — small commands |
| 7 | Cell service | 2, 3 | Medium — core business logic |
| 8 | Dashboard rewrite | 1, 7 | High — most complex, most new code |
| 9 | Update notify | 2, 4 | Low — minor edits |
| 10 | Tmux.conf | 6 | Low — trivial |
| 11 | Cleanup + docs | All | Low — polish |

Phases 1, 2, 3 can be done in parallel. Phase 4 ties them together. Phases 6 and 7 can be done in parallel after 4. Phase 8 is the critical path — start it as soon as Phase 7 is done.
