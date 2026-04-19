# Hive

Hive is a TUI-first CLI tool for spawning isolated, parallel development environments ("cells") using Git clones and tmux. The dashboard is the primary interface — the CLI exists only to launch it.

## Quick Reference

```
hive install               # One-time machine setup (config, dirs, tmux.conf)
hive start                 # Launch the dashboard (or attach if already running)
hive switch                # Fuzzy-find and switch to a cell (also <prefix> f in tmux)
hive notifications         # Ephemeral notifications picker (also <prefix> n in tmux)
hive health                # Show cell consistency across DB, disk, and tmux
hive notify <msg>          # Send a notification from inside a cell
hive notify --from-claude  # Read Claude Code hook JSON from stdin
hive logs [-f]             # Tail the Hive log file
```

Everything else (create cells, kill cells, navigate, configure projects) happens inside the dashboard TUI.

## Architecture

### Core Concepts

- **Dashboard**: The single "queen" — a dedicated tmux session (`hive`) running the TUI, rooted in `~`. Always the first session created, always accessible via `<leader>+.` from any cell.
- **Cell**: An isolated dev environment = git clone + tmux session + DB record. Four types: `normal` (has a clone), `headless` (tmux session only, no clone), `multi` (coordinator session for a multicell parent dir), and `multi_child` (one project's clone inside a multicell, with its own tmux session).
- **Cell Naming**: Normal cells are prefixed with the project name: `<project>-<name>` (e.g., `myapp-work`). Headless and multi cells use the bare name. Inside a multicell, child cells and their clone dirs are named `<project>-<multicell>` (e.g., `api-auth-overhaul`).
- **Multicell**: A named workspace that bundles clones of multiple projects under one parent dir in `multicells_dir`. Each project gets its own tmux session, plus a coordinator session rooted at the parent dir. Useful for features that span repos. Stored as a coordinator row in `cells` with `Type=multi`; each child is its own first-class cell row with `Type=multi_child` and `Parent=<multicell name>`.
- **Project Discovery**: Hive scans directories listed in `config.yaml` → `project_dirs` one level deep for git repos. No registration step needed.

### Tech Stack

- **Go** (module: `github.com/lothardp/hive`)
- **CLI**: `spf13/cobra`
- **State**: SQLite via `modernc.org/sqlite` (pure Go, no CGO)
- **Config**: `~/.hive/config.yaml` (global) + `~/.hive/config/{project}.yml` (per-project), parsed with `gopkg.in/yaml.v3`
- **Logging**: `log/slog` (stdlib)
- **TUI**: `charmbracelet/bubbletea`, `charmbracelet/lipgloss`, `charmbracelet/bubbles`
- **External tools**: `git`, `tmux`
- All external tools called via `os/exec`, no SDKs

### Project Layout

```
hive/
├── main.go                        # Entry point, calls cmd.Execute()
├── cmd/                           # Cobra commands
│   ├── root.go                    # App struct, PersistentPreRunE for DB/config init
│   ├── start.go                   # hive start — launch/attach to dashboard tmux session
│   ├── install.go                 # hive install — one-time machine bootstrap
│   ├── dashboard.go               # hive dashboard — run the Bubble Tea TUI (--tab for initial tab)
│   ├── notify.go                  # hive notify — send notification (supports --from-claude)
│   ├── notifications.go           # hive notifications — standalone notifs picker TUI
│   ├── switch.go                  # hive switch — fuzzy cell finder TUI
│   ├── health.go                  # hive health — cell consistency checker
│   └── logs.go                    # hive logs — tail ~/.hive/hive.log
├── internal/
│   ├── cell/
│   │   ├── service.go             # Cell lifecycle: Create, Kill, CreateHeadless, CreateMulti, List
│   │   └── resume.go              # ResumeAll (on hive start) + Recreate (single cell from dashboard)
│   ├── clone/clone.go             # Git clone create/remove, copies source remotes
│   ├── config/config.go           # GlobalConfig + ProjectConfig loaders, project discovery
│   ├── envars/envars.go           # Build env var map from ports + static env
│   ├── hooks/hooks.go             # Setup hook runner (abort-on-first-failure)
│   ├── keybindings/keybindings.go # Tmux keybinding generator (dashboard switch)
│   ├── layout/layout.go           # Tmux layout applier (windows + panes)
│   ├── ports/ports.go             # Port allocator (range 3001-9999, checks OS + DB)
│   ├── state/
│   │   ├── db.go                  # SQLite Open(), schema, migrations
│   │   ├── models.go              # Cell (with Parent), Notification structs; CellStatus, CellType enums
│   │   ├── cell_repo.go           # CellRepository CRUD (includes ListChildren by parent column)
│   │   └── notification_repo.go   # NotificationRepository CRUD
│   ├── tmux/tmux.go               # Tmux session create/attach/kill/list
│   ├── shell/exec.go              # os/exec helpers: Run(), RunInDir(), RunInDirWithEnv()
│   └── tui/                       # Bubble Tea TUI (multi-tab dashboard)
│       ├── dashboard.go           # Root model: tab switching, global keybinds, scrollable viewport
│       ├── cells.go               # Cells tab: list, navigate, kill
│       ├── projects.go            # Projects tab: list, fuzzy search (/), edit per-project config
│       ├── configtab.go           # Config tab: show/edit global config
│       ├── notifications.go       # Notifs tab: grouped by cell, mark read, jump to pane, clean up
│       ├── notifpicker.go         # Standalone notifs picker (used by hive notifications)
│       ├── create.go              # Create flow: project picker → name input → clone → navigate
│       ├── create_multi.go        # Multicell create flow: multi-picker → name → parent dir + N clones
│       ├── switcher.go            # Standalone fuzzy cell finder (used by hive switch)
│       └── styles.go              # Shared lipgloss styles
```

### How It Fits Together

1. `cmd/root.go` defines an `App` struct that holds DB, CellRepository, NotificationRepository, GlobalConfig, CloneManager, TmuxManager, and the resolved `MulticellsDir`. `PersistentPreRunE` initializes everything; `PersistentPostRunE` closes the DB.
2. `hive start` creates a tmux session named `hive` running `hive dashboard`, then attaches to it. On fresh launches (no existing `hive` session), it first calls `cell.Service.ResumeAll` to recreate tmux sessions for any DB-registered cells that are absent from tmux (e.g., after a reboot or `tmux kill-server`).
3. The dashboard TUI is the primary interface. It uses `internal/cell.Service` for cell lifecycle operations.
4. `cell.Service` orchestrates: clone repo → allocate ports → build env → run hooks → create tmux session → apply layout → record in DB. Hooks run before tmux session creation via the internal `provisionClone` helper, so hook failures tear down the clone cleanly without a half-configured session. Rollback on failure at any step. For multicells, `CreateMulti` calls `provisionChildCell` once per selected project — each child ends up with its own clone, ports, tmux session, layout, and DB row.
5. Navigation between cells uses `tmux switch-client` from within the TUI — the dashboard never exits.
6. State lives in `~/.hive/state.db` (SQLite). Two tables: `cells` (with a `parent` self-FK column linking multicell children to their coordinator; `ON DELETE CASCADE` removes children when the coordinator is deleted) and `notifications`.
7. Config lives in YAML files: `~/.hive/config.yaml` (global) and `~/.hive/config/{project}.yml` (per-project).

## Config

### Global (`~/.hive/config.yaml`)

```yaml
project_dirs:                    # Directories to scan for git repos (one level deep)
  - ~/side_projects
  - ~/work
cells_dir: ~/hive/cells          # Where cell clones are stored
multicells_dir: ~/hive/multicells # Where multicell parent dirs are stored
editor: vim                      # Editor for config editing from dashboard
tmux_leader: "C-a"               # Tmux leader key
```

### Per-Project (`~/.hive/config/{project}.yml`)

```yaml
repo_path: ~/side_projects/my-api  # Resolved automatically from discovery
hooks:                             # Run after cloning
  - npm install
  - npm run db:migrate
env:                               # Injected into tmux session
  NODE_ENV: development
port_vars:                         # Auto-assigned unique ports
  - PORT
  - DB_PORT
layouts:
  default:                         # Auto-applied on cell creation
    windows:
      - name: code
        panes:
          - command: nvim .
      - name: server
        panes:
          - command: npm run dev
```

## Patterns & Conventions

### Command Structure

Each command is a file in `cmd/` with a package-level `*cobra.Command` var and an `init()` that registers it with `rootCmd`. Commands use `RunE` (not `Run`) to propagate errors.

### Error Handling

- Wrap errors with context: `fmt.Errorf("cloning repo: %w", err)`
- Multi-step operations use rollback-on-failure: if step 3 fails, undo steps 1-2
- Shell command results check `ExitCode` explicitly (not just error)

### State Management

- All repo methods take `context.Context` as first arg
- Cell status: `running`, `stopped`, `error`
- Cell types: `normal`, `headless`, `multi`, `multi_child`
- Timestamps are managed by the repo layer, not callers

### Tmux Integration

- The dashboard is a tmux session named `hive` running the TUI
- Navigation uses `tmux switch-client` — the dashboard keeps running
- From any cell, `<prefix> .` switches back to the dashboard
- `<prefix> f` opens the fuzzy cell switcher in a new tmux window
- `<prefix> n` opens the ephemeral notifications picker in a new tmux window
- `hive start` handles attach vs switch-client based on `$TMUX`
- Clones copy remotes from the source repo (so `origin` points to GitHub, not the local path)

### Shell Execution

- `shell.Run(ctx, name, args...)` — run in current dir
- `shell.RunInDir(ctx, dir, name, args...)` — run in specific dir
- `shell.RunInDirWithEnv(ctx, dir, env, name, args...)` — run with extra env vars
- All return `RunResult{Stdout, Stderr, ExitCode}`

### Cell Service (`internal/cell`)

All cell lifecycle logic is in one package. `service.go` handles creation/teardown; `resume.go` handles rehydrating tmux sessions from DB state.
- `Create(ctx, CreateOpts)` — clone + ports + hooks + tmux + layout + DB (normal cells); `CreateOpts.OnProgress` surfaces short step labels for live UI feedback
- `CreateHeadless(ctx, HeadlessOpts)` — tmux session + DB (supports custom dir/project)
- `CreateMulti(ctx, MultiOpts)` — parent dir + coordinator tmux session + `cells` row (type=multi) + N first-class child cells (each with its own clone, ports, hooks, tmux session, layout, and type=multi_child DB row with `parent` set to the coordinator)
- `Kill(ctx, cellName)` — tmux kill + rm clone or multicell dir (based on type) + DB delete. For `multi` cells, also kills each child's tmux session explicitly; child rows are removed by FK cascade. For `multi_child` cells, only that child's clone dir (under the multicells dir) is removed; the parent dir and siblings are left alone.
- `Recreate(ctx, name)` — rehydrates a single cell's tmux session using stored ports, skipping hooks, re-applying the default layout. Safe whether the session is dead or alive (existing session is killed first).
- `ResumeAll(ctx)` — walks all DB cells, skipping any whose tmux session is already alive, and calls the per-type resume helper (`resumeNormal`, `resumeMultiChild`, `resumeHeadless`, `resumeMulti`). Returns a `ResumeSummary` (resumed / skipped / failed). Called by `hive start` on fresh launches.
- `List(ctx)` — all cells from DB
- `ListMultiChildren(ctx, multicellName)` — child cells whose `parent` column equals the multicell name
- Internal `provisionClone(ctx, ProvisionOpts)` — shared orphan-cleanup → clone → alloc ports → build env → run hooks, with rollback on failure. Used by both `Create` (once) and `provisionChildCell` (once per project).
- Internal `provisionChildCell(ctx, ProvisionChildOpts)` — provisions one multicell child end-to-end: `provisionClone` + kill orphan tmux + create session + apply layout + insert `multi_child` row. On failure rolls back only that child; does not touch the parent dir or siblings.

### Setup Hooks

- Defined in per-project config (`~/.hive/config/{project}.yml`)
- Run sequentially in the cell's clone directory, before the tmux session is created
- Abort on first failure — the partially-cloned directory is rolled back (no hook_results.txt is written; stderr from the failing hook is surfaced up the error chain)
- Hook commands receive the cell's full environment (ports, static env, `HIVE_CELL`, `HIVE_REPO_DIR`)
- For multicells, hooks run once per child project in its own clone dir, with extra env `HIVE_MULTICELL`, `HIVE_MULTICELL_DIR`, and `HIVE_PROJECT`

### Port Allocation

- Configured via `ProjectConfig.PortVars`
- `ports.Allocator` assigns unique ports from range 3001-9999, checking both the DB and OS (`lsof`) for conflicts
- Allocated ports are stored as JSON in `cells.ports` and injected as env vars into the tmux session

### Layouts

- Defined in per-project config under `layouts.default`
- Auto-applied on cell creation
- Layouts define tmux windows and panes with optional commands and split directions

## Build & Test

```bash
go build -o hive .          # Build
go test ./...               # All tests
```
