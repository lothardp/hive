# Multicells

## Overview

A **multicell** is a single named environment that bundles clones of **multiple projects** under one parent directory, plus one tmux session rooted at that parent. Instead of spinning up several independent cells (one per repo) and jumping between them, the user creates multicell `foo`, picks e.g. `api`, `web`, `mobile`, and Hive produces:

```
~/hive/multicells/foo/
├── api-foo/     ← clone of api
├── web-foo/     ← clone of web
└── mobile-foo/  ← clone of mobile
```

Each child clone's directory is suffixed with the multicell name. Two reasons:
globally-unique directory names across all multicells (so tab completion and
shell history stay unambiguous), and tools that derive identifiers from the
working directory basename (notably `docker compose`, which uses the cwd as
the default project name for network/volume prefixes) don't collide with
clones of the same repo in other multicells or in regular cells.

Plus a tmux session named `foo` with its working directory set to `~/hive/multicells/foo`. That tmux session is the "father" — the user can `cd api-foo/`, `cd web-foo/`, etc. from any pane.

Multicells are a distinct cell type (`multi`) living in the existing `cells` table, with their child clones tracked in a new `multicell_children` join table.

### Why it matters

Some features span multiple repos (e.g. `server + web + mobile`). Right now a user creating an `auth-overhaul` spike has to create three separate cells in three separate project dirs, each with its own tmux session, and context-switch between them. A multicell captures the feature as one logical workspace.

### In scope for v1

- Per-project setup hooks run inside each child clone, using each source
  project's configured hooks.

### Out of scope for v1

- Per-project port allocation and env injection (child clones get no ports;
  one shared tmux env only). Deferred because sequential clone-provisioning
  would race on port allocation — needs a small allocator change.

Doable later; this plan intentionally keeps v1 minimal so the concept proves
itself before we glue on every feature normal cells have.

---

## Part 1: Data Model

### New cell type

Add `TypeMulti` to `internal/state/models.go`:

```go
type CellType string

const (
    TypeNormal   CellType = "normal"
    TypeHeadless CellType = "headless"
    TypeMulti    CellType = "multi"    // NEW
)
```

The multicell itself is stored as a row in the existing `cells` table:

```go
&state.Cell{
    Name:      "foo",                       // bare name, not project-prefixed
    Project:   "",                          // multicells aren't tied to one project
    ClonePath: "/Users/me/hive/multicells/foo", // the parent dir
    Status:    state.StatusRunning,
    Ports:     "{}",
    Type:      state.TypeMulti,
}
```

No changes to the `cells` schema — all existing columns serve.

### New `multicell_children` table

Child clones inside a multicell need their own record so we know:
- Which projects are part of the multicell
- Where each clone lives (for kill/cleanup)
- Source repo path (for rollback or rebuild)

```sql
CREATE TABLE IF NOT EXISTS multicell_children (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    multicell_name  TEXT NOT NULL,
    project         TEXT NOT NULL,
    clone_path      TEXT NOT NULL,
    source_repo     TEXT NOT NULL,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(multicell_name, project),
    FOREIGN KEY (multicell_name) REFERENCES cells(name) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_mcc_multicell ON multicell_children(multicell_name);
```

Goes in `internal/state/db.go`'s `schema` constant (new `CREATE TABLE`) alongside `cells` and `notifications`.

`ON DELETE CASCADE` means deleting the `cells` row automatically clears child rows. Requires `foreign_keys(1)` pragma — already enabled in `Open()` at `internal/state/db.go:39`.

### Go types

In `internal/state/models.go`:

```go
type MulticellChild struct {
    ID             int64
    MulticellName  string
    Project        string
    ClonePath      string
    SourceRepo     string
    CreatedAt      time.Time
}
```

### Repository

New file `internal/state/multicell_repo.go`:

```go
package state

import (
    "context"
    "database/sql"
    "fmt"
    "time"
)

type MulticellRepository struct {
    db *sql.DB
}

func NewMulticellRepository(db *sql.DB) *MulticellRepository {
    return &MulticellRepository{db: db}
}

func (r *MulticellRepository) AddChild(ctx context.Context, mc *MulticellChild) error
func (r *MulticellRepository) ListChildren(ctx context.Context, multicellName string) ([]MulticellChild, error)
func (r *MulticellRepository) DeleteByMulticell(ctx context.Context, multicellName string) error
```

