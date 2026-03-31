# Phase 2 — Remaining Work

## `hive setup` — Agent Analysis (optional)

After guided prompts, offer to spawn an agent that analyzes the project and suggests config:

- Detect package manager and suggest seed scripts (`npm install`, `bundle install`, etc.)
- Detect common frameworks and suggest cell layout (e.g., Rails → editor + server + console tabs)
- Detect port usage in config files and suggest port env vars
- Detect gitignored files that are needed at runtime (`.env.local`, `node_modules/`, etc.) and suggest copy/symlink hooks
- Output should be suggestions the user confirms, not auto-applied

## `hive setup` — Worktree Compatibility Warning

Before registering, scan the repo for things that break in isolated worktrees:

- Hardcoded absolute paths in config files
- Scripts that assume they're at the repo root (e.g., `../../shared/`)
- Gitignored files required at runtime (`.env`, `.env.local`, config overrides)
- Symlinks pointing outside the repo
- Large directories that should be shared, not copied (`node_modules/`, `vendor/`, `.build/`)

Print warnings with suggested fixes (e.g., "consider adding a seed script to copy .env.local").

## Service Management (was Phase 8)

Start and stop project services per cell.

- `hive up <name>` — start services (could be Docker Compose, could be something else)
- `hive down <name>` — stop services and clean up
- `hive stop <name>` — suspend services (keep cell alive)
- Health check polling — wait for services to be ready

## Reverse Proxy (was Phase 9)

URL-based routing for cells that run web services.

- `hive init-proxy` — start global Caddy container on a shared Docker network
- `hive up` registers `<name>.dev.local` route via Caddy admin API
- `hive down` removes the route
- Complements port allocation — ports for simple setups, proxy for full web stacks

## `hive config edit` — Interactive Config Editing

Open the current repo's config (from DB) as editable YAML in `$EDITOR` (default: vim):

- Read the stored JSON config for the current repo from SQLite
- Convert to YAML and write to a temp file
- Open the temp file in the user's editor and wait for it to close
- On save: validate the YAML, diff against the original, and apply changes if valid
- On invalid YAML or schema errors: show the error and re-open the editor (or abort)
- `hive config edit --global` — same flow but for global config from `global_config` table

## Future — Global Setup Hooks

Global hooks that run for every cell regardless of repo (e.g., always set up a shared tool, always copy a global .gitconfig). Same ordered shell command strings as repo hooks, but stored at the global level. Repo hooks run after global hooks.
