# Hive

A CLI tool for spawning isolated, parallel development environments using Git Worktrees and tmux.

Hive lets you work on multiple feature branches simultaneously — each in its own worktree, with its own tmux session, completely isolated from each other. It's built for developers who juggle many tasks at once, and for AI agents (like Claude Code) that need contained environments where they can't interfere with other work.

## How It Works

Hive creates **cells** — isolated dev environments that combine:

- A **Git Worktree** (separate filesystem per branch, no conflicts)
- A **tmux session** (dedicated terminal, easy to switch between)
- **State tracking** in SQLite (know what's running, where, and on which branch)
- **Port allocation** (unique ports per cell, no conflicts)
- **Setup hooks** (auto-run scripts on cell creation)

```
~/.hive/cells/myproject/
├── feat-auth/       # cell: feat-auth (branch: feat-auth)
├── fix-api-bug/     # cell: fix-api-bug (branch: fix-api-bug)
└── refactor-db/     # cell: refactor-db (branch: refactor-db)
```

Each cell is fully independent. No shared `node_modules`, no port conflicts, no stepping on each other's toes.

Hive also supports **queen sessions** (a read-only environment on your default branch, auto-created with your first cell) and **headless cells** (quick tmux sessions in any directory, no git worktree needed).

## Installation

### Prerequisites

- Go 1.24+
- Git
- tmux
- fzf (for interactive cell switching)

### Build from Source

```bash
git clone https://github.com/lothardp/hive.git
cd hive
go build -o hive .
```

Move the binary somewhere in your `PATH`:

```bash
mv hive /usr/local/bin/
```

## Getting Started

### First-Time Setup

```bash
hive install                  # Configure cells directory, projects directory, tmux integration
```

### Register a Repo

From inside a git repo:

```bash
hive setup                    # Interactive: project name, remote, default branch, config
```

## Usage

### Create a Cell

From inside a registered (or any git) repo:

```bash
hive cell my-feature          # Creates myapp-my-feature (worktree + tmux)
hive cell bugfix -b main      # Creates myapp-bugfix from existing branch
hive cell scratch --headless  # Tmux session in current dir (no prefix, no worktree)
hive cell notes --headless ~/notes  # Tmux session in a specific directory
```

On your first cell for a project, Hive automatically creates a **queen session** — a protected environment on the default branch.

Cell names are automatically prefixed with the project name (e.g., `my-feature` becomes `myapp-my-feature`). Headless cells are never prefixed. You can override the prefix with `cell_prefix` in your config.

### Navigate Cells

```bash
hive join myapp-my-feature    # Attach to cell's tmux session
hive switch                   # Interactive fzf picker
hive status                   # List all cells
```

```
NAME                    PROJECT    BRANCH         STATUS    TMUX    PORTS      AGE
myapp-queen [queen]     myapp      main           stopped   alive   -          2h
myapp-my-feature        myapp      my-feature     stopped   alive   3001,5433  2h
myapp-bugfix            myapp      main           stopped   alive   3002,5434  15m
scratch [headless]      -          -              stopped   alive   -          5m
```

### Dashboard

```bash
hive dashboard                # Interactive TUI overview of all cells
```

A tree-view TUI (built with Bubble Tea) showing cells grouped under projects. Queen cells are highlighted with a crown icon, headless cells are dimmed. Also shows non-Hive tmux sessions in a separate group.

| Key | Action |
|-----|--------|
| `j`/`k` or `↑`/`↓` | Navigate |
| `Enter` | Switch to cell |
| `x` | Kill cell (with confirmation) |
| `n` | Mark notifications read |
| `c` | Create cell prompt |
| `r` | Refresh |
| `q` | Quit |

### Notifications

Agents (or scripts) running inside cells can send notifications:

```bash
hive notify myapp-my-feature -m "Build complete" -t "CI"
hive notifications                # List all
hive notifications --unread       # Unread only
hive notifications 42             # View details for notification #42
```

Unread counts appear in `hive status` and the TUI dashboard.

### Tmux Keybindings

```bash
hive keybindings              # Regenerate and reload tmux keybindings
hive keybindings --direct     # Bind directly to <prefix> <key> instead of key table
```

By default, Hive uses a tmux key table: `<prefix> h` enters the Hive table, then `s`/`c`/`k`/`d` for switch/create/kill/dashboard. With `--direct`, bindings go straight to `<prefix> s`, etc.

### Clean Up

```bash
hive kill myapp-my-feature    # Removes worktree, branch, tmux session, and DB record
```

Queen sessions can't be killed while other cells exist for the project — kill the regular cells first.

## Configuration

Register your repo with `hive setup` for interactive configuration, or manage config with `hive config`:

```bash
hive config show              # Show effective config (DB or .hive.yaml)
hive config export -f cfg.yaml  # Export to file
hive config import -f cfg.yaml  # Import from file
hive config apply -f patch.yaml # Merge partial config into existing
hive config apply -f layouts.yaml --global  # Apply layouts globally
```

Config can live in the database (via `hive setup`) or in a `.hive.yaml` file in your project root. DB config takes precedence.

```yaml
compose_path: docker-compose.yml
expose_port: 3000
cell_prefix: myapp              # Override default project-name prefix (optional)
seed_scripts:
  - npm install
  - npm run db:migrate
env:
  NODE_ENV: development
port_vars:
  - PORT
  - DB_PORT
hooks:
  - cp ../.env .env
  - npm install
layouts:
  default:
    windows:
      - name: editor
        panes:
          - command: nvim .
      - name: server
        panes:
          - command: npm run dev
          - command: npm run test:watch
            split: horizontal
```

### Port Allocation

List the env var names you need in `port_vars`. Hive assigns unique ports (3001-9999) per cell and injects them as environment variables into the tmux session. No two cells will share a port.

### Setup Hooks

Commands listed in `hooks` run sequentially in the cell's worktree on creation. They receive the full cell environment (ports, static env, `HIVE_CELL`, `HIVE_QUEEN_DIR`). Execution aborts on the first failure.

### Layouts

Define tmux window/pane layouts in `layouts`. A layout named `"default"` is auto-applied when creating a cell. Layouts can be set per-repo or globally with `hive config apply --global`.

## Shell Completion

Hive supports tab completion for commands, flags, and cell names:

```bash
# Zsh (add to ~/.zshrc)
source <(hive completion zsh)

# Bash (add to ~/.bashrc)
source <(hive completion bash)

# Fish
hive completion fish | source
```

After setup, `hive kill <TAB>` will show all cell names.

## Roadmap

- **Background tasks** — periodic git fetch and other cron-style operations
- **Service management** (`hive up/down/stop`) — start/stop project services per cell
- **Reverse proxy** — `<cell>.dev.local` URL routing via Caddy

## License

TBD