`DeleteByMulticell` is a safety net — cascade should handle it, but call it explicitly in the kill path so kill order doesn't depend on foreign-key timing.

Wire it into `App` in `cmd/root.go`:

```go
type App struct {
    // ... existing fields ...
    MulticellRepo *state.MulticellRepository  // NEW
}

// In PersistentPreRunE:
app.MulticellRepo = state.NewMulticellRepository(db)
```

---

## Part 2: Global Config

### New `multicells_dir` setting

Where to put the parent dirs. Lives in `~/.hive/config.yaml` alongside `cells_dir`:

```yaml
project_dirs:
  - ~/side_projects
  - ~/work
cells_dir: ~/hive/cells
multicells_dir: ~/hive/multicells   # NEW
editor: vim
tmux_leader: "C-a"
```

Update `internal/config/config.go`:

```go
type GlobalConfig struct {
    ProjectDirs    []string `yaml:"project_dirs"`
    CellsDir       string   `yaml:"cells_dir"`
    MulticellsDir  string   `yaml:"multicells_dir"`   // NEW
    Editor         string   `yaml:"editor"`
    TmuxLeader     string   `yaml:"tmux_leader"`
}

// Default in LoadGlobalOrDefault:
cfg = &GlobalConfig{
    CellsDir:       "~/hive/cells",
    MulticellsDir:  "~/hive/multicells",
    Editor:         "vim",
    TmuxLeader:     "C-a",
}

// New resolver method:
func (g *GlobalConfig) ResolveMulticellsDir() string {
    if g.MulticellsDir == "" {
        return expandTilde("~/hive/multicells")
    }
    return expandTilde(g.MulticellsDir)
}
```

### `hive install` wiring

`cmd/install.go` already writes a default global config. Make sure `MulticellsDir` is populated with `~/hive/multicells` in the generated file. (One-line addition to the default.)

---

## Part 3: Multicell Service

All multicell lifecycle logic lives in `internal/cell/service.go` alongside normal/headless logic. No new package — the service already owns cell creation, so keeping multicells there avoids duplicating `CloneMgr`/`TmuxMgr`/`CellRepo` plumbing. Creation reuses the existing `provisionClone` helper (one call per child), so per-project hooks, env, and clone rollback come for free.

### Struct extension

The existing `Service` gets one more dependency:

```go
type Service struct {
    CellRepo      *state.CellRepository
    MulticellRepo *state.MulticellRepository   // NEW
    CloneMgr      *clone.Manager
    TmuxMgr       *tmux.Manager
    HiveDir       string
    MulticellsDir string                        // NEW — resolved parent dir
    DB            *sql.DB
}
```

`MulticellsDir` is set from `GlobalConfig.ResolveMulticellsDir()` in `cmd/root.go`'s `PersistentPreRunE` (same place `CloneMgr` is built).

### Opts and Result

```go
// MultiOpts holds the parameters for creating a multicell.
type MultiOpts struct {
    Name     string                             // e.g. "auth-overhaul"
    Projects []config.DiscoveredProject         // selected projects with Name + Path
}

// MultiResult holds the outcome of a successful multicell creation.
type MultiResult struct {
    Name       string            // "auth-overhaul"
    ParentDir  string            // "/Users/me/hive/multicells/auth-overhaul"
    Projects   []string          // ["api", "web", "mobile"]
    ClonePaths []string          // absolute paths in the same order as Projects
    HookLogs   map[string]string // project name → hook log summary
}
```

### `CreateMulti`

```go
// CreateMulti provisions a multicell:
//   parent dir + N provisioned clones (with hooks) + tmux session + DB rows.
// On failure at any step, everything created so far is rolled back.
func (s *Service) CreateMulti(ctx context.Context, opts MultiOpts) (*MultiResult, error)
```

Behavior:

1. **Validate name uniqueness.** Call `s.CellRepo.GetByName(ctx, opts.Name)`. If a cell (normal, headless, or multi) with that name already exists, return `fmt.Errorf("cell %q already exists", opts.Name)`. The multicell name lives in the same namespace as every other cell because tmux session names are global.

