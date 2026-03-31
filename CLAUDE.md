# Hive

Hive is a CLI tool for spawning isolated, parallel development environments ("cells") using Git Worktrees and tmux. It's designed to let developers (and AI agents like Claude Code) work on multiple feature branches simultaneously without conflicts.

## Quick Reference

```
hive install                   # One-time machine setup (DB, tmux, directories)
hive setup                     # Register current repo with Hive (interactive)
hive cell <name> [-b branch]   # Create a new cell (worktree + tmux session)
hive cell <name> --headless [dir]  # Create a headless cell (tmux only, no worktree)
hive join <name>               # Attach to a cell's tmux session
hive switch                    # fzf picker to switch between cells
hive status                    # List all cells with status
hive kill <name>               # Destroy cell (worktree + tmux + DB record)
hive dashboard                 # Interactive TUI overview (Bubble Tea)
hive config show               # Show effective config for current repo
hive config export [-f file]   # Export repo config to YAML
hive config import [-f file]   # Import repo config from YAML
hive config apply [-f file] [--global]  # Merge YAML into repo or global config
hive keybindings [--direct]    # Regenerate tmux keybindings
hive notify <cell> -m <msg>    # Send a notification to a cell
hive notifications             # List recent notifications
hive logs                      # Tail the Hive log file
```

## Architecture

### Core Concepts

- **Cell**: An isolated dev environment = git worktree + tmux session + DB record. Each cell gets its own filesystem, branch, and terminal session. Three types: `normal`, `queen`, `headless`.
- **Queen Session**: Auto-created on first `hive cell` for a project. Uses the repo's original directory (not a worktree) on the default branch. Protected: cannot be killed while other cells exist for the project. Branch integrity is verified on every Hive command.
- **Headless Cell**: A tmux session in an arbitrary directory, no git worktree attached. Created with `hive cell <name> --headless [dir]`.
- **Cell Naming**: All cell names are auto-prefixed with the project name (e.g., `hive cell foo` in project `myapp` → cell `myapp-foo`). Override with `cell_prefix` in config. Headless cells are never prefixed.

### Tech Stack

- **Go** (module: `github.com/lothardp/hive`)
- **CLI**: `spf13/cobra`
- **State**: SQLite via `modernc.org/sqlite` (pure Go, no CGO)
- **Config**: `.hive.yaml` per repo, parsed with `gopkg.in/yaml.v3`
- **Logging**: `log/slog` (stdlib)
- **TUI**: `charmbracelet/bubbletea`, `charmbracelet/lipgloss`, `charmbracelet/bubbles`
- **External tools**: `git`, `tmux`, `fzf` (for switch), `docker compose` (future)
- All external tools are called via `os/exec`, no SDKs

### Project Layout

```
hive/
├── main.go                        # Entry point, calls cmd.Execute()
├── cmd/                           # Cobra commands, one file per command
│   ├── root.go                    # App struct, PersistentPreRunE for DB/config init
│   ├── cell.go                    # hive cell — create worktree + tmux + DB (+ queen + headless)
│   ├── completion.go              # Shell completion helpers (cell name completion)
│   ├── install.go                 # hive install — one-time machine bootstrap
│   ├── setup.go                   # hive setup — interactive repo registration
│   ├── config.go                  # hive config — show/export/import/apply subcommands
│   ├── join.go                    # hive join — attach tmux session
│   ├── switch.go                  # hive switch — fzf-based cell picker
│   ├── status.go                  # hive status — table of all cells
│   ├── kill.go                    # hive kill — full teardown (+ queen/headless paths)
│   ├── dashboard.go               # hive dashboard — Bubble Tea TUI
│   ├── keybindings.go             # hive keybindings — regenerate tmux keybindings
│   ├── notify.go                  # hive notify — send notifications to cells
│   ├── notifications.go           # hive notifications — list/view notifications
│   ├── logs.go                    # hive logs — tail ~/.hive/hive.log
│   └── [up, down, stop, etc.]     # Stubs for future phases
├── internal/
│   ├── config/
│   │   ├── config.go              # .hive.yaml loader, ProjectConfig, JSON/YAML serialization
│   │   └── merge.go               # Config merge (upsert semantics for apply)
│   ├── envars/envars.go           # Build env var map from ports + static env
│   ├── hooks/hooks.go             # Setup hook runner (abort-on-first-failure)
│   ├── layout/layout.go           # Tmux layout applier (windows + panes)
│   ├── ports/ports.go             # Port allocator (range 3001-9999, checks OS + DB)
│   ├── state/
│   │   ├── db.go                  # SQLite Open(), schema, migrations
│   │   ├── models.go              # Cell, Repo structs; CellStatus, CellType enums
│   │   ├── repo.go                # CellRepository CRUD (+ GetQueen, CountByProject, UpdatePorts)
│   │   ├── config_repo.go         # ConfigRepository — global_config key/value store
│   │   └── repo_repo.go           # RepoRepository — registered repos CRUD
│   ├── keybindings/keybindings.go  # Tmux keybinding generator (table or direct mode)
│   ├── notify/notify.go           # Notification sender (stub for future)
│   ├── tui/dashboard.go           # Bubble Tea dashboard model (tree view, actions)
│   ├── worktree/worktree.go       # Git worktree create/remove/branch delete, project detection
│   ├── tmux/tmux.go               # Tmux session create/attach/kill/list (env vars via -e flags)
│   └── shell/exec.go              # os/exec helpers: Run(), RunInDir(), RunInDirWithEnv()
```

