# First-Class Multicell Children

## Overview

Today a multicell is **one** tmux session rooted at the parent dir, with N child clones underneath. Working inside a single session across N projects is awkward — users want one session per child, like a normal cell, while keeping the multicell's coordinator session for cross-project planning.

This plan promotes multicell children to first-class cells: each gets its own tmux session, port allocation, hooks, per-project env, and default layout — exactly like a normal cell. The multicell itself becomes a coordinator: a tmux session rooted at the parent dir, a `cells` row of `TypeMulti`, and N child `cells` rows of `TypeMultiChild` pointing back to it via a new `parent` column.

Out of scope for this PR:
- Adding a project to an existing multicell (future "ad-hoc add" feature).
- Recreating a killed child without recreating the whole multicell.
- Expand-in-place UI on the cells tab.

---

## Part 1: Data Model

### New cell type

Add `TypeMultiChild` to `internal/state/models.go`:

```go
type CellType string

const (
    TypeNormal     CellType = "normal"
    TypeHeadless   CellType = "headless"
    TypeMulti      CellType = "multi"       // coordinator (parent dir + its own tmux session)
    TypeMultiChild CellType = "multi_child" // NEW — a child clone inside a multicell
)
```

### `Parent` field on `Cell`

`Cell` gains a `Parent` field. Populated only for `TypeMultiChild`; empty for all other types.

```go
type Cell struct {
    ID        int64
    Name      string
    Project   string
    ClonePath string
    Status    CellStatus
    Ports     string
    Type      CellType
    Parent    string     // NEW — multicell coordinator name for TypeMultiChild, else ""
    CreatedAt time.Time
    UpdatedAt time.Time
}
```

### Schema

Add a `parent` column to `cells`. Nullable. FK back to `cells(name)` with `ON DELETE CASCADE`, so deleting a coordinator wipes its children's rows.

In `internal/state/db.go`'s `runMigrations`:

```go
migrations := []string{
    `ALTER TABLE notifications ADD COLUMN source_pane TEXT NOT NULL DEFAULT ''`,
    `ALTER TABLE cells ADD COLUMN parent TEXT DEFAULT NULL REFERENCES cells(name) ON DELETE CASCADE`, // NEW
    `CREATE INDEX IF NOT EXISTS idx_cells_parent ON cells(parent)`,                                    // NEW
}
```

SQLite allows `ALTER TABLE ... ADD COLUMN` with a `REFERENCES` clause. The FK is enforced because `foreign_keys(1)` pragma is already set in `Open()`.

### Drop `multicell_children` from the live schema

Remove the `CREATE TABLE IF NOT EXISTS multicell_children ...` block from the `schema` constant in `internal/state/db.go`. A fresh install will never have the table. Existing installs still have the table sitting in their DB with data in it — that's fine; no committed Go code touches it anymore. The user's one-off migration script (see Part 3) reads it and drops it when they run the migration.

This PR deletes `MulticellRepository`, the `MulticellChild` struct, and `multicell_repo.go` outright. Nothing in the committed codebase references the old table after this PR lands. Remove `MulticellRepo` from the `App` struct (`cmd/root.go`) and from the `Service` struct (`internal/cell/service.go`); drop the wiring in `PersistentPreRunE`.

### Scan + insert: include `parent`

Every `SELECT` / `Scan` in `internal/state/cell_repo.go` must include `parent`. Every `INSERT` must set it.

```sql
SELECT id, name, project, clone_path, status, ports, type, parent, created_at, updated_at
FROM cells ...
```

```sql
INSERT INTO cells (name, project, clone_path, status, ports, type, parent, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
```

`scanCell` / `scanCells` add `&c.Parent` between `&c.Type` and `&c.CreatedAt`.

Use `sql.NullString` for `parent` in the scan, then normalize to `""` on the Go side — keeps the struct field as plain `string`.

### New repository method

In `internal/state/cell_repo.go`:

```go
// ListChildren returns all cells whose parent column equals the given multicell name,
// ordered by project for stable display.
func (r *CellRepository) ListChildren(ctx context.Context, multicellName string) ([]Cell, error)
```

