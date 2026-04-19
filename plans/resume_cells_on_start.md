# Resume Cells on Start

## Overview

After a machine restart (or any event that kills the tmux server), the DB still knows about every cell but none of their tmux sessions exist. Today the user has to recreate each cell by hand. This feature makes `hive start` automatically rebuild tmux sessions for every cell it can reconcile between the DB and disk — no re-cloning, no hooks, no port re-allocation. Just rehydrate the tmux side.

## Scope & non-goals

In scope:
- Rebuild tmux sessions for `normal`, `headless`, `multi` (coordinator), and `multi_child` cells whose DB record exists and (for types with a clone) whose clone dir is intact.
- Reuse the ports already stored in the DB.
- Re-apply the `default` layout for normal cells and multicell children (so dev servers etc. start back up).
- Wire this into `hive start` only — runs automatically when the `hive` dashboard session does not yet exist.

Not in scope:
- Cleaning up stale DB rows (clone dir gone, etc.). `hive health` remains the tool for that.
- Adopting orphan on-disk clones that have no DB record.
- Verifying or reallocating ports that another process has taken since the DB record was written.
- Re-running setup hooks.
- A standalone `hive resume` command.

---

## Feature: `Service.ResumeAll`

### What it does

Walks the DB, figures out which cells are missing from tmux, and recreates their sessions using only state that's already persisted. Best-effort: each cell is independent, failures are collected into a summary, and one failure never aborts the rest.

### Data model

No schema changes. Uses existing fields:

- `state.Cell.Name`, `.Project`, `.ClonePath`, `.Ports` (JSON `map[string]int`), `.Type` (`normal` | `headless` | `multi` | `multi_child`), `.Parent` (the coordinator name, only set on `multi_child`).
- `config.ProjectConfig.Env`, `.Layouts`, `.RepoPath` — loaded per-cell via `config.LoadProjectOrDefault(hiveDir, project)` (falls back to empty config if the project config is missing).

### Function signatures

New file `internal/cell/resume.go`:

```go
// ResumeSummary describes the outcome of ResumeAll.
type ResumeSummary struct {
    Resumed []string         // cell names that got a fresh tmux session
    Skipped []SkippedCell    // cells deliberately left alone (already running, missing disk, etc.)
    Failed  []FailedCell     // cells we tried to resume but couldn't
}

type SkippedCell struct {
    Name   string
    Reason string // "already running", "no clone dir", "no parent dir"
}

type FailedCell struct {
    Name  string
    Error error
}

// ResumeAll recreates tmux sessions for cells present in the DB that aren't
// currently running. Uses stored ports, skips hooks, re-applies the default
// layout for normal cells. Returns a summary; per-cell failures do not abort.
func (s *Service) ResumeAll(ctx context.Context) (*ResumeSummary, error)
```

### Execution

1. **Gather current state**
   - `sessions, err := s.TmuxMgr.ListSessions(ctx)` → build `map[string]bool`, dropping the `hive` entry.
   - `cells, err := s.CellRepo.List(ctx)`.
   - A top-level error from either of these aborts with a wrapped error — we can't make sensible decisions without both sides.

2. **For each DB cell**, skip if already in the tmux set (record in `Skipped` with reason `"already running"`), otherwise dispatch on `cell.Type`:

   **`normal`**:
   1. If `cell.ClonePath` is empty or `os.Stat(cell.ClonePath)` fails → `Skipped{Reason: "no clone dir"}`.
   2. `projectCfg := config.LoadProjectOrDefault(s.HiveDir, cell.Project)`.
   3. Unmarshal `cell.Ports` (defaulting to `{}` if empty) into `map[string]int`. On JSON error → `Failed`.
   4. Build env:
      ```go
      env := envars.BuildVars(ports, projectCfg.Env)
      env["HIVE_CELL"] = cell.Name
      if projectCfg.RepoPath != "" {
          env["HIVE_REPO_DIR"] = config.ExpandTilde(projectCfg.RepoPath) // see note below
      }
      ```
   5. `s.TmuxMgr.KillSession(ctx, cell.Name)` — best-effort (defensive, in case a half-dead session lingers).
   6. `s.TmuxMgr.CreateSession(ctx, cell.Name, cell.ClonePath, env)`. On failure → `Failed`.
   7. If `lyt, ok := projectCfg.Layouts["default"]; ok`:
      `layout.Apply(ctx, cell.Name, cell.ClonePath, lyt)` — on failure log a warning but still count as `Resumed` (matches how `Service.Create` handles layout failure).
   8. Append to `Resumed`.

   **`headless`**:
   1. Working dir = `cell.ClonePath` (set by `CreateHeadless`; defaults to `$HOME` if it was ever empty).
   2. `env := map[string]string{"HIVE_CELL": cell.Name}`.
   3. `KillSession` + `CreateSession(ctx, cell.Name, dir, env)`. No layout.
   4. On failure → `Failed`, otherwise → `Resumed`.

   **`multi`** (coordinator):
   1. If `cell.ClonePath` is empty or `os.Stat` fails → `Skipped{Reason: "no parent dir"}`.
   2. Env matches `Service.CreateMulti`'s coordinator session (plain shell, no ports/hooks/layout):
      ```go
      env := map[string]string{
          "HIVE_CELL":          cell.Name,
          "HIVE_MULTICELL":     cell.Name,
          "HIVE_MULTICELL_DIR": cell.ClonePath,
      }
      ```
   3. `KillSession` + `CreateSession(ctx, cell.Name, cell.ClonePath, env)`. No layout.
   4. On failure → `Failed`, otherwise → `Resumed`.

   **`multi_child`**:
   Same as `normal`, but with three extra env vars layered on top matching `provisionChildCell`'s `ExtraEnv`:
   ```go
   extra := map[string]string{
       "HIVE_MULTICELL":     cell.Parent,
       "HIVE_MULTICELL_DIR": filepath.Dir(cell.ClonePath), // parent dir of the child clone
       "HIVE_PROJECT":       cell.Project,
   }
   ```
   Child cells go through the same clone/ports/env/layout path as normal cells — they're first-class cells after the first-class-children refactor. The shared helper `resumeClonedCell(cell, extraEnv, summary)` handles both.