### How It Fits Together

1. `cmd/root.go` defines an `App` struct that holds DB, three repositories (CellRepository, ConfigRepository, RepoRepository), config, worktree manager, and tmux manager. `PersistentPreRunE` initializes everything and verifies queen branch integrity; `PersistentPostRunE` closes the DB.
2. Commands access the app context via `cmd.Context()`.
3. All git/tmux/docker operations go through `internal/shell` exec helpers — never called directly from commands.
4. State lives in `~/.hive/state.db` (SQLite). Four tables: `cells`, `notifications`, `global_config`, `repos`.
5. Config is DB-first: `PersistentPreRunE` looks up the registered repo in the `repos` table and loads its JSON config. Falls back to `.hive.yaml` if the repo isn't registered.

## Patterns & Conventions

### Command Structure

Each command is a file in `cmd/` with a package-level `*cobra.Command` var and an `init()` that registers it with `rootCmd`. Commands use `RunE` (not `Run`) to propagate errors. Commands that take a cell name argument use `ValidArgsFunction: completeCellNames` for shell completion.

### Error Handling

- Wrap errors with context: `fmt.Errorf("creating worktree: %w", err)`
- Multi-step operations use rollback-on-failure: if step 3 fails, undo steps 1-2
- Shell command results check `ExitCode` explicitly (not just error)

### State Management

- All repo methods take `context.Context` as first arg
- Cell status transitions: `provisioning` -> `running` -> `stopped` -> `error`
- Timestamps are managed by the repo layer, not callers

### Tmux Integration

- Hive owns tmux. Sessions are created/managed only through Hive, never directly.
- `join` uses `syscall.Exec` to replace the current process (keeps terminal responsive)
- Inside tmux: `switch-client`. Outside: `attach-session`.

### Shell Execution

- `shell.Run(ctx, name, args...)` — run in current dir
- `shell.RunInDir(ctx, dir, name, args...)` — run in specific dir
- `shell.RunInDirWithEnv(ctx, dir, env, name, args...)` — run with extra env vars
- All return `RunResult{Stdout, Stderr, ExitCode}`

### Setup Hooks

- Defined as a list of shell commands in `ProjectConfig.Hooks`
- Run sequentially in the cell's worktree directory on `hive cell`
- Abort on first failure — results written to `hook_results.txt` in the worktree
- Hook commands receive the cell's full environment (ports, static env, `HIVE_CELL`, `HIVE_QUEEN_DIR`)

### Port Allocation

- Configured via `ProjectConfig.PortVars` — a list of env var names (e.g., `["PORT", "DB_PORT"]`)
- `ports.Allocator` assigns unique ports from range 3001-9999, checking both the DB and OS (`lsof`) for conflicts
- Allocated ports are stored as JSON in `cells.ports` and injected as env vars into the tmux session

### Layouts

- Defined in `ProjectConfig.Layouts` (repo-level) or `global_config` (global-level, via `hive config apply --global`)
- A `"default"` layout is auto-applied on cell creation
- Layouts define tmux windows and panes with optional commands and split directions

## Build & Test

```bash
go build -o hive .          # Build
go test ./internal/state/... # Test state layer (uses :memory: SQLite)
go test ./internal/worktree/... # Test worktree (needs git)
go test ./...               # All tests
```

## Current State & Roadmap

### Working Now
- `hive install` — one-time machine bootstrap (cells dir, projects dir, tmux.conf)
- `hive setup` — interactive repo registration (project name, remote, default branch, config)
- `hive config` — show/export/import/apply config with DB-first approach
- Cell creation (`hive cell`) — worktree + tmux + DB + port allocation + hooks + layout
- Queen session auto-creation on first `hive cell` for a project
- Headless cells (`hive cell --headless`) — tmux session in any directory
- Cell joining (`hive join`) and switching (`hive switch` via fzf)
- Cell status listing (`hive status`) with cell type labels, port display, age
- Cell destruction (`hive kill`) with branch cleanup, queen/headless-aware paths
- Setup hooks (abort-on-first-failure, env var passthrough)
- Port allocation (3001-9999 range, DB + OS conflict checking)
- Tmux layouts (windows, panes, auto-apply "default" layout)
- Env var injection (port vars + static env + `HIVE_CELL` + `HIVE_QUEEN_DIR`)
- SQLite state with four tables: `cells`, `notifications`, `global_config`, `repos`
- Config merge via `hive config apply` (upsert semantics)
- Cell name prefixing (project name or `cell_prefix` config; headless cells excluded)
- Shell completion for commands and cell names (`hive completion bash/zsh/fish`)
- Tmux keybindings (`hive keybindings`) — table mode or direct mode, leader key configurable
- Notifications (`hive notify`, `hive notifications`) — agents send notifications from inside cells
- TUI dashboard (`hive dashboard`) — Bubble Tea tree view with switch, kill, notification actions
- File logging to `~/.hive/hive.log` with `hive logs` to tail it
- Graceful kill for orphaned cells (tmux session lost but DB record remains)

### Next Up (in priority order)
1. Background cron tasks (periodic git fetch)
2. `hive up` / `hive down` / `hive stop` — start/stop project services (deferred, needs rethinking)
3. Caddy reverse proxy (`<name>.dev.local` routing) (deferred, needs rethinking)

### Networking Strategy
Port allocation comes first — Hive assigns unique ports per cell via env vars, no containers needed. Caddy reverse proxy (`<name>.dev.local`) is a later addition for projects that want URL-based routing via Docker.