2. **Validate at least one project.** `len(opts.Projects) == 0` → error `"multicell requires at least one project"`.

3. **Validate no duplicate project names.** Defensive — `config.DiscoverProjects` already dedupes upstream, but double-check.

4. **Compute parent dir.** `parentDir := filepath.Join(s.MulticellsDir, opts.Name)`.

5. **Orphan cleanup + mkdir.** `os.RemoveAll(parentDir)` (no-op if missing) then `os.MkdirAll(parentDir, 0o755)`.

6. **Provision each project sequentially.** For each `p := range opts.Projects`,
   the child dir name is `<project>-<multicell>`:

   ```go
   childDirName := p.Name + "-" + opts.Name
   prov, err := s.provisionClone(ctx, ProvisionOpts{
       Project:    p.Name,
       SourceRepo: p.Path,
       TargetPath: filepath.Join(parentDir, childDirName),
       CellName:   opts.Name,        // HIVE_CELL = multicell name, shared across projects
       AllocPorts: false,             // v1: no per-project ports
       ExtraEnv: map[string]string{
           "HIVE_MULTICELL":     opts.Name,
           "HIVE_MULTICELL_DIR": parentDir,
           "HIVE_PROJECT":       p.Name,
       },
   })
   if err != nil {
       os.RemoveAll(parentDir)  // full rollback of any prior successful projects
       return nil, fmt.Errorf("provisioning %q: %w", p.Name, err)
   }
   ```

   Hook failures inside any project abort the whole multicell build (the error
   propagates up, the single clone dir is already rolled back, and the outer
   `os.RemoveAll(parentDir)` wipes any earlier siblings).

   Collect each `prov.HookLog` keyed by project name for the result.

7. **Kill any orphaned tmux session** named `opts.Name`: `_ = s.TmuxMgr.KillSession(ctx, opts.Name)`.

8. **Create tmux session** rooted at `parentDir`:
   ```go
   envVars := map[string]string{
       "HIVE_CELL":          opts.Name,
       "HIVE_MULTICELL":     opts.Name,
       "HIVE_MULTICELL_DIR": parentDir,
   }
   s.TmuxMgr.CreateSession(ctx, opts.Name, parentDir, envVars)
   ```
   On failure: `os.RemoveAll(parentDir)` then return error.

9. **Insert `cells` row** with `Type = TypeMulti`:
   ```go
   cell := &state.Cell{
       Name:      opts.Name,
       Project:   "",
       ClonePath: parentDir,
       Status:    state.StatusRunning,
       Ports:     "{}",
       Type:      state.TypeMulti,
   }
   s.CellRepo.Create(ctx, cell)
   ```
   On failure: kill tmux + remove parentDir, return error.

10. **Insert one `multicell_children` row per project.** If any insert fails, run the full rollback (DB cell row + tmux + parentDir). The foreign key means the child inserts must come after the parent `cells` insert.

11. **Return `MultiResult`** with all the paths and hook logs.

### `KillMulti`

Extend `Service.Kill` to handle `TypeMulti`, rather than a separate entry point. Current `Kill` at `internal/cell/service.go:183` already branches on `cell.Type != state.TypeHeadless` for clone removal; add a branch for `TypeMulti`:

```go
// After tmux kill, before clone removal:
if cell.Type == state.TypeMulti {
    // Remove the entire parent dir (which contains all child clones).
    if cell.ClonePath != "" {
        if err := s.removeMulticellDir(cell.ClonePath); err != nil {
            slog.Warn("failed to remove multicell dir", "path", cell.ClonePath, "error", err)
        }
    }
    // Child rows are removed via ON DELETE CASCADE when we delete the cell row,
    // but call explicitly for safety:
    _ = s.MulticellRepo.DeleteByMulticell(ctx, cellName)
} else if cell.Type != state.TypeHeadless && cell.ClonePath != "" {
    // Existing: remove single clone for normal cells.
    if err := s.CloneMgr.Remove(cell.ClonePath); err != nil {
        slog.Warn("failed to remove clone directory", "path", cell.ClonePath, "error", err)
    }
}
```