Query:
```sql
SELECT id, name, project, clone_path, status, ports, type, parent, created_at, updated_at
FROM cells WHERE parent = ? ORDER BY project
```

This replaces `MulticellRepository.ListChildren` at every call site **except** the migration command.

---

## Part 2: Cell Service Changes

### Helper: `provisionChildCell`

Extract a new helper that provisions one child cell end-to-end. Lives in `internal/cell/service.go`. Reuses `provisionClone` for the clone/hooks/ports phase, then adds tmux session + layout + DB row.

```go
// ProvisionChildOpts describes a single multicell child to provision.
type ProvisionChildOpts struct {
    Project     string // e.g. "api"
    SourceRepo  string // git source path/URL
    ParentName  string // coordinator multicell name, e.g. "auth-overhaul"
    ParentDir   string // absolute path to the parent dir
}

// ProvisionChildResult is the outcome for one successful child.
type ProvisionChildResult struct {
    CellName  string         // "<project>-<parent>"
    ClonePath string         // absolute path to child clone
    Ports     map[string]int
    HookLog   string
    LayoutLog string
}

// provisionChildCell provisions a single first-class multicell child:
//   provisionClone (clone + ports + hooks) → kill orphan tmux → CreateSession →
//   apply default layout → insert TypeMultiChild cells row with parent=<ParentName>.
//
// Returns the result on success, or a rollback error. Rollback removes the child's
// tmux session and clone dir; it does NOT touch the parent dir or sibling cells.
func (s *Service) provisionChildCell(ctx context.Context, opts ProvisionChildOpts) (*ProvisionChildResult, error)
```

Step sequence inside `provisionChildCell`:

1. Compute `cellName := opts.Project + "-" + opts.ParentName`.
2. Compute `target := filepath.Join(opts.ParentDir, cellName)`. (Child dir name == cell name == tmux session name.)
3. Verify `cellName` is free in the DB — if another cell with this name exists, return an error before doing any work.
4. Call `provisionClone` with:
   ```go
   ProvisionOpts{
       Project:    opts.Project,
       SourceRepo: opts.SourceRepo,
       TargetPath: target,
       CellName:   cellName,          // HIVE_CELL on hooks is the child's own cell name
       AllocPorts: true,              // CHANGED from today's multicell behavior
       ExtraEnv: map[string]string{
           "HIVE_MULTICELL":     opts.ParentName,
           "HIVE_MULTICELL_DIR": opts.ParentDir,
           "HIVE_PROJECT":       opts.Project,
       },
   }
   ```
5. `s.TmuxMgr.KillSession(ctx, cellName)` to wipe any orphaned session with this name.
6. `s.TmuxMgr.CreateSession(ctx, cellName, target, prov.Env)`. On failure: rm -rf child dir, return error.
7. Apply `prov.Config.Layouts["default"]` if present via `layout.Apply(ctx, cellName, target, lyt)`. Layout errors are logged but non-fatal (matches `Service.Create`).
8. Marshal ports to JSON.
9. Insert cells row:
   ```go
   &state.Cell{
       Name:      cellName,
       Project:   opts.Project,
       ClonePath: target,
       Status:    state.StatusRunning,
       Ports:     portsJSON,
       Type:      state.TypeMultiChild,
       Parent:    opts.ParentName,
   }
   ```
   On failure: kill tmux session, rm -rf child dir, return error.
10. Return `ProvisionChildResult`.

This helper is also used by the migration command (with layout+tmux+DB, but NOT hooks — see Part 3).

### `Service.CreateMulti` — new step sequence

Rewrite the body of `CreateMulti` in `internal/cell/service.go`.

Old flow (keep for reference; delete after this PR):

```
validate name unique → validate projects → mkdir parent → provisionClone per project
→ kill orphan parent tmux → create coordinator tmux → insert TypeMulti row
→ insert multicell_children rows per project
```

New flow:

