# Hive

A TUI-first tool for spawning isolated, parallel development environments using Git clones and tmux.

Hive lets you work on multiple copies of a repo simultaneously — each in its own full clone, with its own tmux session, completely isolated from each other. The dashboard is your home base: create cells, navigate between them, kill them when you're done.

## How It Works

Hive creates **cells** — isolated dev environments that combine:

- A **Git clone** (full independent copy of the repo)
- A **tmux session** (dedicated terminal, easy to switch between)
- **State tracking** in SQLite (know what's running and where)
- **Port allocation** (unique ports per cell, no conflicts)
- **Setup hooks** (auto-run scripts on cell creation)

```
~/hive/cells/
├── myapp/
│   ├── work/            # clone of myapp
│   └── experiments/     # another clone of myapp
├── api/
│   └── refactor/        # clone of api
```

Each cell is fully independent. No shared `node_modules`, no port conflicts, no stepping on each other's toes. You manage branches inside each clone however you want — Hive doesn't care about git branches.

## Installation

### Prerequisites

- Go 1.24+
- Git
- tmux

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
hive install    # Configure project directories, cells directory, tmux integration
```

This creates `~/.hive/config.yaml` and generates tmux keybindings. Follow the instructions to source `~/.hive/tmux.conf` in your `~/.tmux.conf`.

### Launch the Dashboard

```bash
hive start      # Creates the dashboard tmux session and attaches to it
```

That's it. Everything else happens inside the dashboard.

## The Dashboard

The dashboard is a Bubble Tea TUI with four tabs:

### Cells Tab

Shows all active cells grouped by project. Navigate with `j`/`k`, press Enter to switch to a cell's tmux session, `c` to create a new cell, `x` to kill one.

From any cell, press `<prefix> h` to switch back to the dashboard.

| Key | Action |
|-----|--------|
| `j`/`k` or `↑`/`↓` | Navigate |
| `h`/`l` or `Tab`/`Shift+Tab` | Switch tab |
| `Enter` | Switch to cell |
| `c` | Create new cell (project picker → name → clone) |
| `o` | Open headless cell in a project directory |
| `H` | Create bare headless cell (tmux session only, in `~`) |
| `x` | Kill cell (with confirmation) |
| `n` | Mark notifications read |
| `r` | Refresh |
| `q` | Quit dashboard |

### Projects Tab

Lists all discovered projects (git repos found in your `project_dirs`). Press Enter to edit a project's config in your editor.

### Config Tab

Shows the global config. Press `e` to edit it in your editor.

### Notifs Tab

Browse all notifications with full content. Two sections: Unread at top, Read below. Press Enter to jump to the source cell (and pane), `m` to mark one as read, `M` to mark all, `D` to clean up read notifications.

## Configuration

### Global Config (`~/.hive/config.yaml`)

```yaml
project_dirs:
  - ~/side_projects
  - ~/work
  - ~/repos
cells_dir: ~/hive/cells
editor: vim
tmux_leader: "C-a"
```

Hive scans `project_dirs` one level deep for git repos — that's how it discovers your projects. No registration step needed.

### Per-Project Config (`~/.hive/config/{project}.yml`)

```yaml
repo_path: ~/side_projects/my-api
hooks:
  - npm install
  - npm run db:migrate
env:
  NODE_ENV: development
port_vars:
  - PORT
  - DB_PORT
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

Per-project configs are personal — they live in `~/.hive/config/`, not in the repo. Each developer customizes their own hooks and layouts.

### Port Allocation

List env var names in `port_vars`. Hive assigns unique ports (3001-9999) per cell and injects them as environment variables. No two cells share a port.

### Setup Hooks

Commands in `hooks` run sequentially in the clone directory after creation. They receive the cell's environment (ports, static env, `HIVE_CELL`). Execution aborts on first failure.

### Layouts

Define tmux window/pane layouts under `layouts`. A layout named `"default"` is auto-applied on cell creation.

## Notifications

Scripts or agents inside cells can send notifications:

```bash
hive notify "Build complete" -t "CI"
```

Unread counts appear in the dashboard next to each cell. The Notifs tab shows full notification content and lets you jump directly to the source pane.

For Claude Code integration, use the `--from-claude` flag in a hook — it reads the hook JSON from stdin and extracts the relevant fields automatically:

```json
{ "hooks": { "Notification": [{ "hooks": [{ "type": "command", "command": "[ -n \"$HIVE_CELL\" ] && hive notify --from-claude || true" }] }] } }
```

## CLI Reference

```
hive install               # One-time machine setup
hive start                 # Launch/attach to dashboard
hive switch                # Fuzzy-find and switch to a cell
hive health                # Show cell consistency across DB, disk, and tmux
hive notify <msg>          # Send notification from inside a cell
hive notify --from-claude  # Read Claude Code hook JSON from stdin
hive logs [-f]             # Tail the log file
```

## Tmux Keybindings

After `hive install`, source `~/.hive/tmux.conf` in your `~/.tmux.conf`:

| Keybinding | Action |
|------------|--------|
| `<prefix> h` | Switch to dashboard |
| `<prefix> .` | Fuzzy cell switcher (popup) |

## License

[MIT](LICENSE)