`removeMulticellDir` is a small helper that refuses to delete anything outside `MulticellsDir` (same safety pattern as `CloneMgr.Remove` at `internal/clone/clone.go:81`):

```go
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
```

### Listing child clones

For dashboard rendering ("which projects does this multicell hold?") expose a helper:

```go
func (s *Service) ListMultiChildren(ctx context.Context, multicellName string) ([]state.MulticellChild, error) {
    return s.MulticellRepo.ListChildren(ctx, multicellName)
}
```

---

## Part 4: TUI Flow

Creation happens in the dashboard, matching the existing "TUI-first" pattern (there is no `hive cell` CLI command — see `cmd/` layout; cell creation lives in `internal/tui/create.go`).

### New keybinding

On the Cells tab, add `C` (uppercase — lowercase `c` is taken by single-cell create) for "new multicell". Register in `internal/tui/dashboard.go`:

```go
type dashKeyMap struct {
    // ... existing ...
    Multicell key.Binding    // NEW
}

var dashKeys = dashKeyMap{
    // ... existing ...
    Multicell: key.NewBinding(key.WithKeys("C")),
}
```

Wire into the switch block in `Update()` alongside `dashKeys.Create`:

```go
case m.activeTab == tabCells && key.Matches(msg, dashKeys.Multicell):
    return m.startCreateMulti()
```

Update the cells footer help line at `internal/tui/cells.go:338`:

```go
helpStyle.Render("enter switch  c create  C multicell  o open  H headless  x kill  n read notifs  r refresh  h/l tabs  q quit")
```

### New overlay model

New file `internal/tui/create_multi.go` (sibling to `create.go`):

```go
type multiStep int

const (
    multiStepPickProjects multiStep = iota
    multiStepEnterName
    multiStepCreating
    multiStepDone
)

type CreateMultiModel struct {
    step multiStep

    // Project multi-picker
    projects       []config.DiscoveredProject
    projectCursor  int
    projectFilter  string
    selected       map[string]bool   // project name -> selected

    // Name input
    nameInput string

    // Result
    result  *cell.MultiResult
    err     error

    // Dependencies
    cellService *cell.Service
}

func NewCreateMultiModel(svc *cell.Service, projects []config.DiscoveredProject) *CreateMultiModel
```

Flow:

1. **`multiStepPickProjects`** — fuzzy-find picker, mirroring `CreateModel`'s
   single-project picker at `internal/tui/create.go:90` but for picking one
   project at a time out of many.

   Layout (the Accept row only appears once `len(selected) ≥ 1`, and sits at
   the **top** of the list):

   ```
     Filter: foo█
                                                 3 selected

     → Accept (3 projects)        ← cursor lands here after each accept
       api
       auth
       web           [x]          ← already selected, stays in list
       foo-service                ← visible because filter matches
   ```

   Keys:
   - any printable rune: append to filter, rebuild filtered list, jump
     cursor to the first filtered project (moves off the Accept row).
   - `backspace`: trim filter; cursor stays on the current focused item if
     still visible, else top of list.
   - `ctrl-p` / `up` arrow: move cursor up.
   - `ctrl-n` / `down` arrow: move cursor down.
   - `enter` on a project row: toggle selection for that project
     (`selected[name] = !selected[name]`), **clear the filter**, and move
     cursor up to the Accept row.
   - `enter` on the Accept row: advance to `multiStepEnterName`.
     (Only reachable when `len(selected) ≥ 1` because the row is hidden
     otherwise.)
   - `esc`: cancel the overlay, discard selections.

   Rendering details:
   - Accept row label: `→ Accept (N project)` or `→ Accept (N projects)`
     with pluralization. Rendered with `selectedStyle` when focused,
     `successStyle` otherwise so it stands out from normal rows.
   - Project rows: two-space indent, `[x]` suffix for selected, nothing
     for unselected. Filter-matched substring can optionally be styled
     later — v1 is fine without it.
   - Counter in the top-right: `N selected` (dim). Hidden when 0.

2. **`multiStepEnterName`** — single-line input. `esc` goes back to step 1 (selections preserved). `enter` with non-empty input advances to `multiStepCreating` and dispatches the create command.

3. **`multiStepCreating`** — shows `Cloning <project>...` spinner-ish text (actual progress granularity comes later; for now just a static "Creating multicell…" line is fine).

