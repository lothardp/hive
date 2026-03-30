# Architecture Definition: Orchestrator

**Project Goal:** A CLI-driven factory for spawning isolated, parallel, agentic development environments using Git Worktrees, Docker, and Virtual Hosting.

## 1\. Core Philosophy

  * **Isolation by Default:** Every feature branch gets its own filesystem (Worktree), its own services (Docker), and its own URL (Reverse Proxy).
  * **Ephemeral yet Persistent:** Workspaces are easy to create and destroy, but they stay "Warm" (running) until explicitly stopped.
  * **Agent-First:** Designed to provide **Claude Code** (or other agents) with a predictable, contained environment where it can't interfere with other tasks.

-----

## 2\. System Components

### A. The Controller (Go Binary)

A CLI tool (e.g., `orch`) written in Go. It manages the state and executes system calls.

  * **State Management:** A local JSON/SQLite store mapping workspace names to metadata (path, ports, status).
  * **Process Management:** Wraps `git`, `docker compose`, and `tmux` commands.

### B. The Networking Layer (Virtual Hosting)

Instead of manual port mapping, the system uses a **Global Reverse Proxy**.

  * **Proxy:** A single Traefik or Caddy container running on a "Management" Docker network.
  * **Routing:** Workspaces are assigned a local TLD (e.g., `feature-x.dev.local`). The proxy automatically routes traffic to the correct workspace container based on Docker labels.

### C. The Filesystem (Git Worktrees)

  * Workspaces are stored outside the main repo folder (e.g., `~/workspaces/<project>/<branch>`).
  * This prevents `node_modules` or build artifact collisions between parallel tasks.

-----

## 3\. Workspace Lifecycle

| Phase | Command | Actions Taken |
| :--- | :--- | :--- |
| **Provision** | `orch up <name>` | 1. Create Git Worktree.<br>2. Inject `.env` with unique ID.<br>3. Start Docker Compose with `-p <name>`.<br>4. Register URL with Proxy. |
| **Access** | `orch join <name>` | Attach to the dedicated Tmux session for that workspace. |
| **Suspend** | `orch stop <name>` | `docker compose stop`. Frees CPU/RAM but keeps data/state. |
| **Terminate** | `orch down <name>` | `docker compose down -v`, delete worktree, and clean up state. |

-----

## 4\. Technical Decisions

### Infrastructure-as-Code

Each project should contain an `.orch.yaml` file defining:

  * The path to the `docker-compose.yml`.
  * Post-creation "Seed" scripts (e.g., `npm install && go run migrate.go`).
  * The internal port that needs to be exposed to the proxy.

### Data Handling

  * **Databases:** Every workspace spawns a fresh database container.
  * **Seeding:** The Orchestrator triggers a "Seed" routine after the container is healthy to ensure the agent has a working dataset.

### Multiplexing (Phase 1)

  * **Tmux:** Initial implementation uses Tmux sessions for window management.
  * **Future:** A custom lightweight Go-based terminal multiplexer to provide better programmatic control for agents.

-----

## 5\. Directory Structure (Example)

```text
~/.orch/
├── state.json              # Global registry of workspaces
├── proxy/
│   └── docker-compose.yml  # The global Traefik/Caddy instance
└── bin/
    └── orch                # The compiled Go binary
```

-----

## 6\. Next Steps for Implementation

1.  **Initialize Go Project:** Set up the CLI structure using `spf13/cobra`.
2.  **Docker Integration:** Implement the `docker-compose` wrapper using the official Docker Go SDK.
3.  **Reverse Proxy Setup:** Create a command `orch init-proxy` to spin up the global Traefik container.
4.  **Worktree Logic:** Implement the git-cloning/worktree-addition logic.