```
1. Validate multicell name unique in cells
2. Validate at least one project
3. Validate no duplicate project names
4. Validate every prospective child name "<project>-<name>" is also free in cells
5. Kill any orphaned coordinator tmux session (must precede the rmdir —
   a stale session with cwd inside the old parent dir keeps file handles
   and races against the wipe)
6. mkdir parent dir (os.RemoveAll first for orphan cleanup)
7. Create coordinator tmux session rooted at parent dir
8. Insert coordinator cells row (Type=TypeMulti, Parent="")  ← needed before children for FK
9. For each project in opts.Projects (sequential):
     a. provisionChildCell  (clone + ports + hooks + tmux + layout + child cells row)
     b. On failure: full rollback — kill all already-created children (tmux + row),
        kill coordinator tmux, delete coordinator cells row, os.RemoveAll parent dir,
        return error.
10. Return MultiResult
```

This is the same kill-before-mkdir ordering normal `Create` already uses; keeps the two code paths symmetric.

Step 4 — new up-front collision check:

```go
for _, p := range opts.Projects {
    childName := p.Name + "-" + opts.Name
    existing, err := s.CellRepo.GetByName(ctx, childName)
    if err != nil {
        return nil, fmt.Errorf("checking child cell existence: %w", err)
    }
    if existing != nil {
        return nil, fmt.Errorf("child cell %q would collide with existing cell", childName)
    }
}
```

Step 8 — coordinator row:

```go
coord := &state.Cell{
    Name:      opts.Name,
    Project:   "",
    ClonePath: parentDir,
    Status:    state.StatusRunning,
    Ports:     "{}",
    Type:      state.TypeMulti,
    Parent:    "",
}
if err := s.CellRepo.Create(ctx, coord); err != nil {
    rollbackCoord()  // kill tmux + rm parentDir
    return nil, fmt.Errorf("saving coordinator cell: %w", err)
}
```

Step 9b — cumulative rollback. Keep track of successful child cell names in a slice; on failure, iterate and kill+delete each, then roll back the coordinator.

### Coordinator env vars (unchanged)

The coordinator session gets:
```go
envVars := map[string]string{
    "HIVE_CELL":          opts.Name,
    "HIVE_MULTICELL":     opts.Name,
    "HIVE_MULTICELL_DIR": parentDir,
}
```

No ports, no hooks, no layout — the coordinator is a plain shell at the parent dir. This matches decision 6.

### Drop `MulticellRepo` usage in `CreateMulti`

`CreateMulti` no longer writes to `multicell_children`. Remove the final loop that inserts `MulticellChild` rows and drop the `MulticellRepo` field from the `Service` struct entirely — it's unused.

### `Service.Kill` — handle `TypeMultiChild` and cascade for `TypeMulti`

Update the switch in `Kill`:

```go
// Before the switch: ALWAYS kill the cell's own tmux session.
_ = s.TmuxMgr.KillSession(ctx, cellName)

switch cell.Type {
case state.TypeMulti:
    // Kill each child's tmux session (children's clone dirs vanish
    // when we rm -rf the parent dir below, but their sessions won't
    // self-destruct).
    children, err := s.CellRepo.ListChildren(ctx, cellName)
    if err != nil {
        slog.Warn("listing multicell children", "name", cellName, "error", err)
    }
    for _, child := range children {
        if err := s.TmuxMgr.KillSession(ctx, child.Name); err != nil {
            slog.Warn("failed to kill child tmux session", "name", child.Name, "error", err)
        }
    }
    // Remove parent dir (wipes all child dirs in one shot).
    if cell.ClonePath != "" {
        if err := s.removeMulticellDir(cell.ClonePath); err != nil {
            slog.Warn("failed to remove multicell dir", "path", cell.ClonePath, "error", err)
        }
    }
    // FK ON DELETE CASCADE on cells(parent) removes child cell rows
    // when we delete the coordinator row below.

case state.TypeMultiChild:
    // Remove only this child's clone dir. Parent dir and siblings stay.
    if cell.ClonePath != "" {
        if err := s.removeMulticellDir(cell.ClonePath); err != nil {
            slog.Warn("failed to remove multicell child dir", "path", cell.ClonePath, "error", err)
        }
    }

case state.TypeHeadless:
    // No clone.

default: // TypeNormal
    if cell.ClonePath != "" {
        if err := s.CloneMgr.Remove(cell.ClonePath); err != nil {
            slog.Warn("failed to remove clone", "path", cell.ClonePath, "error", err)
        }
    }
}

// Delete DB record. For TypeMulti, FK cascades to TypeMultiChild rows.
if err := s.CellRepo.Delete(ctx, cellName); err != nil {
    return fmt.Errorf("deleting cell record: %w", err)
}
```

