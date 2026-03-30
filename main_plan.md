# Hive Implementation Plan

## Context
Hive is a CLI tool for spawning isolated, parallel dev environments using Git Worktrees, Docker Compose, and Caddy reverse proxy. The repo is greenfield — no code exists yet. This plan builds the tool incrementally across 6 phases, each independently testable.

## Decisions
- Go 1.24, module `github.com/lothar/hive`
- CLI: `spf13/cobra`
- State: SQLite via `modernc.org/sqlite` (pure Go, no CGO)
- YAML config: `gopkg.in/yaml.v3`
- Concurrency: `golang.org/x/sync/errgroup`
- Logging: `log/slog` (stdlib)
- Wraps `git`, `docker compose`, `tmux` via `os/exec` — no Docker SDK

## Project Structure

```
hive/
├── main.go
├── cmd/
│   ├── root.go          # App struct, persistent flags, DB init
│   ├── up.go            # hive up <name>
│   ├── down.go          # hive down <name>
│   ├── stop.go          # hive stop <name>
│   ├── join.go          # hive join <name>
│   ├── peek.go          # hive peek <name>
│   ├── status.go        # hive status
│   ├── swarm.go         # hive swarm <n1> <n2>...
│   ├── initproxy.go     # hive init-proxy
│   ├── notify.go        # hive notify <message>
│   ├── notifications.go # hive notifications
│   └── jump.go          # hive jump <cell>
├── internal/
│   ├── config/config.go       # .hive.yaml parsing
│   ├── state/
│   │   ├── db.go              # SQLite connection + migrations
│   │   ├── models.go          # Cell struct, CellStatus enum
│   │   └── repo.go            # CellRepository CRUD
│   ├── worktree/worktree.go   # Git worktree create/remove
│   ├── compose/
│   │   ├── compose.go         # Docker Compose up/stop/down
│   │   └── health.go          # Health check polling
│   ├── proxy/
│   │   ├── proxy.go           # Caddy container lifecycle + route management
│   │   └── network.go         # hive-net Docker network mgmt
│   ├── tmux/tmux.go           # Tmux session create/attach
│   ├── notify/
│   │   ├── notify.go          # Notification creation + DB ops
│   │   └── macos.go           # macOS native notifications via osascript
│   └── shell/exec.go          # Shared os/exec helpers
```

---

## Phase 1: Scaffold + CLI Skeleton + SQLite State

**Goal**: Compilable binary, all subcommands wired as stubs, working SQLite CRUD.

**Create**:
- `go.mod`, `main.go`
- `cmd/root.go` — `App` struct holding `*sql.DB`, `*CellRepository`, `*ProjectConfig`. `PersistentPreRunE` bootstraps `~/.hive/` and opens DB.
- `cmd/{up,down,stop,join,peek,status,swarm,initproxy,notify,notifications,jump}.go` — stubs printing "not implemented"
- `internal/state/models.go` — `Cell` struct with fields: Name, Project, Branch, WorktreePath, Status, Ports, CreatedAt, UpdatedAt. Status enum: provisioning/running/stopped/error.
- `internal/state/db.go` — `Open(path)` creates DB + runs schema migration (cells table with indexes).
- `internal/state/repo.go` — `CellRepository` with Create, GetByName, List, ListByStatus, UpdateStatus, Delete.
- `internal/config/config.go` — loads `.hive.yaml` (compose_path, seed_scripts, expose_port, env overrides).
- `internal/shell/exec.go` — `Run()` and `RunInDir()` helpers wrapping `os/exec`.

**Verify**: `go build`, `./hive --help` shows all commands, `go test ./internal/state/...` passes against `:memory:` SQLite.

---

## Phase 2: Git Worktree Management

**Goal**: `hive up` creates worktrees, `hive down` removes them.

**Create**:
- `internal/worktree/worktree.go` — `Manager` with `BaseDir` (default `~/workspaces`). Methods: `Create(repoDir, project, name, branch) → path`, `Remove(repoDir, path)`.
  - Create: auto-creates branch if missing, runs `git worktree add <path> <branch>`
  - Remove: `git worktree remove --force`, then `git worktree prune`

**Modify**:
- `cmd/up.go` — detect project name from git remote/dirname, create worktree, insert cell into DB as `running`.
- `cmd/down.go` — look up cell, remove worktree, delete from DB.
- `cmd/root.go` — add project detection (git rev-parse --show-toplevel).

**Verify**: `hive up test-feat` creates `~/workspaces/<project>/test-feat` as valid worktree. `hive down test-feat` cleans up worktree + DB row.

---

## Phase 3: Docker Compose Integration

**Goal**: `hive up` also starts Docker containers. `hive stop`/`hive down` manage container lifecycle.

**Create**:
- `internal/compose/compose.go` — `Manager` with `Up(workDir, composePath, projectName, env)`, `Stop(...)`, `Down(...)`, `PS(...)`.
- `internal/compose/health.go` — `HealthChecker` with `WaitHealthy()` — polls `docker compose ps` until healthy or 120s timeout.

