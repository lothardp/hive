# Hive Code Walkthrough

A reading order that builds understanding bottom-up. Each layer only depends on the ones before it.

## Layer 1: The plumbing (no dependencies on other internal packages)

**`internal/shell/exec.go`** — 55 lines. This is the foundation everything else uses to run external commands (git, tmux). Read the whole thing. The key insight: non-zero exit codes come back as a `RunResult` with `ExitCode > 0`, not as Go errors. Actual errors mean the binary couldn't be found.

**`internal/state/models.go`** — The data types. `Cell` is the central struct. Notice it's simple: name, project, clone path, status, ports (JSON string), type. No methods, just data.

**`internal/state/db.go`** — How SQLite opens and creates tables. The `schema` const is the source of truth. `runMigrations` handles upgrading old DBs.

**`internal/state/repo.go`** — CRUD for cells. All raw SQL, no ORM. `scanCell`/`scanCells` at the bottom are the shared row-scanning helpers. Every public method follows the same pattern: build query → execute → scan → wrap errors.

**`internal/state/notification_repo.go`** — Same pattern, for notifications. You can skim this.

## Layer 2: Config (no dependencies on other internal packages)

**`internal/config/config.go`** — Two config types: `GlobalConfig` (where are my projects, where do clones go) and `ProjectConfig` (hooks, env, ports, layouts for one project). The loaders are straightforward YAML parsing. `DiscoverProjects` at line 186 is worth reading carefully — it's how the dashboard finds your repos by scanning `project_dirs` one level deep and checking for `.git`.

## Layer 3: The two core operations

**`internal/clone/clone.go`** — 50 lines. `Clone` runs `git clone`, `Remove` does `os.RemoveAll` with a safety check (refuses to delete outside `CellsDir`). That's it.

**`internal/tmux/tmux.go`** — Thin wrapper over tmux commands. `CreateSession` builds a `tmux new-session` command with `-e` flags for env vars. `JoinSession` is interesting: it uses `syscall.Exec` to *replace* the current process (not spawn a child), and picks `switch-client` vs `attach-session` based on whether you're already inside tmux.

## Layer 4: Supporting packages (used during cell creation)

These are all small and independent. Read in any order:

- **`internal/envars/envars.go`** — Merges port allocations + static env into one map. ~20 lines.
- **`internal/ports/ports.go`** — Allocates unique ports from 3001-9999. Checks DB for existing allocations, checks OS via `lsof`. Sequential scan until it finds a free one.
- **`internal/hooks/hooks.go`** — Runs shell commands sequentially via `sh -c`. Aborts on first failure. Writes results to `hook_results.txt`.
- **`internal/layout/layout.go`** — Translates the `Layout` config struct into tmux commands (rename window, split pane, send-keys).

## Layer 5: The orchestrator

**`internal/cell/service.go`** — This is the business logic. Read `Create` carefully. It's a multi-step pipeline with rollback:

```
clone repo → allocate ports → build env → create tmux session → run hooks → apply layout → save to DB
```

Each step has a rollback closure. If step 5 fails, it undoes steps 1-4. `Kill` is the reverse. `CreateHeadless` is the simple path (just tmux + DB, no clone).

## Layer 6: The TUI

Read in this order:

1. **`internal/tui/styles.go`** — Just lipgloss style definitions. Skim.
2. **`internal/tui/cells.go`** — The cells tab. `LoadCells` is the data-fetching command — it queries the DB, checks tmux session liveness, counts unread notifications, and discovers unmanaged tmux sessions. `View` renders the tree. `switchToSession` fires `tmux switch-client` without exiting the TUI.
3. **`internal/tui/create.go`** — The create flow. It's a state machine: `stepPickProject → stepEnterName → stepCreating → stepDone`. Each step handles its own key input.
4. **`internal/tui/projects.go`** and **`internal/tui/configtab.go`** — Simpler tabs. The key thing is `tea.ExecProcess` (used to open `$EDITOR`) — it suspends the TUI, runs the editor, resumes.
5. **`internal/tui/dashboard.go`** — The root model. Routes messages to the correct tab model. The important bit is the message routing in `Update`: data messages go to their owning tab regardless of which tab is active, key events go to the active tab.

## Layer 7: The CLI shell

**`cmd/root.go`** — The `App` struct and `PersistentPreRunE`. This is where everything gets wired together: open DB, load config, create managers. Every command gets these for free.

**`cmd/start.go`** — 12 lines of logic. Checks if the `hive` tmux session exists, creates it running `hive dashboard` if not, then joins.

**`cmd/dashboard.go`** — Just constructs the TUI model from `App` fields and runs `tea.NewProgram`.

**`cmd/install.go`** and **`cmd/notify.go`** — Straightforward, read last.

## How data flows

```
config.yaml ──→ App (root.go) ──→ cell.Service ──→ clone + tmux + hooks + ports + layout
                     │                                         │
                     ▼                                         ▼
                state.db ◄──────────────────────── CellRepository
                     │
                     ▼
                TUI (dashboard.go) ──→ tabs ──→ cell.Service
```

Config files are read-only input. SQLite tracks runtime state. The cell service orchestrates the actual work. The TUI calls the service and reads the DB.