3. **Return** the populated `ResumeSummary`. `error` is only non-nil for the top-level gather failures in step 1.

### Note on `HIVE_REPO_DIR` and `ExpandTilde`

`provisionClone` sets `HIVE_REPO_DIR` to `opts.SourceRepo`, which comes from `DiscoveredProject.Path` (already absolute). On resume we don't have that — we only have the project name from the DB row. `ProjectConfig.RepoPath` is the closest equivalent, but it may start with `~`. `config.expandTilde` already exists as an unexported helper; export it as `ExpandTilde` (trivial rename — add a one-line exported wrapper if you'd rather not touch the helper's signature) and reuse it here. If the project config is missing `repo_path`, just skip setting `HIVE_REPO_DIR` — it's a nice-to-have for hook scripts, and we don't run hooks on resume.

### Error handling

- Top-level tmux/DB errors → return the wrapped error; no summary.
- Per-cell errors are captured in `FailedCell` and the loop continues.
- `layout.Apply` failures are logged via `slog.Warn` and do NOT move the cell from `Resumed` to `Failed` (the session itself came up — matches `Service.Create` semantics).

---

## Wiring: `cmd/start.go`

### Current flow

```
1. Check if "hive" tmux session exists
2. If not, create it running `hive dashboard`
3. Attach / switch-client to it
```

### New flow

```
1. Check if "hive" tmux session exists
2. If it does NOT exist:         ← NEW GATE
   a. Build cell.Service
   b. svc.ResumeAll(ctx)
   c. Print summary (if any)
3. If not, create the "hive" session
4. Attach / switch-client to it
```

The resume runs only when the dashboard session is missing. If the dashboard is already up, we assume this boot already ran resume, and we skip straight to attaching.

### Service construction

Both `cmd/start.go` (new call site) and `internal/tui/dashboard.go` (existing call site at `dashboard.go:73`) build a `cell.Service` from the `App`. Extract a helper on the `App` struct in `cmd/root.go`:

```go
func (a *App) NewCellService() *cell.Service {
    return &cell.Service{
        CellRepo:      a.CellRepo,
        CloneMgr:      a.CloneMgr,
        TmuxMgr:       a.TmuxMgr,
        HiveDir:       a.HiveDir,
        MulticellsDir: a.MulticellsDir,
        DB:            a.DB,
    }
}
```

`App` already holds every dependency the service needs (after the first-class-children refactor dropped `MulticellRepo`). `tui/dashboard.go`'s `NewModel` still builds its own service (different process), which is fine — the helper exists primarily so `start.go` doesn't have to know the field list.

### `cmd/start.go` diff shape

```go
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
        // NEW: resume cells before creating the dashboard session.
        svc := app.NewCellService()
        summary, err := svc.ResumeAll(ctx)
        if err != nil {
            slog.Warn("resume failed", "error", err)
            // Don't abort — the user should still get their dashboard.
        } else if summary != nil {
            if line := formatResumeSummary(summary); line != "" {
                fmt.Println(line)
            }
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
```

`formatResumeSummary` lives in `cmd/start.go` (small formatter, not worth its own package):

```go
func formatResumeSummary(s *cell.ResumeSummary) string {
    if len(s.Resumed) == 0 && len(s.Failed) == 0 {
        return ""
    }

    var b strings.Builder
    if len(s.Resumed) > 0 && len(s.Failed) == 0 {
        fmt.Fprintf(&b, "resumed %d cell%s: %s",
            len(s.Resumed), plural(len(s.Resumed)), strings.Join(s.Resumed, ", "))
    } else if len(s.Resumed) > 0 {
        fmt.Fprintf(&b, "resumed %d cell%s (%d failed): %s",
            len(s.Resumed), plural(len(s.Resumed)), len(s.Failed), strings.Join(s.Resumed, ", "))
    } else {
        fmt.Fprintf(&b, "resume: 0 succeeded, %d failed", len(s.Failed))
    }
    for _, f := range s.Failed {
        fmt.Fprintf(&b, "\n  %s: %v", f.Name, f.Error)
    }
    return b.String()
}
```

