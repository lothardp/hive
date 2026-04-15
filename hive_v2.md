# Hive v2 — Design Document

## Philosophy

Hive v1 was CLI-first: many commands, each doing one thing. Hive v2 flips this. The TUI dashboard is the primary interface. The CLI exists only to launch it.

Three main actions:
1. **Start** — boot Hive (one CLI command)
2. **Create cell** — spin up an isolated environment (from the dashboard)
3. **Navigate** — switch between cells (from the dashboard)

Everything else — killing cells, configuring projects, viewing notifications — happens inside the dashboard.

---

## Core Concepts

### The Dashboard IS the Queen

In v1, the "queen" was one-per-project — a protected cell on the default branch, needed because worktrees required a main checkout to anchor off of. v2 drops that entirely. There's now a single queen: the dashboard itself. It's a dedicated tmux session (`hive`) running the TUI, rooted in `~`. It's always the first session created and the last one standing.

When you press a tmux keybind from any cell, you navigate back to the dashboard. From there you can see everything, create cells, kill cells, or jump to another cell.

### Cells Are Clones, Not Worktrees

v1 used git worktrees. They share the object store, which causes problems: shared stashes, index locks, can't independently nuke `node_modules`, can't run `git clean` freely.

v2 uses full `git clone`. Each cell gets a completely independent copy of the repo. Heavier on disk, but truly isolated. No surprises.

Cell directory structure:
```
~/hive/cells/
├── myapp/
│   ├── work/                  # clone of myapp
│   └── experiments/           # another clone of myapp
├── api/
│   └── refactor/              # clone of api
```

Cell names are just identifiers — they have no relation to git branches. The user manages branches inside each clone however they want.

### Configuration Is a File, Not a Database

v1 stored config in SQLite (`global_config` table, `repos.config` column). v2 uses files:

- **Global config**: `~/.hive/config.yaml` — project directories, cells directory, editor, tmux leader
- **Per-project config**: `~/.hive/config/{project}.yml` — hooks, env vars, ports, layouts for a specific project

Both are editable from the dashboard via `$EDITOR`. The per-project config is personal — it lives outside the repo, so it's not shared or synced with a team. Each developer configures their own hooks and layouts.

---

## Global Config

`~/.hive/config.yaml`:

```yaml
# Directories to scan for projects. Hive looks one level deep for git repos.
project_dirs:
  - ~/side_projects
  - ~/work
  - ~/repos

# Where cell clones are stored.
cells_dir: ~/hive/cells

# Editor used when pressing 'e' in the dashboard.
editor: vim

# Tmux leader key for Hive keybindings.
tmux_leader: "C-a"
```

That's the entire global config. No database for settings. SQLite still tracks cell state (active cells, ports, notifications) — that's runtime data, not configuration.

### Project Discovery

When the dashboard needs to show projects (for cell creation), it scans each `project_dirs` entry one level deep:

```
~/side_projects/
├── hive/          ← git repo → project "hive"
├── my-api/        ← git repo → project "my-api"
└── notes/         ← not a git repo → ignored
```

No registration step. If it's a git repo inside a project directory, Hive knows about it.

---

## Per-Project Config

Each project gets its own config file at `~/.hive/config/{project}.yml`:

```yaml
# Path to the source repo (resolved automatically from project_dirs)
repo_path: ~/side_projects/my-api

# Shell commands to run after cloning (install deps, seed DB, etc.)
hooks:
  - npm install
  - npm run db:migrate

# Environment variables injected into the cell's tmux session.
env:
  NODE_ENV: development
  LOG_LEVEL: debug

# Env var names that get auto-assigned unique ports.
port_vars:
  - PORT
  - DB_PORT

# Tmux layout applied on cell creation.
layouts:
  default:
    windows:
      - name: code
        panes:
          - command: nvim .
      - name: server
        panes:
          - command: npm run dev
      - name: shell
        panes: []
```

This file is personal — it lives outside the repo in `~/.hive/config/`, not in the repo itself. Each developer customizes their own hooks, layouts, and env vars to taste. No syncing, no team conventions to fight over.

The dashboard's Projects tab lists discovered projects and lets you open their config in `$EDITOR`. If the config file doesn't exist yet, Hive creates a default one.

---

## The Three Actions

### 1. `hive start`

The only CLI command that matters. Everything else happens inside the TUI.

**What it does:**
1. If the `hive` tmux session exists → attach to it (or switch-client if already in tmux)
2. If it doesn't exist → create the tmux session, launch the dashboard TUI inside it, attach

**Behavior matrix:**