4. **`multiStepDone`** — on success, show name + project list, then `switchToSession(result.Name)` (same pattern as `CreateModel.Update` at `internal/tui/create.go:68`). On failure, show error; any key dismisses.

### Overlay integration in `Model`

Mirror the existing `creating *CreateModel` overlay. Add to `tui.Model`:

```go
type Model struct {
    // ... existing ...
    creatingMulti *CreateMultiModel   // NEW
}
```

Update the gating block in `Update()`:

```go
// If creating a multicell, delegate to the multi overlay
if m.creatingMulti != nil {
    return m.updateCreatingMulti(msg)
}
```

And the `View()` block at `internal/tui/dashboard.go:484`:

```go
if m.creating != nil {
    content = m.creating.View(m.width, m.height)
} else if m.creatingMulti != nil {
    content = m.creatingMulti.View(m.width, m.height)
} else if m.openingProject { ...
```

Helper:

```go
func (m Model) startCreateMulti() (tea.Model, tea.Cmd) {
    dirs := m.globalCfg.ResolveProjectDirs()
    projects, _ := config.DiscoverProjects(dirs)
    if len(projects) == 0 {
        m.cells.message = "No projects found. Configure project_dirs first."
        return m, nil
    }
    m.creatingMulti = NewCreateMultiModel(m.cellService, projects)
    return m, nil
}

func (m Model) updateCreatingMulti(msg tea.Msg) (tea.Model, tea.Cmd) {
    updated, cmd := m.creatingMulti.Update(msg)
    if updated == nil {
        m.creatingMulti = nil
        return m, m.cells.LoadCells()
    }
    m.creatingMulti = updated
    return m, cmd
}
```

### Cells tab rendering

Multicells show up in the grouped cells list with their own section header. In `internal/tui/cells.go`'s `LoadCells` (~line 65), add a pre-pass before iterating normal cells to build a "Multicells" group:

Group decision: a cell with `Type == TypeMulti` goes under project heading `"(multicells)"` (or `"Multicells"`). Headless cells already bucket under `"(headless)"` at `cells.go:80` — use the same pattern.

```go
p := c.Project
switch c.Type {
case state.TypeHeadless:
    p = "(headless)"
case state.TypeMulti:
    p = "(multicells)"
}
if p == "" {
    p = "(headless)"  // existing behavior
}
```

Row rendering for multicells mirrors the headless case at `cells.go:293`:

```go
var name string
switch c.Type {
case state.TypeHeadless:
    name = "  ◇ " + c.Name
case state.TypeMulti:
    name = "  ⧉ " + c.Name
default:
    name = "    " + c.Name
}
```

(Unicode `⧉` reads as "two overlapping squares" — conveys "group of things". If it renders badly, fall back to `[M]` or `+ `.)

For now don't expand multicells in the cells tab to show child projects — that stays clean. Later: a `space` or arrow-right keybinding could expand a multicell to show its clones.

### Switcher inclusion

`hive switch` (`cmd/switch.go`) and `internal/tui/switcher.go` list every cell via `CellRepo.List`. Multicells are cells, so they already appear. Verify the switcher labels them sensibly — probably render the type marker in the switcher too.

In `internal/tui/switcher.go` (existing file), where each item line is built, add the type marker:

```go
display := c.Name
switch c.Type {
case state.TypeMulti:
    display += "  [multi]"
case state.TypeHeadless:
    display += "  [headless]"
}
```

---

## Part 5: CLI Output

The CLI surface for multicells is minimal (everything happens in the dashboard), but two things need to reflect them:

### `hive health`

Multicells should show up in `hive health` (consistency across DB, disk, tmux). In `cmd/health.go`, when checking clone path existence, a multicell's `ClonePath` is a directory containing child dirs. Treat it the same way — if the dir exists, good; if it's missing, flag as inconsistent.

Output example:

```
$ hive health
Cells
  myapp-feature      OK        (clone, tmux, db all consistent)
  api-experiment     MISSING   (clone dir missing: /Users/me/hive/cells/api/experiment)

Multicells
  auth-overhaul      OK        (parent dir + 3 child clones present, tmux alive)
  checkout-rewrite   STALE     (DB has record but parent dir missing)
```

