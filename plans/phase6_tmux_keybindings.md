# Phase 6: Tmux Keybindings

## Overview

Custom tmux keybindings that let you control Hive without leaving tmux. Keybindings are written to `~/.hive/tmux.conf` (already sourced by the user's `~/.tmux.conf` since `hive install`) and call `hive` commands via tmux popup windows.

Four keybindings, all under a `<prefix> H` leader:
1. **Switch cell** — fzf picker in a tmux popup
2. **Create cell** — prompt for name in a tmux popup
3. **Kill current cell** — confirm, then tear down
4. **Status** — show cell table in a tmux popup

---

## Part 1: Keybinding Design

### What they are

Tmux key bindings that invoke Hive CLI commands. All bindings live behind a two-key sequence: the tmux prefix (usually `Ctrl-b`) followed by `H` enters "Hive mode", then a single key triggers the action. This avoids collisions with existing tmux bindings.

| Sequence | Action | Hive command |
|----------|--------|-------------|
| `<prefix> H s` | Switch cell (fzf popup) | `hive switch` |
| `<prefix> H c` | Create cell (name prompt popup) | `hive cell <name>` |
| `<prefix> H k` | Kill current cell (confirm popup) | `hive kill <name>` |
| `<prefix> H d` | Status dashboard (popup) | `hive status` |

Mnemonics: **s**witch, **c**reate, **k**ill, **d**ashboard.

### Why a key table instead of flat bindings

Tmux supports "key tables" — a binding can switch to a named table, and the next keypress is resolved from that table. This gives us a clean namespace (`H` + one key) without burning four top-level prefix bindings. If the user presses an unbound key after `H`, tmux silently drops back to the root table.

### Tmux mechanisms used

1. **Key tables** — `bind-key -T hive <key> <action>` defines bindings in a custom `hive` table. `bind-key H switch-client -T hive` enters that table from the prefix table.

2. **Popups** — `tmux display-popup` opens a floating terminal overlay. The popup runs a command and closes when it exits. Flags:
   - `-E` — close popup when command exits
   - `-w <width>` / `-h <height>` — size (percentage or absolute)
   - `-T <title>` — popup border title
   - The command runs in a subshell, inheriting the pane's environment (including `HIVE_CELL`)

3. **Cell env vars** — every cell's tmux session has `HIVE_CELL=<cell_name>` and `HIVE_QUEEN=<queen_cell_name>` in its environment. Popups inherit the pane's environment, so `$HIVE_CELL` and `$HIVE_QUEEN` are directly available inside popup commands. The kill binding uses both to switch to the queen before destroying the current cell.

---

## Part 2: The Four Keybindings

### 2a: Switch cell (`<prefix> H s`)

Opens a tmux popup running `hive switch`. Since `hive switch` already uses fzf internally, the popup becomes an fzf picker. When the user selects a cell, `hive switch` calls `switch-client` and the popup closes.

```tmux
bind-key -T hive s display-popup -E -w 60% -h 50% -T " Switch Cell " "hive switch"
```

**Behavior:**
- Popup opens with fzf showing all cells
- User picks a cell → tmux switches to that session → popup closes
- User presses `Esc` / `Ctrl-C` → fzf exits → popup closes → nothing happens

**Edge case:** `hive switch` uses `syscall.Exec` to replace the process with `tmux switch-client`. Inside a popup, this works correctly — the popup's process becomes the switch-client call, which executes and exits, closing the popup.

### 2b: Create cell (`<prefix> H c`)

Opens a popup that prompts for a cell name, then runs `hive cell <name>`. The popup needs to be interactive (read user input).

```tmux
bind-key -T hive c display-popup -E -w 60% -h 40% -T " Create Cell " \
  "read -p 'Cell name: ' name && [ -n \"$name\" ] && hive cell \"$name\" && echo 'Press Enter to close' && read"
```

**Behavior:**
1. Popup opens with prompt: `Cell name: `
2. User types a name and presses Enter
3. `hive cell <name>` runs — output shows worktree path, branch, ports, hooks, etc.
4. User sees the result, presses Enter to dismiss
5. If user presses `Ctrl-C` at the prompt → popup closes → nothing happens

**Why not auto-switch to the new cell?** The user may want to stay in their current cell after spawning a new one (e.g., for an agent). If they want to switch, `<prefix> H s` is one keystroke away.

**Edge case:** If the user is not in a git repo (e.g., a headless cell), `hive cell` will fail with an error. The error is displayed in the popup before the "press Enter" prompt.

### 2c: Kill current cell (`<prefix> H k`)

Reads `HIVE_CELL` from the environment, asks for confirmation, switches to the queen session first, then kills the cell. Switching to the queen before killing ensures the user lands in a known, safe session rather than getting disconnected or landing somewhere unpredictable.

This requires knowing the queen's session name. A new env var `HIVE_QUEEN` is injected into every cell's tmux session (see Part 2e below), containing the queen cell name (e.g., `myapp-queen`).

```tmux
bind-key -T hive k display-popup -E -w 50% -h 30% -T " Kill Cell " \
  "cell=$HIVE_CELL; queen=$HIVE_QUEEN; \
   [ -z \"$cell\" ] && echo 'Not in a Hive cell' && read && exit; \
   printf 'Kill cell \"%s\"? [y/N] ' \"$cell\"; read ans; \
   if [ \"$ans\" = 'y' ] || [ \"$ans\" = 'Y' ]; then \
     [ -n \"$queen\" ] && [ \"$cell\" != \"$queen\" ] && tmux switch-client -t \"$queen\"; \
     hive kill \"$cell\"; \
   else echo 'Cancelled'; fi; \
   echo && echo 'Press Enter to close'; read"
```

**Behavior:**
1. Popup reads `$HIVE_CELL` and `$HIVE_QUEEN`
2. If `HIVE_CELL` empty: shows "Not in a Hive cell", waits for Enter, closes
3. If set: shows confirmation prompt `Kill cell "myapp-feature"? [y/N]`
4. User types `y`:
   - If queen exists and this isn't the queen → `tmux switch-client -t <queen>` (lands in queen session)
   - `hive kill <name>` runs → cell is destroyed → popup closes
   - User is now in the queen session
5. User types anything else or `Ctrl-C` → "Cancelled" → closes

**Edge case — killing a queen:** `hive kill` already refuses to kill a queen with children. The error message shows in the popup. If it's the last cell and the queen can be killed, the switch-client step is skipped (since `$cell == $queen`), and tmux will disconnect after the session dies.

**Edge case — `HIVE_CELL` not set:** Possible if the user is in a tmux session not managed by Hive. The binding gracefully shows an error.

**Edge case — `HIVE_QUEEN` not set:** Possible for headless cells (no project, no queen). The binding skips the switch-client step and kills directly — tmux will switch to whatever session is next.

### 2d: Status dashboard (`<prefix> H d`)

Opens a popup showing `hive status` output. Read-only, press any key to close.

```tmux
bind-key -T hive d display-popup -E -w 80% -h 50% -T " Hive Status " \
  "hive status; echo; echo 'Press Enter to close'; read"
```

**Behavior:**
1. Popup opens showing the cell status table
2. User reads the output, presses Enter → popup closes

### 2e: New env var — `HIVE_QUEEN`

Inject `HIVE_QUEEN=<queen_cell_name>` into every normal cell's tmux session alongside the existing `HIVE_CELL` and `HIVE_QUEEN_DIR`. This is the queen's **cell name** (e.g., `myapp-queen`), which is also its tmux session name. Used by the kill binding (2c) to switch to a safe session before destroying the current cell.

In `cmd/cell.go`, where `HIVE_QUEEN_DIR` is already set:

```go
// Inject HIVE_QUEEN_DIR and HIVE_QUEEN if queen exists
queen, err := app.Repo.GetQueen(ctx, app.Project)
if err == nil && queen != nil {
    envVars["HIVE_QUEEN_DIR"] = queen.WorktreePath
    envVars["HIVE_QUEEN"] = queen.Name  // NEW
}
```

One extra line in the existing queen-env block. No new types or packages.

---

## Part 3: Generating `tmux.conf`

### What changes

`hive install` currently writes a placeholder comment to `~/.hive/tmux.conf`. Phase 6 replaces this with actual keybinding configuration. The file is also regenerated on demand via a new `hive keybindings` command.

### The generated file

```tmux
# Hive tmux configuration
# This file is managed by Hive. Regenerate with: hive keybindings
#
# Keybindings: <prefix> H, then:
#   s — switch cell (fzf picker)
#   c — create cell (name prompt)
#   k — kill current cell (with confirmation)
#   d — status dashboard

# Enter Hive key table
bind-key H switch-client -T hive

# Hive key table bindings
bind-key -T hive s display-popup -E -w 60% -h 50% -T " Switch Cell " "hive switch"
bind-key -T hive c display-popup -E -w 60% -h 40% -T " Create Cell " "read -p 'Cell name: ' name && [ -n \"$name\" ] && hive cell \"$name\" && echo && echo 'Press Enter to close' && read"
bind-key -T hive k display-popup -E -w 50% -h 30% -T " Kill Cell " "cell=$HIVE_CELL; queen=$HIVE_QUEEN; [ -z \"$cell\" ] && echo 'Not in a Hive cell' && read && exit; printf 'Kill cell \"%s\"? [y/N] ' \"$cell\"; read ans; if [ \"$ans\" = 'y' ] || [ \"$ans\" = 'Y' ]; then [ -n \"$queen\" ] && [ \"$cell\" != \"$queen\" ] && tmux switch-client -t \"$queen\"; hive kill \"$cell\"; else echo 'Cancelled'; fi; echo && echo 'Press Enter to close'; read"
bind-key -T hive d display-popup -E -w 80% -h 50% -T " Hive Status " "hive status; echo && echo 'Press Enter to close'; read"
```

### Hive binary path

The generated tmux.conf calls `hive` by name, relying on it being in `$PATH`. This is the simplest approach and matches how users already run Hive. If `hive` is not in PATH inside tmux, the user needs to fix their tmux environment (a common tmux setup issue, not Hive-specific).

### Config: customizing the leader key

The default leader is `H`. Users can change it via global config:

```bash
hive config apply --global -f - <<EOF
keybinding_leader: "F"
EOF
```

Stored in `global_config` under key `keybinding_leader`. Default: `H`.

This is the only customization offered initially. Individual binding keys (`s`, `c`, `k`, `d`) are not configurable — keep it simple, extend later if demanded.

### Data model

No changes to `ProjectConfig`. The leader key is a global preference stored in the `global_config` table:

| Key | Value | Default |
|-----|-------|---------|
| `keybinding_leader` | Single character | `H` |

### Generation logic

New package `internal/keybindings/keybindings.go`:

```go
package keybindings

// Generate returns the contents of the Hive tmux.conf file.
func Generate(leader string, tmuxVersion float64) string
```

The function is pure — takes the leader key and tmux version, returns the full file content as a string. No I/O. This makes it trivially testable. If tmux version < 3.2, bindings are commented out with a warning (see Part 6).

### Writing the file

Two places call `Generate` and write the result:

1. **`hive install`** — during initial setup (replaces the current placeholder)
2. **`hive keybindings`** — new command to regenerate on demand

Both write to `~/.hive/tmux.conf` and then tell tmux to reload:

```go
// After writing tmux.conf:
shell.Run(ctx, "tmux", "source-file", tmuxConfPath)
```

This makes keybindings available immediately without the user needing to restart tmux. The `source-file` call is best-effort — if tmux isn't running, it fails silently.

---

## Part 4: `hive keybindings` Command

### What it does

Regenerates `~/.hive/tmux.conf` and reloads it into tmux. Useful after changing the leader key or after updating Hive to a version with new/changed bindings.

### Command definition

```go
var keybindingsCmd = &cobra.Command{
    Use:   "keybindings",
    Short: "Regenerate tmux keybindings and reload",
    Args:  cobra.NoArgs,
    RunE: func(cmd *cobra.Command, args []string) error {
        // 1. Read leader from global_config (default "H")
        // 2. Generate tmux.conf content
        // 3. Write to ~/.hive/tmux.conf
        // 4. tmux source-file (best-effort)
        // 5. Print summary
    },
}
```

### CLI output

```
$ hive keybindings
Wrote ~/.hive/tmux.conf
Keybindings reloaded into tmux

  <prefix> H s — switch cell
  <prefix> H c — create cell
  <prefix> H k — kill current cell
  <prefix> H d — status dashboard

# With custom leader:
$ hive keybindings
Wrote ~/.hive/tmux.conf
Keybindings reloaded into tmux

  <prefix> F s — switch cell
  <prefix> F c — create cell
  <prefix> F k — kill current cell
  <prefix> F d — status dashboard
```

---

## Part 5: Changes to `hive install`

### Current behavior

`hive install` writes a placeholder comment to `~/.hive/tmux.conf` if the file doesn't exist. If it exists, it skips.

### New behavior

`hive install` always writes the generated keybindings to `~/.hive/tmux.conf`, overwriting the placeholder or any previous version. The file is marked as Hive-managed, so overwriting is expected.

```
1. Create ~/.hive/ directory       (existing)
2. Generate and write tmux.conf    ← CHANGED (was: write placeholder)
3. Source tmux.conf into tmux      ← NEW (best-effort)
4. Prompt for cells directory      (existing)
5. Prompt for projects directory   (existing)
6. Save installed_at               (existing)
```

The logic:

```go
// In install command, replace the placeholder write with:
leader, _ := app.ConfigRepo.Get(ctx, "keybinding_leader")
if leader == "" {
    leader = "H"
}
tmuxVer, _ := keybindings.TmuxVersion(ctx)
content := keybindings.Generate(leader, tmuxVer)
if err := os.WriteFile(tmuxConfPath, []byte(content), 0o644); err != nil {
    return fmt.Errorf("writing tmux.conf: %w", err)
}
fmt.Printf("Wrote %s\n", tmuxConfPath)

// Best-effort reload
shell.Run(ctx, "tmux", "source-file", tmuxConfPath)
```

For users who already have Hive installed, running `hive install` again (it already supports re-running) will upgrade their tmux.conf from placeholder to real keybindings.

---

## Part 6: Tmux Version Compatibility

### `display-popup` requirement

`tmux display-popup` was added in tmux 3.2 (released 2021). Users on older versions won't get working keybindings.

**Strategy:** Check tmux version at generation time. If tmux is < 3.2, generate the file with bindings commented out and a warning:

```tmux
# Hive tmux configuration
# WARNING: tmux 3.2+ required for popup keybindings. Current version: 3.1
# Upgrade tmux to enable keybindings.
#
# bind-key H switch-client -T hive
# ...
```

The version check:

```go
func TmuxVersion(ctx context.Context) (float64, error) {
    res, err := shell.Run(ctx, "tmux", "-V")
    // Parse "tmux 3.4" → 3.4
}
```

Add `TmuxVersion` to the `keybindings` package alongside `Generate`. If version < 3.2, `Generate` returns commented-out bindings with the warning. If version is unknown (tmux not installed), generate normally — the user is presumably installing tmux later.

---

## Implementation Order

### Step 1: Inject `HIVE_QUEEN` env var
- In `cmd/cell.go`, add `envVars["HIVE_QUEEN"] = queen.Name` alongside existing `HIVE_QUEEN_DIR` injection
- One-line change, no new packages or types

### Step 2: `internal/keybindings` package
- `Generate(leader string, tmuxVersion float64) string` — pure function returning tmux.conf content
- `TmuxVersion(ctx context.Context) (float64, error)` — parse `tmux -V` output
- Unit tests: test generated output for default leader, custom leader, and old tmux version

### Step 3: `hive keybindings` command
- New file `cmd/keybindings.go`
- Reads leader from `global_config`, calls `Generate`, writes file, reloads tmux
- Prints binding summary

### Step 4: Update `hive install`
- Replace placeholder tmux.conf write with `keybindings.Generate` call
- Add best-effort `tmux source-file` after write
- Existing users get real keybindings on next `hive install` run

### Step 5: Test keybindings manually in tmux
- Verify each binding works: switch, create, kill, status
- Test kill specifically: confirm it switches to queen before destroying the cell
- Test edge cases: no HIVE_CELL, queen kill protection, non-git session, headless cell (no queen)
- Test with custom leader key
- Verify popup sizing looks reasonable on different terminal sizes

### Step 6: Tests
- Unit tests for `keybindings.Generate` — content correctness, leader substitution, version gating
- Unit tests for `TmuxVersion` — parse various `tmux -V` outputs ("tmux 3.4", "tmux 3.2a", "tmux next-3.5")
