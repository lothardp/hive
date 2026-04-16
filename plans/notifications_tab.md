# Notifications Tab

## Overview

A new fourth tab in the dashboard TUI for browsing, reading, and managing notifications. Currently notifications are only visible as unread counts on the cells tab — there's no way to read their content, see details, or clean up old ones from the dashboard. This tab makes notifications a first-class part of the TUI.

The tab shows two sections: **Unread** notifications at the top and **Read** notifications below. The user can navigate the list, view full details inline, mark individual notifications as read, jump to the source cell, and clean up (delete) all read notifications.

---

## Part 1: New Repository Methods

### What's needed

The existing `NotificationRepository` has `Create`, `List`, `GetByID`, `MarkReadByCell`, and `CountUnread`. The notifications tab needs two new operations:

1. **Mark a single notification as read** — the existing `MarkReadByCell` marks all for a cell; we need per-notification granularity.
2. **Delete read notifications** — for the "clean up" action.

### New methods on `NotificationRepository`

Add to `internal/state/notification_repo.go`:

```go
// MarkRead marks a single notification as read by ID.
func (r *NotificationRepository) MarkRead(ctx context.Context, id int64) error {
    result, err := r.db.ExecContext(ctx,
        `UPDATE notifications SET read = 1 WHERE id = ? AND read = 0`, id,
    )
    if err != nil {
        return fmt.Errorf("marking notification read: %w", err)
    }
    n, err := result.RowsAffected()
    if err != nil {
        return fmt.Errorf("checking rows affected: %w", err)
    }
    if n == 0 {
        return fmt.Errorf("notification %d not found or already read", id)
    }
    return nil
}

// DeleteRead deletes all read notifications. Returns count of rows deleted.
func (r *NotificationRepository) DeleteRead(ctx context.Context) (int, error) {
    result, err := r.db.ExecContext(ctx,
        `DELETE FROM notifications WHERE read = 1`,
    )
    if err != nil {
        return 0, fmt.Errorf("deleting read notifications: %w", err)
    }
    n, err := result.RowsAffected()
    if err != nil {
        return 0, fmt.Errorf("checking rows affected: %w", err)
    }
    return int(n), nil
}
```

No schema changes needed — the existing `notifications` table has everything.

---

## Part 2: The Notifications Tab Model

### File

New file: `internal/tui/notifications.go`

### Model

```go
type NotifsModel struct {
    unread  []state.Notification
    read    []state.Notification
    cursor  int      // index into the flat list (unread header + unread items + read header + read items)
    message string

    // Dependencies
    notifRepo *state.NotificationRepository
    tmuxMgr   *tmux.Manager
}
```

### Flat list structure

The view is a flat list of rows, similar to how the cells tab has project headers + cell rows. The structure:

```
  Unread (3)
  ──────────────────────────────────────────────────
    ● Build complete          hive-main          2m
      All 42 tests passed
    ● Deploy failed           monolith-work      15m
      Error: connection refused to staging
    ● Tests running           monolith-work      1h
      Started full suite...
  Read (2)
  ──────────────────────────────────────────────────
    ○ Build complete          hive-main          3h
      All tests passed
    ○ Setup done              monolith-fix       1d
      npm install completed
```

Each notification takes **2 lines**: the first shows status indicator + title (or "Notification" if no title) + cell name + age, the second shows the message truncated to fit the width. If the notification has `details`, a third line shows the details in dim text.

The cursor moves over notification items (skipping section headers).

### Row types

```go
type notifRow struct {
    isHeader     bool
    headerText   string
    notification *state.Notification
}
```

Build the flat list from `unread` and `read` slices:

```go
func (m NotifsModel) buildRows() []notifRow {
    var rows []notifRow
    rows = append(rows, notifRow{isHeader: true, headerText: fmt.Sprintf("Unread (%d)", len(m.unread))})
    for i := range m.unread {
        rows = append(rows, notifRow{notification: &m.unread[i]})
    }
    rows = append(rows, notifRow{isHeader: true, headerText: fmt.Sprintf("Read (%d)", len(m.read))})
    for i := range m.read {
        rows = append(rows, notifRow{notification: &m.read[i]})
    }
    return rows
}
```

### Data loading