Remove the old `DeleteByMulticell` call — no longer needed; the old `multicell_children` table isn't touched by normal kill paths.

`removeMulticellDir` already refuses deletions outside `MulticellsDir`. That helper works for both a parent dir and a single child dir (any descendant path is covered by the prefix check).

### `Service.ListMultiChildren`

Reimplement to read from `cells` instead of `multicell_children`:

```go
func (s *Service) ListMultiChildren(ctx context.Context, multicellName string) ([]state.Cell, error) {
    return s.CellRepo.ListChildren(ctx, multicellName)
}
```

Note the return type changes from `[]state.MulticellChild` to `[]state.Cell`. Any callers need updating — currently only the TUI, which this plan also changes.

---

## Part 3: Migrating Existing Data

No committed command. The migration is a one-off — only the plan author has existing multicell data (one `auth-overhaul` with 2 children as of this writing). When the PR is ready to merge, write a throwaway script, run it once against the DB, then discard it.

### What the script must do

For each row in `multicell_children`, produce an equivalent first-class child cell:

1. **Read**: `SELECT multicell_name, project, clone_path, source_repo FROM multicell_children`.
2. **Per child** (`childName := project + "-" + multicell_name`):
   - Load the project config from `~/.hive/config/<project>.yml` (reuse `config.LoadProjectOrDefault`).
   - Allocate ports via `ports.Allocator` (if the project declares `port_vars`).
   - Build env via `envars.Build(...)` — **do not** reconstruct the map inline. Using the same helper that `provisionClone` uses guarantees migrated children get byte-identical env to freshly-created ones. Pass in the allocated ports, the project's static `env`, and the extras `HIVE_CELL=<childName>`, `HIVE_MULTICELL=<multicell_name>`, `HIVE_MULTICELL_DIR=<parent dir>`, `HIVE_PROJECT=<project>`, `HIVE_REPO_DIR=<source_repo>`. If `envars.Build`'s current signature can't accept arbitrary extras, add an `Extras map[string]string` param to it as part of this PR — every caller benefits.
   - `tmux kill-session -t <childName>` (orphan cleanup, ignore errors).
   - `tmux new-session -d -s <childName> -c <clone_path> -e KEY=VAL ...` via `tmux.Manager.CreateSession`.
   - Apply default layout via `layout.Apply` if configured.
   - `INSERT INTO cells (name, project, clone_path, status, ports, type, parent, created_at, updated_at) VALUES (...)` with `type='multi_child'` and `parent='<multicell_name>'`.
3. **Drop** the old table: `DROP TABLE multicell_children`.

**Hooks do NOT re-run** — the clones already exist, dependencies are installed, re-running would stomp working state.

**Why go through `envars.Build` for a throwaway script?** Because the script runs *after* this PR lands, when `envars.Build` may already have drifted (e.g. a new `HIVE_*` var added in the same diff). A hand-rolled map at script-write time will miss that and produce sessions subtly different from freshly-created ones — the exact bug this migration is supposed to avoid.

### How to run it

The script can be a throwaway `main.go` placed somewhere outside the module source tree (e.g. `/tmp/hive-migrate.go`) that imports the installed `github.com/lothardp/hive/internal/...` packages, or it can be done even simpler as a shell sequence: raw SQL to read, manual `tmux new-session` commands, raw SQL to insert, raw SQL to drop.