`Skipped` is not printed — it's informational and will usually be dominated by "already running" entries, which are uninteresting to a user running `hive start` after a reboot. It remains in the struct for logs / tests / a potential future `--verbose` flag.

---

## CLI output

Nothing to resume (fresh machine, empty DB):

```
$ hive start
# no resume line — attaches straight to dashboard
```

Happy path after a reboot:

```
$ hive start
resumed 3 cells: myapi-work, myapi-spike, scratch
# then dashboard attaches
```

Mixed success:

```
$ hive start
resumed 2 cells (1 failed): myapi-work, scratch
  myapi-spike: creating tmux session: duplicate session: myapi-spike
```

All failed:

```
$ hive start
resume: 0 succeeded, 2 failed
  myapi-work: creating tmux session: ...
  scratch:    creating tmux session: ...
```

Resume didn't need to run (dashboard already up — e.g. user typed `hive start` twice):

```
$ hive start
# silent, switch-client to hive session
```

Skipped cells (no clone dir etc.) are not shown on stdout. They're logged via `slog.Info` to `~/.hive/hive.log` so `hive logs` can show them if the user wonders why a cell wasn't resumed.

---

## Integration points

Summary of the numbered sequence inside `hive start`:

```
1. Find hive binary path             (existing)
2. Check "hive" tmux session exists  (existing)
3. If missing:
   a. Build cell.Service             ← NEW
   b. ResumeAll                      ← NEW
   c. Print summary                  ← NEW
   d. Create dashboard tmux session  (existing)
4. Attach/switch-client to it        (existing)
```

Files touched:

- `internal/cell/resume.go` — new.
- `internal/cell/service.go` — untouched.
- `internal/config/config.go` — export `expandTilde` as `ExpandTilde` (or add an exported thin wrapper).
- `cmd/root.go` — add `App.NewCellService()` helper; ensure `MulticellRepo` is wired up on `App` (verify; it's already used in `tui/dashboard.go` via `App`-equivalent fields).
- `cmd/start.go` — new resume call site + `formatResumeSummary` helper.

---

## Edge cases

- **Layout re-runs commands.** `layout.Apply` runs the configured pane commands via `send-keys`. After a reboot this will start (e.g.) `npm run dev` again, which is exactly the "restart my PC" behavior we want.
- **Port already bound by a non-hive process.** We don't check. `CreateSession` itself will succeed regardless; any pane command that tries to bind the port will fail visibly in that pane. Matches existing behavior and keeps resume simple.
- **Duplicate tmux session name.** Shouldn't happen because we've already confirmed the session is not in the tmux set — but we issue a defensive `KillSession` before `CreateSession` as belt-and-suspenders. Matches the pattern in `Service.Create`.
- **Headless cell with empty `ClonePath`.** `CreateHeadless` fills in `$HOME` when the caller passes an empty `Dir`, so `ClonePath` is never empty in the DB for live headless cells. If we somehow find one, fall back to `os.UserHomeDir()` and continue.
- **Dashboard already up → resume skipped.** Intentional. If a user wants to manually re-run resume later, they'd need to kill the `hive` session first (or we add a flag / standalone command — explicitly deferred).

---

## Implementation Order

### Step 1: Service method
- Add `internal/cell/resume.go` with `ResumeAll`, `ResumeSummary`, `SkippedCell`, `FailedCell`.
- Export `config.ExpandTilde` (or add a thin wrapper) so resume can resolve `~/...` in `RepoPath`.
- No behavioral change yet — nothing calls it.

### Step 2: App helper + start wiring
- Add `App.NewCellService()` in `cmd/root.go`.
- Update `cmd/start.go` to call `ResumeAll` when the dashboard session is missing, plus `formatResumeSummary`.

### Step 3: Manual verification
- Create 2 normal cells (one with a layout, one without) and 1 headless cell.
- `tmux kill-server`.
- `hive start` → expect a `resumed 3 cells` line, dashboard attached, all three cells reachable via `<prefix> f`, layout commands re-running in panes.
- If multicells are in use, repeat with a multicell: kill server, start, confirm the parent-dir session is back.

### Step 4 (optional, defer if not needed): Unit tests
- Table-driven test for `formatResumeSummary` covering empty / only-resumed / mixed / only-failed.
- The `ResumeAll` loop itself is mostly orchestration over real tmux and filesystem; a full test needs stubs for `TmuxMgr` and `CellRepo`. Worth doing only if this feature grows; skip for the initial version.
