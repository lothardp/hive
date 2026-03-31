# Phase 8: TUI Dashboard

## Overview

A tree-view terminal dashboard using Bubble Tea, accessed via `hive dashboard`. Displays cells nested under project names, similar to tmux's `choose-tree -s`. Queen cells are visually distinct. The user can navigate the tree and perform core actions: switch to a cell, kill a cell, view/dismiss notifications, and create new cells.

The goal is a simple, functional overview — not a full TUI app. Think tmux choose-tree, not a Bubble Tea showcase.

---

## Part 1: Tree View

### What it is

A vertical list of all cells, grouped under project headers. Each project is a collapsible-looking header (always expanded for now), and cells are indented beneath it. The cursor moves between cells; project headers are skipped.

### Visual layout

```
Hive Dashboard

▼ myproject
    ♛ myproject-queen           main              ● 3d
      myproject-feat-auth       feat-auth         ● 1h  (2 unread)
      myproject-feat-search     feat-search       ○ 5m
▼ otherproject
    ♛ otherproject-queen        main              ● 7d
    ◇ scratch                   -                 ● 2h

↑/↓ navigate  enter switch  x kill  n read notifs  c create  r refresh  q quit
```

Key visual elements:
- `▼` prefix on project headers (always expanded, decorative only)
- `♛` prefix for queen cells (yellow)
- `◇` prefix for headless cells (dim/gray)
- No prefix (just indent) for normal cells
- `●` green = tmux session alive, `○` red = dead
- Unread notification count in yellow at the end
- Help bar at the bottom

### Data loading

On startup (and on `r` refresh), load all data in one pass:

1. `cellRepo.List(ctx)` — all cells, ordered by `created_at DESC`
2. Group cells by `Project` field, sort project names alphabetically
3. For each cell:
   - `tmuxMgr.SessionExists(ctx, name)` — check tmux status
   - `notifRepo.CountUnread(ctx, name)` — unread notification count
4. Build a flat `[]row` slice where each row is either a project header or a cell entry

```go
type row struct {
    isProject bool        // true = project header, false = cell
    project   string      // project name (set on both types)
    cell      *state.Cell // nil for project headers
    tmuxAlive bool
    unread    int
}
```

### Styling (lipgloss)

Keep it minimal — no borders, no padding, just color:

| Element | Style |
|---------|-------|
| Project header | Bold, blue (color 4) |
| Queen cell | Yellow (color 3) |
| Normal cell | Default |
| Headless cell | Dim/gray (color 8) |
| Selected row | Bold, background highlight (color 8) |
| Alive indicator `●` | Green (color 2) |
| Dead indicator `○` | Red (color 1) |
| Unread count | Yellow bold (color 3) |
| Help bar | Dim/gray (color 8) |
| Confirm prompt | Red bold (color 1) |

---

## Part 2: Actions

### Switch (Enter)

When the user presses `Enter` on a cell row:

1. If the cell's tmux session is dead, recreate it: `tmuxMgr.CreateSession(ctx, name, cell.WorktreePath, nil)`
2. Store the cell name in `Model.SwitchTarget`
3. Quit the Bubble Tea program (`tea.Quit`)
4. Back in `cmd/dashboard.go`, after the program exits, call `tmuxMgr.JoinSession(name)` to switch/attach

This two-step approach is necessary because `JoinSession` calls `syscall.Exec` (replaces the process), which can't happen inside Bubble Tea's event loop.

### Kill (x)

When the user presses `x` on a cell row:

1. Show confirmation prompt at the bottom: `Kill "myproject-feat-auth"? (y/n)`
2. On `y`, execute the kill in a tea.Cmd (background):
   - **Queen safety**: if `cell.Type == TypeQueen`, check `cellRepo.CountByProject(ctx, project, TypeQueen)` — refuse if other cells exist
   - Kill tmux session: `tmuxMgr.KillSession(ctx, name)`
   - Normal cells only: remove worktree via `m.wtMgr.Remove(ctx, repoDir, cell.WorktreePath)`, delete branch via `m.wtMgr.DeleteBranch(ctx, repoDir, cell.Branch)` — both best-effort
   - Delete DB record: `cellRepo.Delete(ctx, name)`
3. On any other key, cancel
4. Show result message ("Killed X" or "Kill failed: ...") for 3 seconds, then reload cells

The kill logic mirrors `cmd/kill.go` but runs inside the TUI. The TUI imports `internal/worktree` directly — they're all internal packages in the same module.

### View Notifications (n)

When the user presses `n` on a cell with unread notifications:

1. Call `notifRepo.MarkReadByCell(ctx, cellName)` to mark all as read
2. Show a transient message: `Marked N notification(s) read for <cell>`
3. Reload the cell list (unread counts will update)

This is intentionally simple — just a "mark read" action. Full notification browsing stays in `hive notifications`.

### Create Cell (c)

When the user presses `c`:

1. Show a text input at the bottom: `Cell name: █`
2. User types a name, presses Enter
3. Show a message: `Run: hive cell <name>` (for 5 seconds)
4. Press Escape to cancel

Cell creation involves worktree setup, port allocation, hooks, and layouts — too much to replicate inside the TUI. Instead, we just tell the user what command to run. The dashboard is for overview and quick actions, not a full workflow replacement.