Since there is exactly one DB that needs migrating, "simplest possible" is likely best — at the time of migration, read the rows, run the tmux commands by hand (or via a short bash loop), insert the rows with a few SQL statements, drop the table. Claude can drive this interactively when the PR is ready to land.

### Acceptance check after migration

- `hive health` shows coordinator + each child as `ok` / `ok (child of ...)`.
- Dashboard shows the multicell group with coordinator + children, all `●` alive.
- `tmux list-sessions` includes each child session.
- `SELECT COUNT(*) FROM multicell_children` errors with "no such table" (because it was dropped).

---

## Part 4: Dashboard (Cells Tab)

### Grouping rule

Today `CellsModel.LoadCells` buckets by `Type`:
- `TypeHeadless` → `"(headless)"`
- `TypeMulti` → `"(multicells)"`
- default → `c.Project`

New rule — each multicell gets its own group, children live under it:

```go
p := ""
switch c.Type {
case state.TypeHeadless:
    p = "(headless)"
case state.TypeMulti:
    // Coordinator: bucket under its own name.
    p = "⧉ " + c.Name
case state.TypeMultiChild:
    // Child: bucket under its parent's name, matching the coordinator's bucket.
    p = "⧉ " + c.Parent
default:
    p = c.Project
    if p == "" { p = "(headless)" }
}
```

Effect:

```
▼ ⧉ auth-overhaul
    ⧉ auth-overhaul              ●  2h
      api-auth-overhaul          ●  2h  (PORT=3003, DB_PORT=5435)
      web-auth-overhaul          ●  2h  (PORT=3004)
▼ myapp
      myapp-work                 ●  1d
▼ (headless)
    ◇ scratch                    ●  5m
```

Both the coordinator row (marked `⧉`) and each child row appear in the same group. The group header echoes the multicell name with the `⧉` marker. Alphabetical sort puts multicell groups before normal project groups because `⧉` (U+29C9, a 3-byte UTF-8 sequence starting with `0xE2`) sorts after `sort.Strings`'s byte-wise comparison of ASCII letters — wait: `0xE2` is greater than any ASCII letter, so multicell groups actually sort **last**, not first. If you want them at the top, pick an ASCII-range prefix or add a custom sort in `LoadCells` that keys on `Type` before `Name`. Worth deciding before merging; the example in this doc assumes groups-at-top.

### Intra-bucket row order

`sort.Strings` only orders the group headers; within a bucket, rows appear in whatever order `CellRepo.List` returned them (today: `ORDER BY created_at DESC`). That means the coordinator will **not** reliably sit above its children — a freshly-killed-and-recreated child could end up first.

Fix: after grouping, sort each bucket's rows with `TypeMulti` first, then `TypeMultiChild` alphabetical by `Name`. Roughly:

```go
for _, g := range groups {
    sort.SliceStable(g.Cells, func(i, j int) bool {
        a, b := g.Cells[i], g.Cells[j]
        if a.Type != b.Type {
            return a.Type == state.TypeMulti   // coordinator floats to top
        }
        return a.Name < b.Name
    })
}
```

This also keeps normal-project buckets deterministic instead of `created_at`-dependent, which is a nice side effect.

### Row rendering

In `CellsModel.View` row-rendering loop, add a case for `TypeMultiChild`:

```go
var name string
switch c.Type {
case state.TypeHeadless:
    name = "  ◇ " + c.Name
case state.TypeMulti:
    name = "  ⧉ " + c.Name
case state.TypeMultiChild:
    // No prefix marker — child's membership is already shown by the group header.
    name = "    " + c.Name
default:
    name = "    " + c.Name
}
```

Optional enhancement — show allocated ports next to child rows (normal cells don't show ports today, but it's useful for multicell debugging). Out of scope for first cut; revisit if the grouping feels bare.

### Kill confirmation text

