# Notification Pane Targeting

## Overview

When jumping to a notification from the dashboard, navigate to the exact tmux pane that sent it — not just the session. Currently `switchToSession` uses `tmux switch-client -t <session>`, which lands on whichever window/pane was last active. This feature captures the source pane at send time and targets it at jump time.

The tmux pane ID (`$TMUX_PANE`, e.g. `%91`) is globally unique, stable for the lifetime of the pane, and tmux's `switch-client -t %91` resolves it to the correct session, window, and pane in one command. If the pane is gone (session restarted, pane closed), we fall back to the session name.

---

## Part 1: Schema Migration

### New column

Add `source_pane` to the `notifications` table via `db.go`. This stores the tmux pane ID (e.g. `%91`). Empty string means "no pane info" (backward compatible with existing rows).

```sql
ALTER TABLE notifications ADD COLUMN source_pane TEXT NOT NULL DEFAULT '';
```

Add to `Open()` in `internal/state/db.go`, after the schema creation:

```go
func Open(path string) (*sql.DB, error) {
    // ... existing schema exec ...

    // Migrations
    migrations := []string{
        `ALTER TABLE notifications ADD COLUMN source_pane TEXT NOT NULL DEFAULT ''`,
    }
    for _, m := range migrations {
        _, _ = db.Exec(m) // ignore "duplicate column" errors
    }

    return db, nil
}
```

---

## Part 2: Model Update

### Notification struct

Add `SourcePane` to `internal/state/models.go`:

```go
type Notification struct {
    ID         int64
    CellName   string
    Title      string
    Message    string
    Details    string
    Read       bool
    SourcePane string // tmux pane ID, e.g. "%91"
    CreatedAt  time.Time
}
```

---

## Part 3: Repository Updates

### All queries that read notifications

Every `SELECT` and `Scan` in `notification_repo.go` must include `source_pane`. There are three query methods:

**`List`** — the main query:
```sql
SELECT id, cell_name, title, message, details, read, source_pane, created_at
FROM notifications WHERE ...
```

**`GetByID`** — same column addition:
```sql
SELECT id, cell_name, title, message, details, read, source_pane, created_at
FROM notifications WHERE id = ?
```

**`Create`** — add `source_pane` to the INSERT:
```sql
INSERT INTO notifications (cell_name, title, message, details, source_pane, created_at)
VALUES (?, ?, ?, ?, ?, ?)
```

Each corresponding `Scan` call adds `&n.SourcePane` after `&n.Read`.

The `MarkRead`, `MarkReadByCell`, `DeleteRead`, and `CountUnread` methods don't read notification columns — no changes needed.

---

## Part 4: Capture Pane at Send Time

### `cmd/notify.go`

Read `$TMUX_PANE` from the environment and store it on the notification:

```go
// In the RunE function, after building the Notification struct:
notif := &state.Notification{
    CellName:   cellName,
    Title:      title,
    Message:    message,
    Details:    details,
    SourcePane: os.Getenv("TMUX_PANE"), // e.g. "%91", empty if not in tmux
}
```

This works for both the normal `hive notify "msg"` path and the `--from-claude` path. `$TMUX_PANE` is set by tmux in every pane's environment — it's already there, no tmux queries needed.

---

## Part 5: Jump to Pane

### `internal/tui/cells.go` — `switchToSession`

Currently:

```go
func switchToSession(name string) tea.Cmd {
    return func() tea.Msg {
        ctx := context.Background()
        _, _ = shell.Run(ctx, "tmux", "switch-client", "-t", name)
        return cellSwitched{}
    }
}
```

Add a new function that targets a pane, falling back to session:

```go
func switchToPane(sessionName, paneID string) tea.Cmd {
    return func() tea.Msg {
        ctx := context.Background()
        // Try pane ID first (e.g. "%91") — resolves to session:window.pane
        if paneID != "" {
            res, err := shell.Run(ctx, "tmux", "switch-client", "-t", paneID)
            if err == nil && res.ExitCode == 0 {
                return cellSwitched{}
            }
            // Pane gone — fall back to session
        }
        _, _ = shell.Run(ctx, "tmux", "switch-client", "-t", sessionName)
        return cellSwitched{}
    }
}
```

### `internal/tui/notifications.go` — Enter key handler

Change the jump action from `switchToSession(r.CellName)` to `switchToPane(r.CellName, r.SourcePane)`:

```go
case key.Matches(msg, notifKeys.Enter):
    if r := m.selectedNotif(); r != nil {
        return m, switchToPane(r.CellName, r.SourcePane)
    }
    return m, nil
```

The cells tab continues using `switchToSession` unchanged — pane targeting only applies to notification jumps.

---

## Implementation Order

### Step 1: Schema migration + model
- Add `source_pane` column migration in `internal/state/db.go`
- Add `SourcePane` field to `Notification` in `internal/state/models.go`

### Step 2: Repository queries
- Update `Create` INSERT to include `source_pane`
- Update `List` and `GetByID` SELECTs and Scans to include `source_pane`
- Update existing tests if any scan notifications

### Step 3: Capture pane in `cmd/notify.go`
- Read `os.Getenv("TMUX_PANE")` and set `notif.SourcePane`
- One-line change, works for both normal and `--from-claude` paths

### Step 4: Jump to pane in TUI
- Add `switchToPane(sessionName, paneID)` in `internal/tui/cells.go`
- Update notifications tab Enter handler to use `switchToPane`

### Step 5: Test manually
- Send notification from a specific pane: `hive notify "test" -t "pane test"`
- Open dashboard, go to Notifs tab, press Enter
- Verify: lands on the exact pane, not just the session
- Kill the source pane, try again — verify: falls back to the session
- Send notification from `--from-claude` path — verify: pane captured
- Old notifications (no `source_pane`) — verify: falls back to session
