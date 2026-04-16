# Fuzzy Cell Switcher

## Overview

A standalone `hive switch` command that opens a fuzzy-finder TUI for switching between active cells. Invokable from anywhere — a tmux keybinding, the shell, or a tmux popup — it lists all managed cells, lets the user type to filter, and navigates to the selected cell. Escape cancels cleanly.

This replaces the tmux-sessionizer pattern (`<prefix> f` → fzf over directories) with a Hive-native equivalent that understands cells, shows project grouping and status, and uses `tmux switch-client` for navigation — the same logic the dashboard already uses.

---

## Part 1: The `hive switch` Command

### What it is

A CLI command that launches a small Bubble Tea TUI for fuzzy-finding and switching to a cell. The TUI runs inline in the terminal (no alt-screen) — perfect for tmux popups. It:

1. Loads all cells from the DB
2. Checks tmux liveness for each
3. Displays a filterable list
4. On Enter: runs `tmux switch-client -t <name>` (same as the dashboard's `switchToSession`)
5. On Escape: exits cleanly with no action

### Why not use fzf?

fzf would require piping cell names through a shell script and loses context (project, type, status). A native Bubble Tea TUI can show richer information and is already the pattern used by the dashboard. It also avoids an external dependency.

### CLI definition

```go
// cmd/switch.go
var switchCmd = &cobra.Command{
    Use:   "switch",
    Short: "Fuzzy-find and switch to a cell",
    Args:  cobra.NoArgs,
    RunE:  switchRun,
}
```

No flags. No arguments. Just launch and pick.

### Behavior

```
$ hive switch
  hive-main                    ● 2d
  monolith-mlts-emails         ● 5h
  monolith-test         [h]    ● 1h
> monolith-sideci              ● 30m

  3/4  filter: mono█

  enter switch  esc cancel
```

- The `>` cursor highlights the selected item
- `[h]` marker for headless cells
- `●` green when tmux session is alive, `○` red when dead
- Age shown on the right
- Filter input at the bottom — type to narrow the list
- Item count shown: `3/4` means 3 matching out of 4 total

### Data loading

Reuse `cell.Service.List(ctx)` to get all cells from the DB, then check tmux liveness for each via `tmux.Manager.SessionExists()`. This is the same data the dashboard's cells tab loads.

```go
// In cmd/switch.go or a shared helper:

type switchItem struct {
    cell      state.Cell
    tmuxAlive bool
}

func loadSwitchItems(ctx context.Context) []switchItem {
    cells, err := app.CellRepo.List(ctx)
    // ... check tmux for each, build []switchItem
}
```

### Navigation

On Enter, use `shell.Run(ctx, "tmux", "switch-client", "-t", name)` — the same approach the dashboard uses in `switchToSession()`. This works inside tmux popups: the switch-client executes, and the popup's process exits, closing the popup.

**Not** `tmux.Manager.JoinSession()` — that uses `syscall.Exec` which replaces the process. Inside a popup, `switch-client` via `shell.Run` is the right call because the popup needs to exit cleanly after the switch.

However, if `$TMUX` is not set (user ran `hive switch` outside tmux), fall back to `JoinSession()` which does `tmux attach-session`.

```go
func switchToCell(ctx context.Context, name string) error {
    if os.Getenv("TMUX") != "" {
        _, err := shell.Run(ctx, "tmux", "switch-client", "-t", name)
        return err
    }
    return app.TmuxMgr.JoinSession(name)
}
```

### On Escape

The TUI exits with a zero exit code and no side effects. The popup closes. Nothing happens.

---

## Part 2: The Bubble Tea Model

### Architecture

A single-file TUI model in `internal/tui/switcher.go`. It's separate from the dashboard — the dashboard is a complex multi-tab app, while the switcher is a focused single-purpose picker.

### Model

```go
// internal/tui/switcher.go
package tui

type SwitchItem struct {
    Cell      state.Cell
    TmuxAlive bool
}

type SwitcherModel struct {
    items    []SwitchItem
    filtered []SwitchItem
    cursor   int
    filter   string
    selected string // cell name to switch to (read after quit)
    width    int
    height   int
}

func NewSwitcherModel(items []SwitchItem) SwitcherModel
func (m SwitcherModel) Init() tea.Cmd
func (m SwitcherModel) Update(msg tea.Msg) (tea.Model, tea.Cmd)
func (m SwitcherModel) View() string

// Selected returns the cell name the user picked, or "" if cancelled.
func (m SwitcherModel) Selected() string
```

### Key bindings

| Key | Action |
|-----|--------|
| `↑` / `k` / `ctrl+p` | Move cursor up |
| `↓` / `j` / `ctrl+n` | Move cursor down |
| `Enter` | Select and quit |
| `Escape` / `ctrl+c` | Cancel and quit |
| Any other rune | Append to filter |
| `Backspace` | Delete last filter char |

Note: `j`/`k` work as navigation because there's no multi-character text input — the filter is just rune-by-rune appending. If the user types "j", it filters by "j". This matches fzf behavior. To keep vim-style nav, use `ctrl+p`/`ctrl+n` instead while filtering. Actually, this creates a conflict: typing "j" should filter, not navigate.

**Resolution**: All printable characters go to the filter. Navigation is arrows + `ctrl+p`/`ctrl+n` only. This matches fzf's UX where typing always filters.

| Key | Action |
|-----|--------|
| `↑` / `ctrl+p` | Move cursor up |
| `↓` / `ctrl+n` | Move cursor down |
| `Enter` | Select and quit |
| `Escape` / `ctrl+c` | Cancel and quit |
| Any printable rune | Append to filter |
| `Backspace` | Delete last filter char |

### Filtering

Simple substring match on cell name (case-insensitive). Reset cursor to 0 on every filter change.

```go
func (m *SwitcherModel) applyFilter() {
    if m.filter == "" {
        m.filtered = m.items
        return
    }
    f := strings.ToLower(m.filter)
    m.filtered = nil
    for _, item := range m.items {
        if strings.Contains(strings.ToLower(item.Cell.Name), f) {
            m.filtered = append(m.filtered, item)
        }
    }
    m.cursor = 0
}
```

### View

The list is rendered bottom-up (selected item at the bottom, like fzf's default):

```
  hive-main                    ● 2d
  monolith-mlts-emails         ● 5h
  monolith-test         [h]    ● 1h
> monolith-sideci              ● 30m

  3/4  filter: mono█

  enter switch  esc cancel
```

Or with no filter:

```
  hive-main                    ● 2d
  monolith-mlts-emails         ● 5h
  monolith-test         [h]    ● 1h
> monolith-sideci              ● 30m

  4/4  █

  enter switch  esc cancel
```

The view reuses the same lipgloss styles from `internal/tui/styles.go` (`selectedStyle`, `statusAlive`, `statusDead`, `headlessStyle`, `helpStyle`, `dimStyle`).

Height-aware: if there are more items than fit, show a scrolling window around the cursor.

---

## Part 3: Tmux Keybinding

### What it does

Bind `<prefix> .` (or another key) to open `hive switch` in a tmux popup. This gives the same experience as the current `<prefix> f` → tmux-sessionizer, but Hive-native.

### Keybinding

Update `internal/keybindings/keybindings.go` to generate:

```tmux
# Hive — switch to dashboard
bind-key h switch-client -t hive

# Hive — fuzzy cell switcher
bind-key . display-popup -E -w 60% -h 50% -T " Switch Cell " "hive switch"
```

The `.` key is chosen because:
- It was previously `choose-tree -s` (commented out in the user's tmux.conf)
- It's easy to reach
- Mnemonic: "." as in "go to..."

### Generated tmux.conf

```go
func GenerateTmuxConf() string {
    return `# Hive — switch to dashboard
bind-key h switch-client -t hive

# Hive — fuzzy cell switcher
bind-key . display-popup -E -w 60% -h 50% -T " Switch Cell " "hive switch"
`
}
```

After implementation, run `hive install` to regenerate `~/.hive/tmux.conf` and reload.

---

## Part 4: Wiring into `cmd/switch.go`

### Full command

```go
func switchRun(cmd *cobra.Command, args []string) error {
    ctx := cmd.Context()

    // Load cells
    cells, err := app.CellRepo.List(ctx)
    if err != nil {
        return fmt.Errorf("listing cells: %w", err)
    }
    if len(cells) == 0 {
        fmt.Println("No cells.")
        return nil
    }

    // Check tmux liveness
    items := make([]tui.SwitchItem, len(cells))
    for i, c := range cells {
        alive, _ := app.TmuxMgr.SessionExists(ctx, c.Name)
        items[i] = tui.SwitchItem{Cell: c, TmuxAlive: alive}
    }

    // Run the picker TUI (no alt-screen — works in popups)
    m := tui.NewSwitcherModel(items)
    p := tea.NewProgram(m)
    result, err := p.Run()
    if err != nil {
        return fmt.Errorf("running switcher: %w", err)
    }

    // Check what was selected
    final := result.(tui.SwitcherModel)
    name := final.Selected()
    if name == "" {
        return nil // cancelled
    }

    // Navigate
    if os.Getenv("TMUX") != "" {
        res, err := shell.Run(ctx, "tmux", "switch-client", "-t", name)
        if err != nil {
            return fmt.Errorf("switching to cell: %w", err)
        }
        if res.ExitCode != 0 {
            return fmt.Errorf("switching to cell: %s", strings.TrimSpace(res.Stderr))
        }
        return nil
    }
    return app.TmuxMgr.JoinSession(name)
}
```

Note: no `tea.WithAltScreen()` — the TUI renders inline. This is important for tmux popups where alt-screen can cause rendering issues.

---

## CLI Output

### Normal use (inside tmux popup)

```
  hive-main                    ● 2d
  monolith-mlts-emails         ● 5h
> monolith-test         [h]    ● 1h

  3/3  █

  enter switch  esc cancel
```

User presses Enter → popup closes, tmux switches to `monolith-test`.

### With filter

```
> monolith-mlts-emails         ● 5h

  1/3  mlt█

  enter switch  esc cancel
```

### No cells

```
$ hive switch
No cells.
```

### Escape / cancel

TUI exits, popup closes, nothing happens.

---

## Implementation Order

### Step 1: `internal/tui/switcher.go`

Create the Bubble Tea model:
- `SwitchItem` struct
- `SwitcherModel` with filter, cursor, items
- `Init`, `Update`, `View`
- Filtering logic
- Reuse existing styles from `styles.go`
- No external dependencies beyond what's already in `go.mod`

### Step 2: `cmd/switch.go`

Create the command:
- Load cells from DB via `app.CellRepo.List()`
- Check tmux liveness for each
- Run `tui.NewSwitcherModel` → `tea.NewProgram` → `Run()`
- On selection: `tmux switch-client` (inside tmux) or `JoinSession` (outside)
- On cancel: exit cleanly

### Step 3: Update keybindings

Update `internal/keybindings/keybindings.go`:
- Add the `bind-key . display-popup` line to `GenerateTmuxConf()`
- Run `hive install` to regenerate and test

### Step 4: Test manually

- `hive switch` from a shell inside tmux → picker appears, select cell, switch works
- `<prefix> .` from any tmux pane → popup appears with picker, select, popup closes, switch works
- Escape in both contexts → clean exit, no action
- Empty cell list → "No cells." message
- Filter narrows results, cursor resets, selection works on filtered list