Unchanged. `x` triggers the confirm. When the user hits `y`:
- If the selected row is a `TypeMulti` coordinator, the existing `cellService.Kill` path kills all child sessions and wipes the parent dir.
- If the selected row is a `TypeMultiChild`, `Kill` removes just that child dir and session.
- Siblings are unaffected either way.

No new keybindings. Matches decision 3.

### Create flow (unchanged from user's perspective)

`create_multi.go` still invokes `cellService.CreateMulti` the same way. The new `CreateMulti` internally creates N child sessions — no UI change needed beyond the "Creating multicell..." spinner already shown.

After success, `switchToSession(result.Name)` switches to the **coordinator**, matching decision 8.

---

## Part 5: Switcher Display

In `internal/tui/switcher.go`, the existing display code tags headless and multi cells. Extend it to tag children with their multicell membership.

Current (approximate) pattern:

```go
display := c.Name
switch c.Type {
case state.TypeMulti:
    display += "  [multi]"
case state.TypeHeadless:
    display += "  [headless]"
}
```

New:

```go
display := c.Name
switch c.Type {
case state.TypeMulti:
    display += "  [multi]"
case state.TypeMultiChild:
    display += "  [" + c.Parent + "]"
case state.TypeHeadless:
    display += "  [headless]"
}
```

Example switcher output:

```
  myapp-work                         ● 1d
  auth-overhaul         [multi]      ● 2h
  api-auth-overhaul     [auth-overhaul]   ● 2h
> web-auth-overhaul     [auth-overhaul]   ● 2h
  scratch               [headless]   ● 5m
```

---

## Part 6: Health Command

`cmd/health.go` today scans `multicells_dir` at depth 1 and treats each subdir as a multicell. With first-class children, it must also scan depth 2 and match each child dir against the new `TypeMultiChild` rows.

### Disk scan — extend to depth 2

```go
// 2b. Multicell coordinators (depth 1) AND children (depth 2).
multicellsDir := app.Config.ResolveMulticellsDir()
mcDirs, _ := os.ReadDir(multicellsDir)
for _, entry := range mcDirs {
    if !entry.IsDir() { continue }
    mcName := entry.Name()
    mcPath := filepath.Join(multicellsDir, mcName)
    diskCells[mcName] = mcPath  // coordinator

    // Depth 2: child clone dirs named "<project>-<mcName>".
    children, _ := os.ReadDir(mcPath)
    for _, child := range children {
        if !child.IsDir() { continue }
        // Reject directories that don't match the "<project>-<mcName>" naming
        // convention, so user-created scratch files/dirs don't pollute health.
        if !strings.HasSuffix(child.Name(), "-"+mcName) { continue }
        diskCells[child.Name()] = filepath.Join(mcPath, child.Name())
    }
}
```

### Status classification

Extend the switch inside the report loop:

```go
isHeadless    := cellType == state.TypeHeadless
isMulti       := cellType == state.TypeMulti
isMultiChild  := cellType == state.TypeMultiChild   // NEW

switch {
case isHeadless:
    // (unchanged)

case isMulti:
    if inDB && onDisk && inTmux {
        status = "ok (multi)"
        healthy++
    } else {
        status = "multi: " + describeIssue(inDB, onDisk, inTmux)
        issues++
    }

case isMultiChild:                                   // NEW
    if inDB && onDisk && inTmux {
        status = "ok (child of " + dbCells[name].Parent + ")"
        healthy++
    } else {
        status = "child: " + describeIssue(inDB, onDisk, inTmux)
        issues++
    }

default:
    // normal cells (unchanged)
}
```

Orphan children (dir exists, no DB row) surface as `no DB record` — caller can kill the dir manually or rerun the migrate command.

---

## Part 7: App Struct & Wiring

Remove `MulticellRepo *state.MulticellRepository` from both the `App` struct (`cmd/root.go`) and the `Service` struct (`internal/cell/service.go`). Drop the wiring in `PersistentPreRunE` that creates it. Delete `internal/state/multicell_repo.go` and the `MulticellChild` struct from `internal/state/models.go`.

No new App fields are required.

---

## CLI Output Examples

### Creating a multicell (new behavior)