**Future**: shell out to `hive cell <name>` in a subprocess from within the TUI, or open a tmux popup.

### Refresh (r)

Reload all cell data. Same as the initial load.

---

## Part 3: Package Structure

### `internal/tui/dashboard.go`

The Bubble Tea model, view, and update logic. Single file — the TUI is simple enough.

```go
package tui

import (
    "database/sql"

    tea "github.com/charmbracelet/bubbletea"
    "github.com/lothardp/hive/internal/state"
    "github.com/lothardp/hive/internal/tmux"
    "github.com/lothardp/hive/internal/worktree"
)

// Model is the Bubble Tea model for the dashboard.
type Model struct {
    rows     []row
    cursor   int
    width    int
    height   int
    quitting bool

    // Dependencies
    cellRepo  *state.CellRepository
    notifRepo *state.NotificationRepository
    tmuxMgr   *tmux.Manager
    wtMgr     *worktree.Manager
    db        *sql.DB
    repoDir   string

    // UI state
    confirming   bool
    confirmName  string
    creating     bool
    createInput  string
    message      string

    // Output — read by cmd layer after tea.Quit
    SwitchTarget string
}

func NewModel(
    cellRepo *state.CellRepository,
    notifRepo *state.NotificationRepository,
    tmuxMgr *tmux.Manager,
    wtMgr *worktree.Manager,
    db *sql.DB,
    repoDir string,
) Model

func (m Model) Init() (tea.Model, tea.Cmd)
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd)
func (m Model) View() string
```

Key messages:

```go
type cellsLoaded struct{ rows []row }
type cellKilled struct{ name string }
type killFailed struct{ name string; err error }
type clearMsg struct{} // clears transient messages after a timeout
```

### Key bindings

```go
var keys = keyMap{
    Up:      key.NewBinding(key.WithKeys("up", "k")),
    Down:    key.NewBinding(key.WithKeys("down", "j")),
    Enter:   key.NewBinding(key.WithKeys("enter")),
    Kill:    key.NewBinding(key.WithKeys("x")),
    Notifs:  key.NewBinding(key.WithKeys("n")),
    Create:  key.NewBinding(key.WithKeys("c")),
    Refresh: key.NewBinding(key.WithKeys("r")),
    Quit:    key.NewBinding(key.WithKeys("q", "ctrl+c")),
}
```

---

## Part 4: `hive dashboard` Command

### `cmd/dashboard.go`

```go
var dashboardCmd = &cobra.Command{
    Use:   "dashboard",
    Short: "Interactive TUI overview of all cells",
    Args:  cobra.NoArgs,
    RunE: func(cmd *cobra.Command, args []string) error {
        m := tui.NewModel(
            app.Repo,
            app.NotifRepo,
            app.TmuxMgr,
            app.WtMgr,
            app.DB,
            app.RepoDir,
        )

        p := tea.NewProgram(m, tea.WithAltScreen())
        result, err := p.Run()
        if err != nil {
            return fmt.Errorf("running dashboard: %w", err)
        }

        // After TUI exits, check if user selected a cell to switch to
        if final, ok := result.(tui.Model); ok && final.SwitchTarget != "" {
            // Recreate session if needed
            exists, _ := app.TmuxMgr.SessionExists(cmd.Context(), final.SwitchTarget)
            if !exists {
                cell, err := app.Repo.GetByName(cmd.Context(), final.SwitchTarget)
                if err == nil && cell != nil {
                    _ = app.TmuxMgr.CreateSession(cmd.Context(), cell.Name, cell.WorktreePath, nil)
                }
            }
            return app.TmuxMgr.JoinSession(final.SwitchTarget)
        }

        return nil
    },
}

func init() {
    rootCmd.AddCommand(dashboardCmd)
}
```

### CLI usage

```
$ hive dashboard
# Opens the TUI in alt-screen mode
# Navigate with j/k, press Enter to switch, x to kill, etc.
# On quit, returns to the shell
# On Enter, replaces process with tmux attach/switch
```

---

## Implementation Order

### Step 1: Package and model scaffold
- Create `internal/tui/dashboard.go`
- Define `Model`, `row`, `NewModel`, message types
- Implement `Init` (loads cells), `Update` (navigation only), `View` (tree rendering)
- No actions yet — just display

### Step 2: `hive dashboard` command
- Create `cmd/dashboard.go`
- Wire up `NewModel` with app dependencies
- Run Bubble Tea program with `tea.WithAltScreen()`
- Test: `go build && ./hive dashboard` shows the tree

### Step 3: Switch action
- Implement Enter key → set `SwitchTarget` → `tea.Quit`
- In `cmd/dashboard.go`, after program exits, call `JoinSession`
- Handle dead session recreation

### Step 4: Kill action
- Implement `x` → confirmation → kill in background cmd
- Mirror `cmd/kill.go` logic with queen safety check
- Reload cell list after kill
- Transient success/error messages with auto-clear

### Step 5: Notification and create actions
- `n` key → mark read → reload → transient message
- `c` key → text input → show "run hive cell X" message

### Step 6: Polish
- Handle empty state ("No cells. Press c to create one.")
- Handle terminal resize (`tea.WindowSizeMsg`)
- Test inside and outside tmux
- Verify `go build` compiles cleanly
