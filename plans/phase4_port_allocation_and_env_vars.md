# Phase 4: Port Allocation & Environment Variables

## Overview

When running multiple cells in parallel, every cell tries to bind to the same ports (e.g., `:3000` for the web server, `:5432` for Postgres). This phase eliminates port conflicts by having Hive allocate unique ports per cell and inject them as environment variables into the tmux session. Static env vars (non-port) are also supported for things like `NODE_ENV=development` or `DATABASE_URL`.

---

## Part 1: Port Variables

### What they are

Port variables are env var names that the repo's services read for their listen ports. For example, a Node.js app might read `PORT`, and a local Postgres might read `DB_PORT`. Hive allocates a unique port for each variable per cell, so two cells can run the same services without clashing.

Typical use case:
- Cell "feature-a" gets `PORT=3001`, `DB_PORT=5433`
- Cell "feature-b" gets `PORT=3002`, `DB_PORT=5434`

### Data model

Add a `PortVars` field to `ProjectConfig`:

```go
type ProjectConfig struct {
    ComposePath string            `yaml:"compose_path" json:"compose_path"`
    SeedScripts []string          `yaml:"seed_scripts" json:"seed_scripts"`
    ExposePort  int               `yaml:"expose_port" json:"expose_port"`
    Env         map[string]string `yaml:"env" json:"env"`
    Hooks       []string          `yaml:"hooks" json:"hooks"`
    Layouts     map[string]Layout `yaml:"layouts" json:"layouts"`
    PortVars    []string          `yaml:"port_vars" json:"port_vars"` // NEW
}
```

`PortVars` is a list of env var names that should be allocated ports. The order doesn't matter — each gets a unique port from the allocator.

The existing `Cell.Ports` field (currently `"{}"` in the DB) will store the actual allocated ports as a JSON object:

```json
{"PORT": 3001, "DB_PORT": 5433}
```

This is already a `TEXT NOT NULL DEFAULT '{}'` column in the `cells` table — no schema migration needed.

### YAML format

Minimal (just port vars):
```yaml
port_vars:
  - PORT
  - DB_PORT
```

Full config with port vars and static env:
```yaml
port_vars:
  - PORT
  - DB_PORT
  - REDIS_PORT

env:
  NODE_ENV: development
  LOG_LEVEL: debug
```

### Port allocator

New package `internal/ports/ports.go`:

```go
package ports

type Allocator struct {
    db    *sql.DB
    start int // lower bound of port range (inclusive)
    end   int // upper bound of port range (inclusive)
}

func NewAllocator(db *sql.DB) *Allocator {
    return &Allocator{
        db:    db,
        start: 3001,
        end:   9999,
    }
}

// Allocate finds unused ports for the given variable names.
// Returns a map of var name -> allocated port.
func (a *Allocator) Allocate(ctx context.Context, varNames []string) (map[string]int, error)

// UsedPorts returns all ports currently allocated to any cell.
func (a *Allocator) UsedPorts(ctx context.Context) (map[int]string, error)
```

#### Allocation algorithm

1. Query all existing cells and collect every port number from their `ports` JSON column:
   ```sql
   SELECT name, ports FROM cells WHERE ports != '{}'
   ```
2. Parse each cell's ports JSON into `map[string]int`, collect all port values into a `usedPorts` set.
3. Starting from `a.start`, find the next available port not in `usedPorts`. Assign it to the first var name. Repeat for each var name.
4. Return the map.

This is simple and correct for the expected scale (tens of cells, not thousands). No need for a separate `port_allocations` table — the `cells.ports` column is the source of truth.

#### Port range

Default range: `3001–9999`. This avoids:
- Ports below 1024 (privileged)
- Port 3000 (commonly hardcoded in starter templates)
- Ports above 10000 (less conventional for dev)

The range is hardcoded for now. Future: make it configurable via `global_config`.

#### Error handling

- If the port range is exhausted (all ports used), return an error: `fmt.Errorf("no available ports in range %d-%d", a.start, a.end)`
- If the DB query fails, propagate the error.
- Port allocation is part of cell creation — if it fails, the cell is not created (rolls back worktree + DB).

### Port release

Ports are released automatically when a cell is deleted from the DB (`hive kill`). Since the allocator reads from the `cells` table, a deleted cell's ports are immediately available. No explicit "release" step needed.

---

## Part 2: Environment Variable Injection

### How env vars reach the tmux session

Tmux sessions inherit the environment of the process that created them. To inject env vars into a cell's tmux session, we need to set them on the session **before** any commands run (hooks, layout commands).

The tmux `set-environment` command does exactly this:

