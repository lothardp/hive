# Hive — Vision & Feature Spec

## Core Philosophy

Hive fully owns tmux. All sessions are created and managed through Hive — never directly. Tmux is treated as an implementation detail that will eventually be replaced, but kept for now because it already provides tabs and panes.

---

## Cell Types

- **Normal**: The most common. Opens a worktree of a registered repo on a new branch. Gets its own tmux session, setup hooks, port allocation, and env vars.
- **Headless**: A tmux session in an arbitrary directory. No git, no worktree — just a quick scratch workspace.
- **Multicell** *(future)*: Groups multiple repos under a single feature (e.g., server + web + mobile). Creates worktrees across all repos and places you in a parent directory. Deferred until normal cells are solid.

## Queen Session

When you create your first cell for a repo, Hive automatically creates a "queen" session — a read-only-ish main worktree parked on the default branch. This gives you a clean environment to explore the repo without creating a feature branch.

---

## Repo Registration (`hive setup`)

Each repo must be registered with Hive before creating cells. Running `hive setup` in a repo:

- Creates the repo record and per-repo config in the DB.
- Optionally spawns a guided agent that analyzes the project: What env vars does it need? Does it use Docker? What dependencies does it have? Does it need ports?
- Warns you if the codebase needs small modifications to work with isolated worktrees.
- Runs inside its own setup cell.

## Initial Installation (`hive install`)

One-time bootstrap for a new machine:

- Creates the Hive DB and config directory.
- Integrates with tmux by generating `~/.hive/tmux.conf` and asking the user to add a single `source-file` line to their existing tmux config. No magic, no overwriting.
- Optionally sets up cron jobs for background tasks.
- Prompts for your main projects directory (e.g., `~/side_projects`).

---

## Setup Hooks

Per-repo hooks that run when a new cell is created. Configured during `hive setup`. Examples:

- Copy or symlink `node_modules` from the queen worktree.
- Copy `.env.local` or other gitignored files into the new worktree.
- Run install commands (`npm install`, `bundle install`, etc.).
- Symlink shared build caches or artifact directories.

## Port Allocation & Environment Variables

Hive manages port ranges and injects env vars per cell:

- The repo config declares which env vars hold port numbers (e.g., `PORT`, `DB_PORT`).
- When a cell is created, Hive allocates unused ports and writes them into the cell's environment.
- Additional static env vars can be configured per repo (e.g., `MY_HOST=localhost`).
- This eliminates port conflicts between parallel cells.

## Tmux Keybindings

Hive registers custom tmux keybindings that map directly to Hive commands:

- Switch between cells (fzf picker).
- Quick-create a new cell.
- Kill the current cell.
- Show cell status.

These are defined in `~/.hive/tmux.conf` and stay under Hive's control.

---

## Hive TUI

When you launch `hive` with no arguments, it opens an interactive TUI (terminal UI) dashboard. From here you can:

- See all active cells with their status, branch, and project.
- Navigate between cells and switch into one.
- Create new cells.
- Kill cells.
- View notifications from agents.

This becomes the primary entry point — a command center for all your workspaces. Built with Bubble Tea (Go) to stay native to the terminal.

---

## Background Tasks

Instead of a daemon, Hive uses OS-level cron jobs managed by `hive install`:

- Periodic `git fetch` across all registered repos.
- Configurable per repo (e.g., fetch every 5 minutes, or never).
- Avoids the complexity of a custom daemon (process management, PID files, crash recovery).

## Config & Portability

- All config lives in the Hive SQLite DB for fast runtime access.
- Global config and per-repo config are exportable to JSON/YAML via `hive config export`.
- Importable via `hive config import` — makes it easy to share setups across machines or teams.

## Web Dashboard *(future)*

An optional web server (`hive serve`) that exposes a browser-based GUI for monitoring and managing cells. Useful for:

- Viewing cell status from a separate screen or device.
- Monitoring long-running agent activity without a terminal.
- Sharing workspace status with teammates.

Deferred until the TUI and core features are stable. The same data layer (SQLite) powers both interfaces.

---

## Priority Order

1. `hive install` — bootstrap (DB, tmux integration, projects directory)
2. Queen session auto-creation
3. Headless cells
4. Setup hooks system
5. Port allocation & env vars
6. `hive setup` (guided repo registration)
7. Tmux keybindings
8. Hive TUI
9. Background cron tasks
10. Web dashboard