```
Dashboard: press C → pick api,web → name "auth-overhaul"

Creating multicell...
  Cloning api into /Users/me/hive/multicells/auth-overhaul/api-auth-overhaul
  Running api hooks (2/2)
  Allocated ports: PORT=3003, DB_PORT=5435
  Created tmux session "api-auth-overhaul"
  Applied default layout

  Cloning web into /Users/me/hive/multicells/auth-overhaul/web-auth-overhaul
  Running web hooks (1/1)
  Allocated ports: PORT=3004
  Created tmux session "web-auth-overhaul"
  Applied default layout

  Created coordinator tmux session "auth-overhaul"

Multicell created
  Name:      auth-overhaul
  Dir:       /Users/me/hive/multicells/auth-overhaul
  Children:  api-auth-overhaul, web-auth-overhaul
  Tmux:      auth-overhaul (+ 2 child sessions)

  switching to coordinator...
```

(The progress log is best-effort. If implementation ends up keeping the existing "Creating multicell..." one-liner, that's fine — just update the success screen to list child cell names.)

### Kill a single child

```
Dashboard: cursor on "web-auth-overhaul" → press x → y

Killed "web-auth-overhaul"
```

Afterwards the cells tab still shows the coordinator and `api-auth-overhaul`.

### Kill the coordinator (cascade)

```
Dashboard: cursor on "auth-overhaul" → press x → y

Killed "auth-overhaul"
```

Cells tab: the entire `⧉ auth-overhaul` group disappears.

### Health after migration

```
$ hive health
CELL                                 DB      DISK    TMUX    STATUS
─────────────────────────────────────────────────────────────────────
auth-overhaul                          ✓       ✓       ✓     ok (multi)
api-auth-overhaul                      ✓       ✓       ✓     ok (child of auth-overhaul)
web-auth-overhaul                      ✓       ✓       ✓     ok (child of auth-overhaul)
myapp-work                             ✓       ✓       ✓     ok
─────────────────────────────────────────────────────────────────────
4 healthy, 0 issues
```

---

## Implementation Order

Each step is small enough to be reviewed independently. Steps 1–4 can be done before any user-facing changes are visible; step 5 runs the one-off migration; step 6+ polish the surface.

### Step 1 — Data model & schema

- Add `TypeMultiChild` to `internal/state/models.go`.
- Add `Parent string` field to `Cell` struct.
- Delete the `MulticellChild` struct from `internal/state/models.go`.
- Delete `internal/state/multicell_repo.go`.
- Remove `CREATE TABLE multicell_children` from the `schema` const in `internal/state/db.go` (fresh installs never see it; existing installs keep the table in their DB until the migration script drops it).
- Add migration SQL to `runMigrations` for the `parent` column + index.
- Update `cell_repo.go`: all SELECTs/INSERTs/scans include `parent`; add `ListChildren(ctx, parent)`.
- Update `cell_repo_test.go` for the new column + method.
- Remove `MulticellRepo` field from `App` (`cmd/root.go`) and the `NewMulticellRepository` call in `PersistentPreRunE`.
- Remove `MulticellRepo` field from `Service` (`internal/cell/service.go`).

### Step 2 — Service: new `provisionChildCell` helper

- Add `provisionChildCell` and `ProvisionChildOpts` / `ProvisionChildResult` in `internal/cell/service.go`.
- Unit test: with a fake CloneMgr/TmuxMgr, drive a full success path + rollback after tmux failure + rollback after cell-row insert failure.

### Step 3 — Service: rewrite `CreateMulti`

- Add up-front collision check for every child cell name.
- Insert coordinator row BEFORE any child rows (FK requirement).
- Loop `provisionChildCell`. Track created children for rollback.
- Stop writing to `multicell_children` (the whole block goes away, not just the loop).
- Update `ListMultiChildren` to use `CellRepo.ListChildren` (return type changes from `[]MulticellChild` to `[]Cell`).
- Extend `service_test.go` for: happy path, mid-loop rollback, name collision detection.

### Step 4 — Service: extend `Kill` for `TypeMultiChild`; cascade child sessions for `TypeMulti`

- Kill each child's tmux session when killing a coordinator.
- For `TypeMultiChild`: remove just the child dir via `removeMulticellDir`.
- Remove the `DeleteByMulticell` call from `Kill` — FK cascade handles it.
- Test: kill child leaves siblings/coordinator intact; kill coordinator removes everything.

### Step 5 — Run the one-off migration against the author's DB

- Before merging, run the throwaway script described in Part 3 against `~/.hive/state.db`.
- Verify: coordinator + 2 children visible in dashboard, all tmux sessions alive, `multicell_children` table dropped.
- No code from this step is committed.

### Step 6 — TUI cells tab grouping

- Update `LoadCells` bucket rule: coordinators and their children share an `⧉ <name>` bucket.
- Add intra-bucket sort: `TypeMulti` first, then the rest alphabetical by name (see "Intra-bucket row order" above). Without this the coordinator won't reliably sit above its children.
- Decide whether multicell groups render at the top or bottom of the cells tab. `sort.Strings` on UTF-8 `⧉` puts them last, not first — if groups-at-top is wanted, either switch to an ASCII-range marker or add a custom sort on group headers keyed on "starts with `⧉`".
- Update row rendering to handle `TypeMultiChild`.
- Verify `skipToCell` still works (child rows are real cell rows, so cursor navigation just works).

### Step 7 — Switcher display

- Add `case TypeMultiChild` in the switcher's display tag logic.
- Tag reads `"[" + c.Parent + "]"`.

### Step 8 — Health

- Extend disk scan to depth 2 under `multicells_dir`, filtering children by the `-<mcName>` suffix.
- Add `isMultiChild` branch to the status classifier.

### Step 9 — Manual integration test

1. After migration (step 5) + upgrade: open dashboard → multicell group shows coordinator + 2 children, all `●`.
2. `hive switch` lists children with `[auth-overhaul]` tag.
3. Switch into a child session → `echo $HIVE_CELL $HIVE_MULTICELL $HIVE_PROJECT` prints expected values; ports are in env.
4. Kill one child → siblings + coordinator alive; dir gone.
5. Kill coordinator → everything gone.
6. Recreate via dashboard `C` → verify new flow produces the same structure.
7. `hive health` shows all OK.

---

## Edge Cases & Constraints

- **Child cell name collision with existing normal cell**: detected in Step 9b of `CreateMulti` before any work is done.
- **Partial multicell after creation failure**: full rollback includes already-created children (tmux + cell row + dir via parent-dir rm).
- **Killing a child while another has a layout-startup command running**: unrelated — each child is its own tmux session.
- **Running the migration script twice**: the second run should see zero `multicell_children` rows (table was dropped) and no-op. If it runs partway and aborts, the script can be rewritten as needed — nothing about this is committed code.
- **User manually deleted a child dir between creation and kill**: `removeMulticellDir` runs `os.RemoveAll` on a missing path — no-op, logged.
- **User manually deleted the parent dir**: killing the coordinator logs a warning but still deletes DB rows via FK cascade.
- **Orphaned tmux session with a prospective child's name**: every child creation path calls `KillSession` first, same as normal cells.
- **Port exhaustion mid-loop**: `provisionChildCell` returns an error; the full-rollback path in `CreateMulti` tears down already-created siblings.
- **FK enforcement on SQLite**: `foreign_keys(1)` pragma is already set at `internal/state/db.go:39` — cascade deletes will fire.

---

## Future

- **Ad-hoc child add** — dashboard action to clone one more project into an existing multicell (calls `provisionChildCell` standalone).
- **Ad-hoc child recreate** — after killing a child, bring it back without rebuilding the whole multicell. Same `provisionChildCell` call with the same opts.
- **Per-child ports display on cells tab** — show `PORT=3003, DB_PORT=5435` inline on child rows.
- **Expand/collapse multicell groups** — `space`/`→` on the coordinator row hides its children.