```
tmux set-environment -t <session> <VAR> <VALUE>
```

Variables set this way are inherited by all new panes/windows in that session.

### What gets injected

Two sources of env vars are combined:

1. **Port vars** — from the allocator: `{"PORT": 3001, "DB_PORT": 5433}` → `PORT=3001`, `DB_PORT=5433`
2. **Static env vars** — from `ProjectConfig.Env`: `{"NODE_ENV": "development"}` → `NODE_ENV=development`

If a key appears in both (e.g., someone puts `PORT` in both `port_vars` and `env`), the allocated port wins. This prevents accidentally hardcoding a port that should be dynamic.

### Env injector

New package `internal/envars/envars.go`:

```go
package envars

import (
    "context"

    "github.com/lothardp/hive/internal/shell"
)

// Injection represents a set of env vars to inject into a tmux session.
type Injection struct {
    Session string
    Vars    map[string]string // combined port + static vars
}

// Inject sets environment variables on a tmux session.
// Must be called after session creation but before hooks/layout.
func Inject(ctx context.Context, inj *Injection) error
```

Implementation:
```go
func Inject(ctx context.Context, inj *Injection) error {
    for k, v := range inj.Vars {
        res, err := shell.Run(ctx, "tmux", "set-environment", "-t", inj.Session, k, v)
        if err != nil {
            return fmt.Errorf("setting env var %s: %w", k, err)
        }
        if res.ExitCode != 0 {
            return fmt.Errorf("setting env var %s: %s", k, strings.TrimSpace(res.Stderr))
        }
    }
    return nil
}

// BuildVars combines allocated ports and static env vars into a single map.
// Port vars take precedence over static env vars with the same name.
func BuildVars(ports map[string]int, staticEnv map[string]string) map[string]string {
    vars := make(map[string]string, len(ports)+len(staticEnv))
    for k, v := range staticEnv {
        vars[k] = v
    }
    for k, v := range ports {
        vars[k] = strconv.Itoa(v)
    }
    return vars
}
```

### Also inject `HIVE_CELL`

Always inject `HIVE_CELL=<cell_name>` into every tmux session, even if no port vars or static env are configured. This lets scripts and tools detect they're running inside a Hive cell. This is unconditional — no config needed.

---

## Part 3: Integration into Cell Lifecycle

### Cell creation flow

Updated step sequence in `cmd/cell.go`:

```
1. Create worktree           (existing)
2. Record in DB              (existing)
3. Allocate ports            ← NEW
4. Update cell ports in DB   ← NEW
5. Create tmux session       (existing)
6. Inject env vars           ← NEW (must be after session creation, before hooks)
7. Run setup hooks           (existing — hooks can now read injected vars)
8. Apply layout              (existing — layout commands inherit injected vars)
9. Print summary             (existing, updated to show ports)
```

#### Step 3–4: Allocate and persist

```go
// Allocate ports
var allocatedPorts map[string]int
if app.Config != nil && len(app.Config.PortVars) > 0 {
    allocator := ports.NewAllocator(app.DB)
    allocatedPorts, err = allocator.Allocate(ctx, app.Config.PortVars)
    if err != nil {
        _ = app.Repo.Delete(ctx, name)
        _ = app.WtMgr.Remove(ctx, app.RepoDir, wtPath)
        return fmt.Errorf("allocating ports: %w", err)
    }

    // Persist to cell record
    portsJSON, _ := json.Marshal(allocatedPorts)
    cell.Ports = string(portsJSON)
    if err := app.Repo.UpdatePorts(ctx, name, cell.Ports); err != nil {
        _ = app.Repo.Delete(ctx, name)
        _ = app.WtMgr.Remove(ctx, app.RepoDir, wtPath)
        return fmt.Errorf("saving port allocations: %w", err)
    }
}
```

#### Step 6: Inject env vars

```go
// Inject env vars into tmux session
envVars := envars.BuildVars(allocatedPorts, app.Config.Env)
envVars["HIVE_CELL"] = name
if len(envVars) > 0 {
    inj := &envars.Injection{Session: name, Vars: envVars}
    if err := envars.Inject(ctx, inj); err != nil {
        // Non-fatal — cell is still usable, just without injected vars
        fmt.Fprintf(cmd.ErrOrStderr(), "warning: failed to inject env vars: %v\n", err)
    }
}
```

Env injection failure is a **warning**, not a fatal error. The cell is still usable — the user just won't have the env vars set automatically.

#### Rollback behavior

Port allocation failure **is** fatal — it happens before the tmux session, so we roll back worktree + DB (same as existing pattern). Env injection failure is **non-fatal** — the session already exists, and the cell is still functional.

