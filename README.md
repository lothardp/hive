# Hive

A CLI tool for spawning isolated, parallel development environments using Git Worktrees and tmux.

Hive lets you work on multiple feature branches simultaneously — each in its own worktree, with its own tmux session, completely isolated from each other. It's built for developers who juggle many tasks at once, and for AI agents (like Claude Code) that need contained environments where they can't interfere with other work.

## How It Works

Hive creates **cells** — isolated dev environments that combine:

- A **Git Worktree** (separate filesystem per branch, no conflicts)
- A **tmux session** (dedicated terminal, easy to switch between)
- **State tracking** in SQLite (know what's running, where, and on which branch)

```
~/workspaces/myproject/
├── feat-auth/       # cell: feat-auth (branch: feat-auth)
├── fix-api-bug/     # cell: fix-api-bug (branch: fix-api-bug)
└── refactor-db/     # cell: refactor-db (branch: refactor-db)
```

Each cell is fully independent. No shared `node_modules`, no port conflicts, no stepping on each other's toes.

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

## Usage

### Create a Cell

From inside a git repo:

```bash
hive cell my-feature          # Creates worktree + tmux on a new branch
hive cell bugfix -b main      # Creates worktree from existing branch
```

### Navigate Cells

```bash
hive join my-feature          # Attach to cell's tmux session
hive switch                   # Interactive fzf picker
hive status                   # List all cells
```

```
NAME           PROJECT    BRANCH         STATUS    TMUX    AGE
my-feature     myapp      my-feature     running   alive   2h
bugfix         myapp      main           running   alive   15m
```

### Clean Up

```bash
hive kill my-feature          # Removes worktree, tmux session, and DB record
```

## Configuration

Create a `.hive.yaml` in your project root to configure per-repo behavior:

```yaml
compose_path: docker-compose.yml
expose_port: 3000
seed_scripts:
  - npm install
  - npm run db:migrate
env:
  NODE_ENV: development
```

## Roadmap

- **Repo registration** (`hive setup`) — guided onboarding per repo
- **Setup hooks** — auto-run scripts on cell creation (copy deps, env files)
- **Port allocation** — auto-assign unique ports per cell, injected as env vars
- **Service management** (`hive up/down/stop`) — start/stop project services per cell
- **Reverse proxy** — `<cell>.dev.local` URL routing via Caddy
- **TUI dashboard** — interactive terminal UI as the default `hive` command
- **Notifications** — agents can notify you from inside cells
- **Queen sessions** — read-only main branch environment per repo
- **Headless cells** — quick scratch tmux sessions without git

## License

TBD
