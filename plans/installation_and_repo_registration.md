# Phase 2: Installation & Repo Registration

## Context

Hive currently bootstraps implicitly — `PersistentPreRunE` creates `~/.hive/` and the DB on every command run, and config lives in `.hive.yaml` files. Phase 2 makes installation explicit, moves config to SQLite, and adds repo registration. This enables per-repo customization (layout, hooks, ports) that later phases depend on.

---

## Step 1: Schema + Models

**Files**: `internal/state/db.go`, `internal/state/models.go`

Add two tables to the `schema` constant:

```sql
CREATE TABLE IF NOT EXISTS global_config (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS repos (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    name           TEXT UNIQUE NOT NULL,
    path           TEXT UNIQUE NOT NULL,
    remote_url     TEXT NOT NULL DEFAULT '',
    default_branch TEXT NOT NULL DEFAULT 'main',
    config         TEXT NOT NULL DEFAULT '{}',
    created_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_repos_path ON repos(path);
```

Add `Repo` struct to `models.go` (same pattern as `Cell`): ID, Name, Path, RemoteURL, DefaultBranch, Config (JSON string), CreatedAt, UpdatedAt.

---

## Step 2: Repositories

**New files**: `internal/state/config_repo.go`, `internal/state/repo_repo.go`

**ConfigRepository** (key-value for global config):
- `Get(ctx, key) (string, error)` — returns `""` if not found
- `Set(ctx, key, value) error` — INSERT OR REPLACE
- `Delete(ctx, key) error`
- `All(ctx) (map[string]string, error)`

**RepoRepository** (registered repos):
- `Create(ctx, repo) error`
- `GetByName(ctx, name) (*Repo, error)` — nil,nil if not found
- `GetByPath(ctx, path) (*Repo, error)` — primary lookup in PersistentPreRunE
- `List(ctx) ([]Repo, error)`
- `Update(ctx, repo) error`
- `Delete(ctx, name) error`

Write tests for both following `repo_test.go` pattern (in-memory DB, context.Background).

---

## Step 3: Config serialization

**File**: `internal/config/config.go`

Add methods to `ProjectConfig`:
- `ToJSON() (string, error)` — for storing in repos.config column
- `ProjectConfigFromJSON(data string) (*ProjectConfig, error)` — unmarshal + applyDefaults
- `ToYAML() ([]byte, error)` — for export

Keep existing `Load()` and `LoadOrDefault()` for backward compat with unregistered repos.

---

## Step 4: App struct + PersistentPreRunE

**File**: `cmd/root.go`

Add to App struct:
```go
ConfigRepo *state.ConfigRepository
RepoRepo   *state.RepoRepository
RepoRecord *state.Repo  // registered repo for current dir, or nil
```

Update PersistentPreRunE config resolution:
1. Initialize ConfigRepo and RepoRepo after opening DB
2. After detecting git repo, look up `RepoRepo.GetByPath(ctx, repoDir)`
3. If found: set `app.RepoRecord`, parse `repo.Config` JSON → `app.Config`
4. If not found: fall back to `config.LoadOrDefault(repoDir)` (existing behavior)
5. For worktree base dir: try `ConfigRepo.Get("projects_dir")`, fall back to `DefaultBaseDir()`

---

## Step 5: `hive install`

**New file**: `cmd/install.go`

Interactive command, no args, idempotent. Steps:
1. Check `global_config` for `installed_at` — if found, print status and ask to re-run
2. Generate `~/.hive/tmux.conf` (minimal placeholder, skip if exists)
3. Print tmux integration instructions (detect if already sourced in `~/.tmux.conf`)
4. Prompt for projects directory (default `~/side_projects`), store as `projects_dir`
5. Write `installed_at` timestamp
6. Print summary

Interactive input via `bufio.NewReader(os.Stdin).ReadString('\n')`.

---

## Step 6: `hive setup`

**New file**: `cmd/setup.go`

Must be run from inside a git repo. Steps:
1. Require `app.RepoDir != ""`
2. Check if already registered via `GetByPath` — if so, offer to update
3. Display detected project name (from `app.Project`), let user confirm/override
4. Detect remote URL and default branch from git
5. Prompt for config: compose_path, seed_scripts, expose_port, env vars
6. Pre-populate defaults from existing `.hive.yaml` if one is found
7. Serialize config to JSON, create/update repo record
8. Print summary

---

## Step 7: `hive config export/import`

**New file**: `cmd/config.go`

Parent command `hive config` with subcommands:

- **`hive config export [--file path]`**: Requires registered repo. Serialize `app.Config` to YAML. Write to file or stdout.
- **`hive config import [--file path]`**: Requires registered repo (error with "run hive setup first"). Parse YAML, update repo record.
- **`hive config show`**: Print current effective config as YAML (works for both registered and unregistered repos).

---

## Migration Path

No automated migration. Unregistered repos keep working with `.hive.yaml` fallback. To migrate: run `hive setup` in each repo (prompts pre-populate from existing `.hive.yaml`). Zero breaking changes.

---

## Verification

1. `go test ./internal/state/...` — new repo tests pass against :memory: SQLite
2. `go build` — compiles clean
3. `hive install` — creates tmux.conf, stores projects_dir, prints instructions
4. `hive install` again — detects already installed, offers re-run
5. `hive setup` in a git repo — registers repo, stores config in DB
6. `hive cell test` — creates cell using DB config (not .hive.yaml)
7. `hive config export` — outputs YAML matching what was entered in setup
8. `hive config export | hive config import` — round-trips
9. In an unregistered repo: `hive cell test` — still works with .hive.yaml fallback