**Modify**:
- `cmd/up.go` — after worktree creation: inject `.env` file, `compose.Up()`, wait healthy, run seed scripts. Add rollback on failure (cleanup stack pattern).
- `cmd/down.go` — `compose.Down()` before worktree removal.
- `cmd/stop.go` — implement: `compose.Stop()` + update status to `stopped`.
- `cmd/up.go` — detect if cell exists in `stopped` state → resume (compose up) instead of re-creating worktree.

**Verify**: `hive up` with a simple docker-compose.yml starts containers. `hive stop` stops them. `hive down` removes everything.

---

## Phase 4: Reverse Proxy (Caddy)

**Goal**: `hive init-proxy` starts global Caddy. `hive up` registers `<name>.dev.local` routes.

**Approach**: Use Caddy's admin API (`:2019`) to add/remove routes dynamically — cleaner than rewriting Caddyfiles.

**Create**:
- `internal/proxy/proxy.go` — `ProxyManager` with `Init()`, `IsRunning()`, `AddRoute(name, containerAddr)`, `RemoveRoute(name)`, `Stop()`.
- `internal/proxy/network.go` — create/manage `hive-net` Docker network; `ConnectNetwork(container)` / `DisconnectNetwork(container)`.
- Embedded Caddy docker-compose.yml via `//go:embed`.

**Modify**:
- `cmd/initproxy.go` — create `hive-net`, start Caddy container, print DNS setup instructions.
- `cmd/up.go` — after compose up: connect container to `hive-net`, add Caddy route for `<name>.dev.local` → `<container>:<expose_port>`.
- `cmd/down.go` — remove Caddy route, disconnect from `hive-net`.

**Verify**: `hive init-proxy`, then `hive up test` → `curl -H "Host: test.dev.local" localhost` reaches the container.

---

## Phase 5: Compound Commands

**Goal**: `swarm`, `status`, `peek`, `join`.

**Create**:
- `internal/tmux/tmux.go` — `SessionExists()`, `CreateSession(name, workDir)`, `AttachSession(name)` (uses `syscall.Exec` to replace process).

**Modify**:
- `cmd/swarm.go` — accept multiple names, spin up cells concurrently with `errgroup` (limit 3).
- `cmd/status.go` — query all cells, enrich with live `compose.PS()`, render table via `text/tabwriter`.
- `cmd/peek.go` — single cell detail: path, branch, containers, ports, URL. Optional `--logs` flag.
- `cmd/join.go` — look up cell, create tmux session if needed, attach.

**Verify**: Full integration walkthrough — swarm 3 cells, status shows all, peek one, join one, down all.

---

## Phase 6: Notifications

**Goal**: Agents (Claude Code etc.) can send notifications from inside a cell. Notifications are stored in SQLite, shown as macOS notifications, and allow quick jumping to the notifying cell.

**Commands**:
- `hive notify "Need API key"` — send notification (auto-detects cell from cwd matching a worktree path in DB)
- `hive notifications` — list recent notifications, filterable by `--cell` and `--unread`
- `hive jump <cell>` — mark notification read + attach to cell's tmux session (alias for peek + join)

**Create**:
- `internal/notify/notify.go` — `Notification` model (ID, CellName, Message, Read bool, CreatedAt). `NotificationRepository` with Create, List, MarkRead, MarkAllRead, DeleteForCell.
- `internal/notify/macos.go` — `SendMacOS(title, message, cellName)` using `osascript -e 'display notification ...'`. The notification title is the cell name, subtitle is "Hive", body is the message.
- `cmd/notify.go` — detect current cell from cwd (match against worktree paths in DB), create notification + fire macOS alert.
- `cmd/notifications.go` — list notifications as a table (cell, message, time, read/unread).
- `cmd/jump.go` — mark notification(s) read for that cell + attach tmux session.

**Schema addition** (in `state/db.go` migration):
```sql
CREATE TABLE IF NOT EXISTS notifications (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    cell_name TEXT NOT NULL,
    message   TEXT NOT NULL,
    read      BOOLEAN NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (cell_name) REFERENCES cells(name) ON DELETE CASCADE
);
```

**Cell detection from cwd**: When an agent runs `hive notify` from inside a worktree, Hive checks `os.Getwd()` against all `worktree_path` values in the cells table. Longest prefix match determines the cell.

**Verify**: From inside a cell's worktree, `hive notify "done"` fires a macOS notification and shows up in `hive notifications`. `hive jump <cell>` attaches to tmux.

---

## Cross-Cutting

- **Rollback on failure**: `hive up` uses a deferred cleanup stack — if compose fails, worktree is removed and DB row deleted.
- **Verbose mode**: `--verbose` / `-v` flag on root sets `slog` to debug level.
- **Project detection**: `git rev-parse --show-toplevel` in root command; error if not in a git repo (except `status`, `init-proxy`).
- **Embed**: Caddy compose template embedded in binary via `//go:embed`.