| Context | Action |
|---|---|
| No tmux running | Create `hive` session → run dashboard → `tmux attach` |
| In tmux, `hive` session exists | `tmux switch-client -t hive` |
| In tmux, `hive` session gone | Recreate `hive` session → run dashboard → `tmux switch-client` |
| Outside tmux, `hive` session exists | `tmux attach -t hive` |

The dashboard session is named `hive`. Its working directory is `~`. The tmux session runs the `hive dashboard` binary as its main process — if the dashboard exits, the session dies, and `hive start` recreates it.

### 2. Create Cell (from dashboard)

Triggered by pressing a keybind (e.g., `c`) in the dashboard. The flow:

1. **Pick a project** — fuzzy finder over discovered projects (scanned from `project_dirs`)
2. **Enter cell name** — text input, e.g., `feature-auth`
3. **Hive creates the cell:**
   a. Determine cell name: `<project>-<name>` (e.g., `myapp-work`)
   b. `git clone <repo-path> ~/hive/cells/<project>/<name>`
   c. Read project config from `~/.hive/config/<project>.yml`
   d. Allocate ports (if `port_vars` configured)
   e. Create tmux session with env vars (`HIVE_CELL`, ports, static env)
   f. Run hooks in the clone directory (sequentially, abort on failure)
   g. Apply layout (if `default` layout configured)
   h. Record cell in SQLite
4. **Navigate to the cell** — `tmux switch-client` to the new session

If any step after cloning fails, the clone directory is cleaned up (rm -rf) and the DB record is deleted.

The clone starts on whatever the repo's default branch is. Hive doesn't manage branches — the user checks out, creates, or switches branches inside each cell however they want. A cell is just a directory with a full clone; git operations are the user's business.

### 3. Navigate to Cell (from dashboard)

The dashboard shows all active cells in a tree view. Press Enter on a cell to switch to its tmux session. That's it.

From any cell, a tmux keybind (e.g., `<leader> + h`) switches back to the `hive` dashboard session. This creates a natural hub-and-spoke pattern: dashboard in the center, cells around it.

---

## The Dashboard

The dashboard is a Bubble Tea TUI with tabs/views:

### Cells View (default)

Shows all active cells grouped by project:

```
Hive Dashboard                                         Cells | Projects | Config

▼ myapp
    myapp-feature-auth      feature-auth     ●  2h    (3001, 5433)
    myapp-fix-bug-123       fix-bug-123      ●  15m   (3002, 5434)
▼ api
    api-refactor-endpoints  refactor-endpts  ●  1d
▼ Other tmux sessions
    random-session                           ●

↑/↓ navigate  enter switch  c create  x kill  tab next-tab  q quit
```

Actions available:
- **Enter** — switch to cell's tmux session
- **c** — create new cell (project picker → name input → clone → navigate)
- **x** — kill cell (confirm → kill tmux → rm clone dir → delete DB record)
- **r** — refresh cell list
- **n** — mark notifications as read

### Projects View

Lists discovered projects from `project_dirs`:

```
Hive Dashboard                                         Cells | Projects | Config

  Project          Path                              Config
  ─────────────────────────────────────────────────────────
  hive             ~/side_projects/hive               ✓ config
  my-api           ~/side_projects/my-api             ✓ config
  dotfiles         ~/repos/dotfiles                   ✗ no config
  frontend         ~/work/frontend                    ✓ config

↑/↓ navigate  enter edit config  e edit in $EDITOR  tab next-tab  q quit
```

Actions available:
- **Enter / e** — open `~/.hive/config/{project}.yml` in `$EDITOR` (creates a default if missing)

### Config View

Shows current global config. Press `e` to open `~/.hive/config.yaml` in `$EDITOR`.

```
Hive Dashboard                                         Cells | Projects | Config

  Global Config (~/.hive/config.yaml)
  ───────────────────────────────────
  project_dirs:
    - ~/side_projects
    - ~/work
    - ~/repos

  cells_dir:    ~/hive/cells
  editor:       vim
  tmux_leader:  C-a

  Press 'e' to edit config

↑/↓ navigate  e edit config  tab next-tab  q quit
```

After the editor closes, the dashboard reloads the config file.

---

## Kill Flow

Killing a cell (from the dashboard):

1. Confirm with the user (`Kill "myapp-work"? y/n`)
2. Kill the tmux session
3. Delete the clone directory (`rm -rf ~/hive/cells/myapp/work`)
4. Delete the DB record

The dashboard never kills itself. If you try, it refuses.

---

## Tmux Integration

Hive manages a simple set of tmux keybindings:

```tmux
# Navigate to Hive dashboard from any cell
bind-key h switch-client -t hive
```

These are generated by `hive install` and sourced from `~/.hive/tmux.conf`. The user adds `source-file ~/.hive/tmux.conf` to their `~/.tmux.conf`.