```go
type notifsLoaded struct {
    unread []state.Notification
    read   []state.Notification
}

func (m NotifsModel) LoadNotifs() tea.Cmd {
    return func() tea.Msg {
        ctx := context.Background()
        unread, _ := m.notifRepo.List(ctx, "", true)   // unreadOnly=true
        all, _ := m.notifRepo.List(ctx, "", false)      // all
        // Separate read from all (all is sorted DESC, so order is preserved)
        var read []state.Notification
        for _, n := range all {
            if n.Read {
                read = append(read, n)
            }
        }
        return notifsLoaded{unread: unread, read: read}
    }
}
```

### Key bindings

| Key | Action | When |
|-----|--------|------|
| `j` / `↓` | Move cursor down | Always |
| `k` / `↑` | Move cursor up | Always |
| `enter` | Jump to source cell (`tmux switch-client`) | On a notification row |
| `m` | Mark selected notification as read | On an unread notification |
| `M` | Mark all unread as read | Always (in this tab) |
| `D` | Delete all read notifications | Always (with confirmation) |
| `r` | Refresh | Always |

```go
type notifKeyMap struct {
    Up      key.Binding
    Down    key.Binding
    Enter   key.Binding
    Mark    key.Binding
    MarkAll key.Binding
    Clean   key.Binding
    Refresh key.Binding
}

var notifKeys = notifKeyMap{
    Up:      key.NewBinding(key.WithKeys("up", "k")),
    Down:    key.NewBinding(key.WithKeys("down", "j")),
    Enter:   key.NewBinding(key.WithKeys("enter")),
    Mark:    key.NewBinding(key.WithKeys("m")),
    MarkAll: key.NewBinding(key.WithKeys("M")),
    Clean:   key.NewBinding(key.WithKeys("D")),
    Refresh: key.NewBinding(key.WithKeys("r")),
}
```

### Actions

#### Jump to cell (Enter)

Read `notification.CellName`, run `tmux switch-client -t <cellName>` — the same `switchToSession()` helper already used by the cells tab.

#### Mark read (m)

On an unread notification: call `notifRepo.MarkRead(ctx, id)`, then reload. Show transient message "Marked as read".

#### Mark all read (M)

For each unique cell name in unread list, call `notifRepo.MarkReadByCell(ctx, cellName)`. Then reload. Show "Marked N notification(s) as read".

#### Clean up read (D)

Show confirmation inline: `Delete all read notifications? (y/n)`. On `y`, call `notifRepo.DeleteRead(ctx)`. Show "Deleted N notification(s)". On any other key, cancel.

### View

```go
func (m NotifsModel) View(width int) string
```

Renders the two-section list. Each notification row:

**Line 1**: `    ● Title                    cell-name          age`
- `●` green for unread, `○` dim for read
- Title in bold for unread, dim for read. Falls back to `"(no title)"` if empty
- Cell name right-aligned or in a fixed column
- Age far right

**Line 2**: `      Message text truncated to width...`
- Indented, showing the message body
- Truncated with `...` if too long

**Line 3** (optional, only if `details` is non-empty):
- `      Details text in dim...`

Selected row gets `selectedStyle` background highlight (same as cells tab).

### Footer

```
enter jump  m mark read  M mark all  D clean up  r refresh  h/l tabs  q quit
```

During confirmation:
```
Delete all read notifications? (y/n)
```

### Cursor behavior

Skip header rows (same `skipToNotif` pattern as `skipToCell` in the cells tab). If no notifications exist, show:

```
  No notifications.
```

---

## Part 3: Dashboard Integration

### Tab registration

Update `internal/tui/dashboard.go`:

```go
const (
    tabCells    = 0
    tabProjects = 1
    tabConfig   = 2
    tabNotifs   = 3  // NEW
)

var tabNames = []string{"Cells", "Projects", "Config", "Notifs"}
```

### Model field

Add to `Model` struct:

```go
type Model struct {
    // ... existing fields ...
    notifs    NotifsModel  // NEW
}
```

### Constructor

In `NewModel`, add:

```go
return Model{
    cells:     NewCellsModel(svc, notifRepo, tmuxMgr),
    projects:  NewProjectsModel(hiveDir, editor),
    configTab: NewConfigModel(hiveDir, editor),
    notifs:    NewNotifsModel(notifRepo, tmuxMgr),  // NEW
    // ...
}
```

