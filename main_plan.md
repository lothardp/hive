# Hive — Build Plan

What to build, in what order. Each phase builds on the previous one and is independently useful.

---

## Phase 1: Core Cell Lifecycle ✅

The basics — create, join, switch between, and destroy cells.

- `hive cell <name>` — create worktree + tmux session + DB record
- `hive join <name>` — attach to a cell's tmux session
- `hive switch` — fzf picker to switch between cells
- `hive status` — list all cells
- `hive kill <name>` — destroy worktree, tmux session, and DB record
- SQLite state layer with full CRUD
- `.hive.yaml` config loading (temporary — will move to DB)
- Rollback on failure (if tmux fails, clean up worktree and DB)

---

## Phase 2: Installation & Repo Registration

Make Hive properly installable and let repos declare how they work.

### `hive install`
- Create `~/.hive/` directory and SQLite DB
- Generate `~/.hive/tmux.conf` and prompt user to source it
- Prompt for main projects directory (e.g., `~/side_projects`)
- Optionally set up cron jobs for background tasks

### `hive setup`
- Register a repo with Hive (run from inside the repo)
- Store per-repo config in DB: layout, hooks, port vars, env vars
- Optionally spawn a guided agent to analyze the project
- Warn if codebase needs changes to work with isolated worktrees

### Config in DB
- All config lives in SQLite, not YAML files
- `hive config export` — dump to YAML for sharing/version control
- `hive config import` — load from YAML back into DB

---

## Phase 3: Setup Hooks & Cell Layout

Make cells useful out of the box — run setup scripts and open the right tabs.

### Setup Hooks
Per-repo scripts that run when a new cell is created (configured during `hive setup`):
- Copy or symlink `node_modules` from queen worktree
- Copy `.env.local` or other gitignored files
- Run install commands (`npm install`, `bundle install`, etc.)
- Symlink shared build caches

### Cell Layout
Per-repo tmux layout — tabs, panes, and startup commands:
- Each tab becomes a tmux window (e.g., editor, server, tests, shell)
- Tabs can have up to two panes with a horizontal or vertical split
- Each pane can run a command or be a plain shell
- No layout configured = current behavior (one tab, plain shell)

---

## Phase 4: Port Allocation & Environment Variables

Eliminate port conflicts between parallel cells.

- Repo config declares which env vars hold port numbers (e.g., `PORT`, `DB_PORT`)
- Hive allocates unused ports per cell and injects them as env vars
- Additional static env vars configurable per repo (e.g., `MY_HOST=localhost`)
- Ports are released when cells are killed

---

## Phase 5: Queen Sessions & Headless Cells

Two new cell types beyond normal cells.

### Queen Sessions
- Auto-created when you create your first cell for a repo
- Read-only-ish worktree parked on the default branch
- Clean environment for exploring the repo without a feature branch

### Headless Cells
- `hive cell --headless <name> [dir]` — tmux session in any directory
- No git, no worktree — just a quick scratch workspace
- Still tracked in DB and shows up in `hive status` / `hive switch`

---

## Phase 6: Tmux Integration

Deeper tmux integration so you never need to leave the keyboard.

### Keybindings
Custom tmux keybindings defined in `~/.hive/tmux.conf`:
- Switch between cells (fzf picker)
- Quick-create a new cell
- Kill the current cell
- Show cell status

---

## Phase 7: Notifications

Let agents communicate from inside cells.

- `hive notify "message"` — auto-detects cell from cwd, stores in DB, fires macOS notification
- `hive notifications` — list recent notifications, filterable by cell and read/unread
- `hive jump <cell>` — mark notifications read + attach to cell's tmux session

---

## Phase 8: TUI Dashboard

Interactive terminal UI as the default `hive` command (Bubble Tea).

- See all active cells with status, branch, project
- Navigate and switch into cells
- Create and kill cells
- View agent notifications
- Replaces `hive status` as the primary overview

---

## Phase 9: Background Tasks & Polish

- Cron-based `git fetch` across registered repos (configurable per repo)
- `hive swarm <n1> <n2>...` — batch cell creation with concurrency limit
- `hive peek <name>` — detailed cell info (path, branch, ports, containers)

---

## Future

- **Multicell**: group multiple repos under a single feature (e.g., server + web + mobile). Deferred until normal cells are solid.
- **Web Dashboard** (`hive serve`): browser-based GUI for monitoring cells. Same SQLite data layer. Deferred until TUI is stable.