---

## State Storage

### SQLite (`~/.hive/state.db`) — Runtime State

What stays in the DB:
- `cells` table — active cells (name, project, clone path, status, ports, type)
- `notifications` table — messages sent between cells

What moves OUT of the DB:
- `global_config` table — replaced by `~/.hive/config.yaml`
- `repos` table — no longer needed; projects are discovered from `project_dirs`, per-project config lives in `~/.hive/config/{project}.yml`

### Config Files — User Configuration

- `~/.hive/config.yaml` — global settings (project dirs, cells dir, editor, tmux leader)
- `~/.hive/config/{project}.yml` — per-project settings (hooks, env, ports, layouts)

Both are parsed on dashboard startup and reloaded after editor changes.

---

## What Gets Cut from v1

### Commands removed
- `hive setup` — no registration; projects discovered automatically
- `hive join` — folded into dashboard navigate
- `hive switch` — folded into dashboard navigate
- `hive status` — folded into dashboard cells view
- `hive config show/export/import/apply` — replaced by editing config files directly
- `hive keybindings` — simplified, just `hive install`
- `hive notifications` — folded into dashboard
- `hive logs` — can stay as a convenience, or fold into dashboard
- `hive cell` — folded into dashboard create flow
- `hive kill` — folded into dashboard

### Commands remaining
- `hive start` — the entry point
- `hive install` — one-time machine setup (create dirs, generate tmux.conf, write default config.yaml)
- `hive dashboard` — the TUI (launched by `hive start`, not called directly by users)
- `hive notify` — send a notification from inside a cell (used by scripts/agents)

### Internal packages
- `internal/worktree/` — **removed**, replaced by simple git clone + rm -rf
- `internal/keybindings/` — **simplified**, just the dashboard keybind
- `internal/config/merge.go` — **removed**, no more config merging
- `internal/config/config.go` — **rewritten** to load global config + per-project config from YAML files
- `internal/hooks/` — stays
- `internal/layout/` — stays
- `internal/ports/` — stays
- `internal/envars/` — stays
- `internal/shell/` — stays
- `internal/tmux/` — stays
- `internal/state/` — simplified (drop `global_config` and `repos` tables)
- `internal/tui/` — **rewritten** as the primary interface with tabs, project picker, cell creation flow

---

## Flow Diagrams

### First Time Ever

```
$ hive install
  → creates ~/.hive/ and ~/.hive/config/
  → creates ~/hive/cells/
  → writes ~/.hive/config.yaml (prompts for project_dirs)
  → writes ~/.hive/tmux.conf
  → prints "add source-file ~/.hive/tmux.conf to your ~/.tmux.conf"

$ hive start
  → no `hive` tmux session exists
  → creates tmux session `hive` in ~/
  → launches dashboard TUI
  → attaches to tmux
  → user sees dashboard with no cells
  → presses 'c' to create first cell
```

### Daily Use

```
$ hive start
  → `hive` tmux session already exists
  → attaches to it
  → user sees dashboard with their cells
  → Enter to jump into a cell
  → <leader>+h to come back to dashboard
  → 'c' to create another cell
  → 'x' to kill a finished cell
```

### Creating a Cell

```
Dashboard: user presses 'c'
  → fuzzy picker shows projects: hive, my-api, frontend, ...
  → user picks "my-api"
  → text prompt: "Cell name: "
  → user types "work"
  → Hive:
      git clone ~/side_projects/my-api ~/hive/cells/my-api/work
      # reads ~/.hive/config/my-api.yml
      # allocates ports: PORT=3005, DB_PORT=5437
      # creates tmux session "my-api-work"
      # runs hooks: npm install, npm run db:migrate
      # applies default layout
      # records in SQLite
  → switches to "my-api-work" tmux session
```

### Creating a Headless Cell

```
Dashboard: user presses 'h' (or create → headless option)
  → text prompt: "Headless cell name: "
  → user types "scratch"
  → Hive:
      # creates tmux session "scratch" in ~/
      # records in SQLite as headless type
  → switches to "scratch" tmux session
```

---

## Resolved Decisions

- **Branches**: Hive doesn't manage branches. Each cell is a full clone; the user manages git inside it however they want. Cell names are just identifiers.
- **Clone depth**: Always full clone. No shallow option.
- **CLI cell creation**: Dropped for now. All cell creation goes through the dashboard. The internal logic is reusable if we want a CLI command later.
- **Notifications**: Still useful. `hive notify` remains as a CLI command for scripts/agents to send notifications from inside cells. The dashboard shows them.
- **Headless cells**: Still a thing. Created from the dashboard, just a tmux session in `~` with no git clone.
