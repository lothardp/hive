# Phase 3: Setup Hooks & Cell Layout

## Overview

Two independent features that make cells useful out of the box:
1. **Setup hooks** — ordered shell commands that run after a cell's worktree is created
2. **Cell layouts** — tmux window/pane configurations applied when creating a cell

Both are stored in the DB and configured via `hive config apply`.

---

## Part 1: Setup Hooks

### What they are

An ordered list of shell command strings stored per-repo. They run sequentially in the new worktree directory after cell creation. Typical use cases:

- `cp ../queen/.env.local .env.local`
- `ln -s ../queen/node_modules node_modules`
- `npm install`
- `bundle install`

### Data model

Add a `hooks` field to `ProjectConfig`:

```go
type ProjectConfig struct {
    ComposePath string            `yaml:"compose_path" json:"compose_path"`
    SeedScripts []string          `yaml:"seed_scripts" json:"seed_scripts"`
    ExposePort  int               `yaml:"expose_port" json:"expose_port"`
    Env         map[string]string `yaml:"env" json:"env"`
    Hooks       []string          `yaml:"hooks" json:"hooks"`        // NEW
    Layouts     map[string]Layout `yaml:"layouts" json:"layouts"`    // NEW (Part 2)
}
```

Hooks live in the existing `repos.config` JSON blob — no new tables needed.

### Execution

New package `internal/hooks/hooks.go`:

```go
type Runner struct{}

func (r *Runner) Run(ctx context.Context, workDir string, hooks []string) *Result

type Result struct {
    Ran    int    // how many hooks executed (including the failed one)
    Total  int    // total hooks configured
    Failed *HookError // nil if all passed
}

type HookError struct {
    Command string
    Index   int
    Stderr  string
    Err     error
}
```

- Run each hook sequentially via `shell.RunInDir(ctx, workDir, "sh", "-c", command)`
- On first failure, **stop immediately** — later hooks may depend on earlier ones
- Write `hook_results.txt` to the worktree root with outcome of each hook that ran, plus the error details for the failed one
- Return result to caller for display

### Integration into `hive cell`

In `cmd/cell.go`, after tmux session creation and before the success message:

```
1. Create worktree        (existing)
2. Record in DB           (existing)
3. Create tmux session    (existing)
4. Run setup hooks        ← NEW
5. Apply layout           ← NEW (Part 2)
6. Print summary          (existing)
```

If hooks fail, print a warning but don't roll back. The cell is still created and usable.

### CLI output

```
$ hive cell my-feature
Cell "my-feature" created
  Branch:   my-feature
  Worktree: /Users/lothar/side_projects/hive-my-feature
  Tmux:     my-feature
  Hooks:    3/3 passed

# or on failure:
  Hooks:    2/3 failed at hook 2 (see hook_results.txt)
```

---

## Part 2: Cell Layouts

### What they are

Named tmux layout definitions. Each layout describes a set of windows (tabs), where each window can have multiple panes with optional commands. No artificial limits on windows or panes — anything tmux supports.

### Data model

```go
type Layout struct {
    Windows []Window `yaml:"windows" json:"windows"`
}

type Window struct {
    Name  string `yaml:"name" json:"name"`
    Panes []Pane `yaml:"panes" json:"panes"`
}

type Pane struct {
    Command string `yaml:"command,omitempty" json:"command,omitempty"`
    Split   string `yaml:"split,omitempty" json:"split,omitempty"` // "horizontal" or "vertical", empty for first pane
}
```

Layouts are stored in `ProjectConfig.Layouts` as a `map[string]Layout`. The key is the layout name (e.g., `"default"`, `"fullstack"`, `"minimal"`).

### Global layouts

Stored in `global_config` under the key `layouts` as a JSON map of `map[string]Layout`. Same format as repo layouts.

Resolution order: repo layout > global layout (by name).

### YAML format

```yaml
layouts:
  default:
    windows:
      - name: editor
        panes:
          - command: nvim
      - name: server
        panes:
          - command: npm run dev
      - name: tests
        panes:
          - command: npm test -- --watch
          - split: horizontal
            command: ""
      - name: shell
        panes:
          - command: ""

  minimal:
    windows:
      - name: editor
        panes:
          - command: nvim
      - name: shell
        panes:
          - command: ""
```

### Execution

New package `internal/layout/layout.go`:

```go
func Apply(ctx context.Context, sessionName string, workDir string, layout Layout) error
```

Implementation using tmux commands:
1. The session already has one window (created by `tmux new-session`). Rename it to the first layout window's name.
2. For each additional window: `tmux new-window -t <session> -n <name> -c <workDir>`
3. For each additional pane in a window: `tmux split-window -t <session>:<window> [-h|-v] -c <workDir>`
4. For each pane with a command: `tmux send-keys -t <session>:<window>.<pane> "<command>" Enter`
5. Select the first window when done: `tmux select-window -t <session>:0`

### Integration into `hive cell`

After hooks run, if the repo has a `"default"` layout (repo-level or global), apply it. No layout = current behavior (one window, plain shell).

Later: `--layout` flag and interactive picker.

### Layout selection (current scope)

For now, `hive cell` always uses the `"default"` layout if one exists. No `--layout` flag yet.

Future: when called interactively (e.g., from a tmux keybinding), show available layouts and let the user pick.

---

## Part 3: `hive config apply`

### What it does

Merges a YAML file into the repo's existing config. Unlike `hive config import` which replaces everything, `apply` does an upsert:

- Scalar fields (compose_path, expose_port): overwrite
- Maps (env, layouts): merge keys (new keys added, existing keys updated)
- Lists (hooks, seed_scripts): replace entirely (no way to merge ordered lists sensibly)

### Usage

```bash
# Apply from file
hive config apply -f layout.yaml

# Apply from stdin
cat layout.yaml | hive config apply

# Apply global layout
hive config apply -f layout.yaml --global
```

### `--global` flag

When `--global` is passed, layouts are stored in `global_config` instead of the repo config. Only layouts are supported at the global level for now.

### Implementation

New subcommand in `cmd/config.go`:

```go
var configApplyCmd = &cobra.Command{
    Use:   "apply",
    Short: "Merge config from YAML into current repo config",
}
```

Merge logic lives in `internal/config/merge.go`:

```go
func (c *ProjectConfig) Merge(other *ProjectConfig)
```

---

## Implementation Order

### Step 1: Data model changes
- Add `Hooks []string` and `Layouts map[string]Layout` to `ProjectConfig`
- Add `Layout`, `Window`, `Pane` types in `internal/config/`
- Update JSON/YAML serialization

### Step 2: `hive config apply`
- Add `configApplyCmd` with `--global` flag
- Implement merge logic
- This lets us configure hooks and layouts before the execution code exists

### Step 3: Hook execution
- `internal/hooks/hooks.go` — runner
- Wire into `cmd/cell.go`
- `hook_errors.txt` on failure

### Step 4: Layout execution
- `internal/layout/layout.go` — apply layout via tmux commands
- Wire into `cmd/cell.go` (after hooks)
- Resolve layout: repo default > global default > no layout

### Step 5: Tests
- Unit tests for config merge logic
- Unit tests for hook runner (mock shell)
- Unit tests for layout tmux command generation
- Integration test: full cell creation with hooks and layout