### New repository method

Add `UpdatePorts` to `CellRepository` in `internal/state/repo.go`:

```go
func (r *CellRepository) UpdatePorts(ctx context.Context, name, ports string) error {
    result, err := r.db.ExecContext(ctx,
        `UPDATE cells SET ports = ?, updated_at = ? WHERE name = ?`,
        ports, time.Now(), name,
    )
    if err != nil {
        return fmt.Errorf("updating cell ports: %w", err)
    }
    n, err := result.RowsAffected()
    if err != nil {
        return fmt.Errorf("checking rows affected: %w", err)
    }
    if n == 0 {
        return fmt.Errorf("cell %q not found", name)
    }
    return nil
}
```

### `hive kill` — no changes needed

The cell is deleted from the DB, which removes the ports JSON. The allocator will no longer see those ports as used. No explicit release step.

### `hive status` — show ports

Update `cmd/status.go` to include a PORTS column:

```
NAME          PROJECT  BRANCH       STATUS   TMUX   PORTS              AGE
feature-a     myapp    feature-a    stopped  alive  3001,5433          2h
feature-b     myapp    feature-b    stopped  alive  3002,5434          1h
backend-fix   api      backend-fix  stopped  alive  -                  30m
```

Parse the cell's `Ports` JSON and display as a comma-separated list of port numbers. Show `-` if no ports allocated.

---

## Part 4: CLI Output

### Cell creation with ports

```
$ hive cell my-feature
Cell "my-feature" created
  Branch:   my-feature
  Worktree: ~/.hive/cells/myapp/my-feature
  Tmux:     my-feature
  Ports:    3001 (PORT), 5433 (DB_PORT)
  Hooks:    3/3 passed
  Layout:   applied
```

### Cell creation without ports (no port_vars configured)

```
$ hive cell my-feature
Cell "my-feature" created
  Branch:   my-feature
  Worktree: ~/.hive/cells/myapp/my-feature
  Tmux:     my-feature
  Hooks:    3/3 passed
  Layout:   applied
```

No "Ports" line — same as current behavior.

### Port exhaustion error

```
$ hive cell my-feature
Error: allocating ports: no available ports in range 3001-9999
```

The cell is not created. Worktree and DB record are rolled back.

---

## Part 5: YAML Configuration Examples

### Repo config with port vars

```yaml
port_vars:
  - PORT
  - DB_PORT

env:
  NODE_ENV: development
  DATABASE_URL: postgres://localhost:$DB_PORT/myapp_dev

hooks:
  - npm install
  - cp ../.env.local .env.local

layouts:
  default:
    windows:
      - name: editor
        panes:
          - command: ""
      - name: server
        panes:
          - command: npm run dev
```

Note: `DATABASE_URL` contains `$DB_PORT` — this is a raw string, not shell-expanded by Hive. The application would need to resolve it at startup, or the hook could do the substitution. This is intentional — Hive injects vars, it doesn't template config files.

### Minimal config (just ports)

```yaml
port_vars:
  - PORT
```

---

## Implementation Order

### Step 1: Data model changes
- Add `PortVars []string` to `ProjectConfig` in `internal/config/config.go`
- Update `Merge()` in `internal/config/merge.go` — `PortVars` is a list, so it uses replace semantics (same as `Hooks`)
- No DB schema changes needed — `cells.ports` column already exists

### Step 2: Port allocator
- Create `internal/ports/ports.go` with `Allocator` struct
- `Allocate()` method: query cells, collect used ports, find available ones
- `UsedPorts()` method: return map of port→cell name (useful for debugging)
- Unit tests with in-memory SQLite: create cells with known ports, verify allocation avoids them

### Step 3: Env var injector
- Create `internal/envars/envars.go` with `Inject()` and `BuildVars()`
- `Inject()` calls `tmux set-environment` for each var
- `BuildVars()` merges ports + static env, ports take precedence
- Unit test for `BuildVars()` (merge logic and precedence)

### Step 4: Wire into cell creation
- Add `UpdatePorts()` to `CellRepository`
- Update `cmd/cell.go` with the new steps (allocate → persist → inject)
- Always inject `HIVE_CELL=<cell_name>`
- Update port display in success output

### Step 5: Update `hive status`
- Parse `Ports` JSON column
- Add PORTS column to table output

### Step 6: Tests
- Port allocator: empty DB, DB with existing cells, range exhaustion
- Env var builder: ports only, static only, combined, precedence
- Integration: full cell creation with ports (requires tmux — may be manual test)