### Init

Add to `Init()`:

```go
func (m Model) Init() tea.Cmd {
    return tea.Batch(
        m.cells.LoadCells(),
        m.projects.LoadProjects(m.globalCfg),
        m.configTab.LoadConfig(),
        m.notifs.LoadNotifs(),  // NEW
    )
}
```

### Message routing

In the `Update` method, add to the data message routing:

```go
case notifsLoaded, notifMarked, notifMarkFailed, notifsCleaned, notifsCleanFailed:
    m.notifs, cmd = m.notifs.Update(msg)
    return m, cmd
```

Add to the active-tab key routing:

```go
case tabNotifs:
    m.notifs, cmd = m.notifs.Update(msg)
```

### Footer and content in View

Add cases for `tabNotifs` in the View content switch:

```go
case tabNotifs:
    content = m.notifs.View(m.width)
```

And footer:

```go
case tabNotifs:
    footer = m.notifs.Footer()
```

### Scroll support

Add to `cursorContentLine()`:

```go
case tabNotifs:
    return m.notifs.cursorLine()
```

The `cursorLine()` method on `NotifsModel` returns the cursor's position in the flat row list, accounting for multi-line notification rendering (each notification is 2-3 lines, headers are 2 lines with separator).

### Tab bar width

The `renderHeader()` currently hardcodes the tab bar width calculation. Update the gap calculation to account for the new tab name:

```go
// In renderHeader:
rawTabText := strings.Join(tabNames, "  ")
gap := m.width - len("Hive Dashboard") - len(rawTabText) - 4
```

---

## Part 4: Messages

```go
// internal/tui/notifications.go

type notifsLoaded struct {
    unread []state.Notification
    read   []state.Notification
}

type notifMarked struct{ id int64 }
type notifMarkFailed struct {
    id  int64
    err error
}

type notifsCleaned struct{ count int }
type notifsCleanFailed struct{ err error }
```

---

## Part 5: Notification Rendering Detail

### Two-line format

```
    ● Build complete              hive-main        2m
      All 42 tests passed
```

Column layout:
- Col 1 (4 chars): indent + indicator (`    ●` or `    ○`)
- Col 2 (variable, ~25 chars): title or "(no title)"
- Col 3 (variable, ~20 chars): cell name
- Col 4 (right): age
- Line 2: 6-char indent + message (truncated)
- Line 3 (optional): 6-char indent + details in dim (truncated)

### Selected highlight

When cursor is on a notification, all its lines (1-3) get `selectedStyle` background. This means the cursor logic needs to know how many lines each notification occupies.

Simpler approach: highlight only line 1 (the title line). Lines 2-3 are always rendered in their normal style. This avoids complex multi-line selection tracking and matches how the cells tab works (one line per item).

### Empty state

```
  No notifications.
```

### Section headers

```
  Unread (3)
  ──────────────────────────────────────────────────
```

Header is bold blue (like project headers in cells tab). Separator is the same `─` character used in the projects tab.

---

## Implementation Order

### Step 1: Repository methods

- Add `MarkRead(ctx, id)` to `NotificationRepository`
- Add `DeleteRead(ctx)` to `NotificationRepository`
- Add unit tests for both in `notification_repo_test.go`

### Step 2: `NotifsModel`

- Create `internal/tui/notifications.go`
- `NotifsModel` struct, `NewNotifsModel`, `LoadNotifs`
- `Update` with navigation (j/k, skip headers)
- `View` with two-section rendering
- `Footer` with help text
- Mark read (m), mark all (M), clean up (D) with confirmation
- Jump to cell (Enter) reusing `switchToSession`

### Step 3: Dashboard integration

- Add `tabNotifs = 3` constant, update `tabNames`
- Add `notifs NotifsModel` to `Model`
- Wire into `NewModel`, `Init`, `Update` (message routing + key routing), `View`, `cursorContentLine`
- Update `renderHeader` gap calculation

### Step 4: Test manually

- Send notifications via `hive notify` from cells
- Open dashboard, switch to Notifs tab
- Verify: notifications appear in correct sections, navigation works, mark read moves item to read section, jump switches to cell, clean up deletes read, empty state renders correctly
