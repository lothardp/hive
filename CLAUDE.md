# Hive

Hive is a CLI tool for spawning isolated, parallel development environments ("cells") using Git Worktrees and tmux. It's designed to let developers (and AI agents like Claude Code) work on multiple feature branches simultaneously without conflicts.

## Quick Reference

```
hive cell <name> [-b branch]   # Create a new cell (worktree + tmux session)
hive join <name>               # Attach to a cell's tmux session
hive switch                    # fzf picker to switch between cells
hive status                    # List all cells with status
hive kill <name>               # Destroy cell (worktree + tmux + DB record)
```

## Architecture

### Core Concepts

- **Cell**: An isolated dev environment = git worktree + tmux session + DB record. Each cell gets its own filesystem, branch, and terminal session.
- **Queen Session** (planned): A read-only session on the default branch for exploring the repo without creating a feature branch.
- **Headless Cell** (planned): A tmux session in an arbitrary directory, no git worktree attached.

### Tech Stack

- **Go** (module: `github.com/lothardp/hive`)
- **CLI**: `spf13/cobra`
- **State**: SQLite via `modernc.org/sqlite` (pure Go, no CGO)
- **Config**: `.hive.yaml` per repo, parsed with `gopkg.in/yaml.v3`
- **Logging**: `log/slog` (stdlib)
- **External tools**: `git`, `tmux`, `fzf` (for switch), `docker compose` (future)
- All external tools are called via `os/exec`, no SDKs

### Project Layout

```
hive/
├── main.go                        # Entry point, calls cmd.Execute()
├── cmd/                           # Cobra commands, one file per command
│   ├── root.go                    # App struct, PersistentPreRunE for DB/config init
│   ├── cell.go                    # hive cell — create worktree + tmux + DB
│   ├── join.go                    # hive join — attach tmux session
│   ├── switch.go                  # hive switch — fzf-based cell picker
│   ├── status.go                  # hive status — table of all cells
│   ├── kill.go                    # hive kill — full teardown
│   └── [up, down, stop, etc.]     # Stubs for future phases
├── internal/
│   ├── config/config.go           # .hive.yaml loader (ProjectConfig)
│   ├── state/
│   │   ├── db.go                  # SQLite Open(), schema migrations
│   │   ├── models.go              # Cell struct, CellStatus enum
│   │   └── repo.go                # CellRepository CRUD
│   ├── worktree/worktree.go       # Git worktree create/remove, project detection
│   ├── tmux/tmux.go               # Tmux session create/attach/kill
│   └── shell/exec.go              # os/exec helpers: Run(), RunInDir()
```

### How It Fits Together

1. `cmd/root.go` defines an `App` struct that holds DB, repository, config, worktree manager, and tmux manager. `PersistentPreRunE` initializes everything; `PersistentPostRunE` closes the DB.
2. Commands access the app context via `cmd.Context()`.
3. All git/tmux/docker operations go through `internal/shell` exec helpers — never called directly from commands.
4. State lives in `~/.hive/hive.db` (SQLite). Two tables: `cells` and `notifications`.

## Patterns & Conventions

### Command Structure

Each command is a file in `cmd/` with a package-level `*cobra.Command` var and an `init()` that registers it with `rootCmd`. Commands use `RunE` (not `Run`) to propagate errors.

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
- Both return `Result{Stdout, Stderr, ExitCode}`

## Build & Test

```bash
go build -o hive .          # Build
go test ./internal/state/... # Test state layer (uses :memory: SQLite)
go test ./internal/worktree/... # Test worktree (needs git)
go test ./...               # All tests
```

## Current State & Roadmap

### Working Now
- Cell creation (`hive cell`) — worktree + tmux + DB
- Cell joining (`hive join`) and switching (`hive switch` via fzf)
- Cell status listing (`hive status`)
- Cell destruction (`hive kill`)
- SQLite state with full CRUD
- `.hive.yaml` config loading

### Next Up (in priority order)
1. `hive install` — one-time bootstrap (DB, tmux integration, projects dir)
2. `hive setup` — guided repo registration with optional agent analysis
3. Queen session auto-creation on first cell for a repo
4. Headless cells (tmux session in any directory, no worktree)
5. Setup hooks (per-repo scripts on cell creation: copy node_modules, .env, run installs)
6. Port allocation and env var injection per cell (eliminates port conflicts)
7. `hive up` / `hive down` / `hive stop` — start/stop project services
8. Caddy reverse proxy (`<name>.dev.local` routing)
9. Tmux keybindings for Hive commands
10. TUI dashboard (Bubble Tea) as default `hive` command
11. Notifications system (agents notify from inside cells)
12. Background cron tasks (periodic git fetch)

### Networking Strategy
Port allocation comes first — Hive assigns unique ports per cell via env vars, no containers needed. Caddy reverse proxy (`<name>.dev.local`) is a later addition for projects that want URL-based routing via Docker.
