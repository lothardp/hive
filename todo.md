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