### Multicell creation "success" text (inside the TUI)

The done-step view in `CreateMultiModel`:

```
  Multicell created

  Name:      auth-overhaul
  Dir:       /Users/me/hive/multicells/auth-overhaul
  Projects:  api, web, mobile
  Tmux:      auth-overhaul

  switching...
```

Then immediately `tmux switch-client -t auth-overhaul` via `switchToSession` (defined at `internal/tui/cells.go:343`).

### Multicell creation failure text

```
  Multicell create failed

  cloning "web" into /Users/me/hive/multicells/auth-overhaul: exit 128
  fatal: repository 'https://…' not found

  (any key to dismiss)
```

---

## Part 6: Integration Points

Summary of where existing code gets touched:

```
internal/state/models.go         add TypeMulti, MulticellChild struct
internal/state/db.go             add multicell_children CREATE TABLE
internal/state/multicell_repo.go NEW
cmd/root.go                      wire MulticellRepo + MulticellsDir into App and Service
internal/config/config.go        add MulticellsDir field + resolver + default
cmd/install.go                   include MulticellsDir in generated default config
internal/cell/service.go         add CreateMulti, removeMulticellDir; extend Kill for TypeMulti
internal/tui/create_multi.go     NEW — multi-project picker + name input overlay
internal/tui/dashboard.go        add C keybinding, overlay gating, startCreateMulti
internal/tui/cells.go            bucket TypeMulti rows under "(multicells)"; add ⧉ marker
internal/tui/switcher.go         show [multi] / [headless] marker in switcher
cmd/health.go                    include multicells in consistency report
```

Multicell creation step sequence:

```
Service.CreateMulti:
  1. Check uniqueness
  2. Validate projects
  3. mkdir parent dir
  4. For each project:
       provisionClone with TargetPath = parentDir/<project>-<multicell>
         (per-project hooks run here; failure rolls back parent dir)
  5. Create tmux in parent dir (HIVE_MULTICELL env)
  6. Insert cells row (type=multi)
  7. Insert multicell_children rows
  (no per-project ports in v1)
```

---

## Part 7: Edge Cases & Constraints

- **Name collision.** Multicell name shares the `cells.name` unique constraint. If a user tries to create multicell `foo` while cell `foo` exists (normal or headless), it fails early with the same "already exists" error.
- **Project name collision across child dirs.** Child dirs are named `<project>-<multicell>`, so two selected projects with the same basename would still collide (same multicell suffix = same final dir name). `config.DiscoverProjects` at `internal/config/config.go:186` already deduplicates discovered projects by name with a `seen` map, so this is enforced upstream — the user simply won't see two `api` entries in the picker.
- **Parent dir already exists but wasn't registered in DB.** The orphan cleanup at step 5 deletes it (same pattern as single cells at `internal/cell/service.go:66`). Logged but not surfaced to the user.
- **Partial clone failure.** If project 2 of 3 fails, roll back fully: `os.RemoveAll(parentDir)` wipes the already-cloned project 1 too. No half-built multicells on disk.
- **Tmux session name clash with an unmanaged tmux session** (a tmux session named `foo` exists but Hive doesn't know about it). `CreateSession` via `tmux new-session -d -s foo` would fail. Pre-kill the orphan via `_ = s.TmuxMgr.KillSession(ctx, opts.Name)` first — this is what the normal cell path does at `internal/cell/service.go:104`.
- **Multicells and queens.** v1 skips queen auto-creation for child projects. Rationale: the queen concept (phase 5) hasn't been merged in the current CLAUDE.md-described codebase, and even once it is, multicells wouldn't need their own queens — the child clones *are* ephemeral copies, not meant to be canonical.
- **Kill always tries to remove `parentDir`.** If the user manually `rm -rf`'d the parent dir, `os.RemoveAll` is a no-op on a missing path; we log a warning and continue. DB cleanup still happens via cascade.

---

## Part 8: Implementation Order

Numbered steps, each small enough for one PR. Dependencies are linear — each step unblocks the next.

### Step 1 — Data model

- Add `TypeMulti` constant to `internal/state/models.go`.
- Add `MulticellChild` struct to `internal/state/models.go`.
- Add `multicell_children` CREATE TABLE to the schema constant in `internal/state/db.go`.
- Create `internal/state/multicell_repo.go` with `AddChild`, `ListChildren`, `DeleteByMulticell`.
- Unit tests: insert multicell row + child rows, verify cascade delete when the parent `cells` row is removed.

### Step 2 — Config plumbing

- Add `MulticellsDir` field + `ResolveMulticellsDir()` to `internal/config/config.go`.
- Default to `~/hive/multicells` in `LoadGlobalOrDefault`.
- Update `cmd/install.go` to populate `MulticellsDir` in the generated default config.
- Add `MulticellRepo` + `MulticellsDir` to the `App` struct and `Service` struct; wire both in `cmd/root.go`'s `PersistentPreRunE`.

### Step 3 — `Service.CreateMulti`

- Implement in `internal/cell/service.go` with the step sequence from Part 3.
- Loops `provisionClone` once per project (per-project hooks work for free).
- Child dir naming: `<project>-<multicell>`.
- Add `removeMulticellDir` safety helper.
- Unit tests with a fake `CloneMgr` / `TmuxMgr`: happy path + failure at each
  step verifying full rollback.

### Step 4 — `Service.Kill` extension

- Branch on `TypeMulti` in the existing `Kill` method.
- Add explicit `MulticellRepo.DeleteByMulticell` call for defense-in-depth.
- Test: create a multicell via `CreateMulti`, kill it, assert: tmux gone, parent dir gone, cell row gone, multicell_children rows gone.

### Step 5 — TUI overlay (`create_multi.go`)

- New file with the three-step model (pick → name → done).
- Reuse `createKeys` for arrow/enter/backspace bindings; add a `Space` binding for toggle-select.
- Render with same `titleStyle` / `dimStyle` / `selectedStyle` as the single-cell picker for consistency.

### Step 6 — Dashboard wiring

- Add `C` keybinding to `dashKeys` in `internal/tui/dashboard.go`.
- Add `creatingMulti *CreateMultiModel` field and overlay gating in `Update`/`View`.
- Update cells-tab footer help text.

### Step 7 — Cells tab display

- In `internal/tui/cells.go`'s `LoadCells`, bucket multicells under `"(multicells)"`.
- Add `⧉` (or fallback) marker in row rendering.

### Step 8 — Switcher + health

- In `internal/tui/switcher.go`, render `[multi]` / `[headless]` tags next to names.
- In `cmd/health.go`, include multicells in the consistency check output (grouped separately).

### Step 9 — Manual integration test

Since tmux/git tests are hard to mock end-to-end, a manual test checklist:

1. `hive start` → dashboard opens.
2. Press `C` on Cells tab → project picker appears.
3. Select 2 projects (space to toggle) → enter → type `test-multi` → enter.
4. Dashboard switches into the new tmux session; cwd is `~/hive/multicells/test-multi`.
5. `ls` shows two dirs named `<project>-test-multi`, each a working git clone.
6. `tmux showenv HIVE_MULTICELL` prints `test-multi`.
7. Back in dashboard: multicell appears under "Multicells" group, switcher lists it as `test-multi  [multi]`.
8. `x` to kill → parent dir gone, tmux session gone, DB rows gone.
9. `hive health` shows no leftovers.

---

## Future

Explicitly out of v1, flagged as follow-ups:

- **Per-project port allocation with a prefix.** E.g. `API_PORT=3001`,
  `WEB_PORT=3002`. Two challenges: (1) `provisionClone` calls share a DB
  view of used ports but haven't persisted their own allocations yet, so
  sequential calls race; fix by threading an "extra used" set through
  `Allocator.Allocate`. (2) Need a prefixing convention — project name
  uppercased, or an explicit `port_prefix` field in the project config.
- **Ad-hoc project addition.** `hive multicell add <multicell> <project>` to
  clone another project into an existing multicell parent dir.
- **Expand-in-place in the cells tab.** Pressing `space` or `→` on a multicell
  row expands it to show child clone paths inline.
- **Multicell templates.** Save a named set of projects (e.g. "frontend-stack"
  = web+mobile+design-system) so you don't have to re-pick the same projects
  every time.
