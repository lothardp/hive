# Phase 7: Notifications

## Overview

A lightweight notification system that lets agents (or users) send messages from inside cells. Notifications are stored in the DB, shown via a list command, and trigger macOS system notifications. `hive jump` combines "mark as read" with "attach to cell" — the fast path for responding to an agent that needs attention.

The `notifications` table already exists in the schema. The three stub commands (`notify`, `notifications`, `jump`) already exist in `cmd/`. This phase fills them in and adds the supporting repository layer.

---

## Part 1: Notification Model & Repository

### Data model

Notifications have three text fields with distinct purposes:

- **`title`** — short headline (e.g., "Build complete"). Shown in the macOS notification banner title and in the `hive notifications` table.
- **`message`** — one-liner body (e.g., "All 42 tests passed"). Shown in the macOS notification banner body and in the `hive notifications` table.
- **`details`** — optional longer text for context (e.g., full test output, a URL, next steps). Only shown when viewing a specific notification or with `--detail` flag.

The split mirrors how macOS notifications work: title + message are the banner; details are what you see when you click through.

Add a `Notification` struct to `internal/state/models.go`:

```go
type Notification struct {
    ID        int64
    CellName  string
    Title     string
    Message   string
    Details   string
    Read      bool
    CreatedAt time.Time
}
```

### Schema migration

The existing `notifications` table has only a `message` column. Add `title` and `details` via `runMigrations` in `internal/state/db.go`:

```go
func runMigrations(db *sql.DB) {
    stmts := []string{
        `ALTER TABLE cells ADD COLUMN type TEXT NOT NULL DEFAULT 'normal'`,
        `ALTER TABLE notifications ADD COLUMN title TEXT NOT NULL DEFAULT ''`,   // NEW
        `ALTER TABLE notifications ADD COLUMN details TEXT NOT NULL DEFAULT ''`, // NEW
    }
    for _, stmt := range stmts {
        _, _ = db.Exec(stmt) // ignore "duplicate column" errors
    }
}
```

The existing `message` column stays as-is. After migration, all three text columns exist:
- `title` — defaults to `''` for pre-existing rows
- `message` — the original column, always populated
- `details` — defaults to `''`, optional

ON DELETE CASCADE on the foreign key means killing a cell automatically cleans up its notifications.

### Repository

New file `internal/state/notification_repo.go`:

```go
type NotificationRepository struct {
    db *sql.DB
}

func NewNotificationRepository(db *sql.DB) *NotificationRepository

// Create inserts a notification. Sets n.ID and n.CreatedAt on success.
func (r *NotificationRepository) Create(ctx context.Context, n *Notification) error

// List returns notifications ordered by created_at DESC.
// If cellName is non-empty, filters to that cell.
// If unreadOnly is true, filters to read = 0.
func (r *NotificationRepository) List(ctx context.Context, cellName string, unreadOnly bool) ([]Notification, error)

// GetByID returns a single notification by ID, or nil if not found.
func (r *NotificationRepository) GetByID(ctx context.Context, id int64) (*Notification, error)

// MarkReadByCell marks all notifications for a cell as read. Returns count of rows updated.
func (r *NotificationRepository) MarkReadByCell(ctx context.Context, cellName string) (int, error)

// CountUnread returns the number of unread notifications, optionally filtered by cell.
func (r *NotificationRepository) CountUnread(ctx context.Context, cellName string) (int, error)
```

**Query details:**

`Create`:

```sql
INSERT INTO notifications (cell_name, title, message, details, created_at)
VALUES (?, ?, ?, ?, ?)
```

`List` builds the query dynamically:

```sql
SELECT id, cell_name, title, message, details, read, created_at
FROM notifications
WHERE 1=1
  [AND cell_name = ?]   -- if cellName != ""
  [AND read = 0]         -- if unreadOnly
ORDER BY created_at DESC
```

`GetByID`:

```sql
SELECT id, cell_name, title, message, details, read, created_at
FROM notifications WHERE id = ?
```

`MarkReadByCell`:

```sql
UPDATE notifications SET read = 1 WHERE cell_name = ? AND read = 0
```

`CountUnread`:

```sql
SELECT COUNT(*) FROM notifications WHERE read = 0
  [AND cell_name = ?]   -- if cellName != ""
```

### Integration into App struct

Add `NotifRepo` to the `App` struct in `cmd/root.go`:

```go
type App struct {
    // ... existing fields ...
    NotifRepo  *state.NotificationRepository // NEW
}
```

Initialize in `PersistentPreRunE`, right after the other repositories:

```go
app.NotifRepo = state.NewNotificationRepository(db)
```

---

## Part 2: `hive notify`

### What it does

Sends a notification from the current cell. The cell is auto-detected from the `HIVE_CELL` environment variable (set by Hive in every tmux session). Stores the notification in the DB and fires a macOS notification via `osascript`.

### Cell detection

The command reads `os.Getenv("HIVE_CELL")` to determine which cell it's running in. If the env var is empty, the command fails with a clear error:

```
Error: not inside a Hive cell (HIVE_CELL not set)
```

The command also verifies the cell exists in the DB (it might have been killed while the shell was still open).

### Arguments and flags

The positional argument is the **message** (the short body). Two optional flags provide title and details:

```
hive notify [flags] <message>
```

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--title` | `-t` | `""` | Short headline for the notification |
| `--details` | `-d` | `""` | Longer context (viewable via `hive notifications <id>` or `--detail`) |

If `--title` is omitted, the macOS banner uses `"Hive: <cell_name>"` as the title. If `--title` is provided, the macOS banner uses `"Hive: <title>"` as the title and `<message>` as the subtitle.

### macOS notification

After storing the notification in the DB, fire a macOS notification using `osascript`:

```go
func sendSystemNotification(ctx context.Context, cellName, title, message string) error {
    bannerTitle := fmt.Sprintf("Hive: %s", cellName)
    if title != "" {
        bannerTitle = fmt.Sprintf("Hive: %s", title)
    }
    script := fmt.Sprintf(`display notification %q with title %q`, message, bannerTitle)
    _, err := shell.Run(ctx, "osascript", "-e", script)
    return err
}
```

The system notification is best-effort — if `osascript` fails (e.g., non-macOS, permissions), log a warning but don't fail the command. The DB record is the source of truth.

### CLI

```go
var notifyTitle string
var notifyDetails string

var notifyCmd = &cobra.Command{
    Use:   "notify <message>",
    Short: "Send a notification from the current cell",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        ctx := cmd.Context()
        message := args[0]

        cellName := os.Getenv("HIVE_CELL")
        if cellName == "" {
            return fmt.Errorf("not inside a Hive cell (HIVE_CELL not set)")
        }

        // Verify cell exists
        cell, err := app.Repo.GetByName(ctx, cellName)
        if err != nil {
            return fmt.Errorf("looking up cell: %w", err)
        }
        if cell == nil {
            return fmt.Errorf("cell %q not found in DB", cellName)
        }

        // Store notification
        notif := &state.Notification{
            CellName: cellName,
            Title:    notifyTitle,
            Message:  message,
            Details:  notifyDetails,
        }
        if err := app.NotifRepo.Create(ctx, notif); err != nil {
            return fmt.Errorf("creating notification: %w", err)
        }

        // Fire macOS notification (best-effort)
        if err := sendSystemNotification(ctx, cellName, notifyTitle, message); err != nil {
            slog.Warn("system notification failed", "error", err)
        }

        fmt.Printf("Notification sent from %s\n", cellName)
        return nil
    },
}

func init() {
    notifyCmd.Flags().StringVarP(&notifyTitle, "title", "t", "", "Short headline")
    notifyCmd.Flags().StringVarP(&notifyDetails, "details", "d", "", "Detailed context")
    rootCmd.AddCommand(notifyCmd)
}
```

### CLI output

```
$ hive notify "All 42 tests passed" -t "Build complete" -d "See full output at /tmp/test-results.log"
Notification sent from myapp-feature-x

# minimal — just a message:
$ hive notify "Done, ready for review"
Notification sent from myapp-feature-x

# error cases:
$ hive notify "hello"
Error: not inside a Hive cell (HIVE_CELL not set)

$ hive notify "hello"   # cell was killed
Error: cell "myapp-feature-x" not found in DB
```

---

## Part 3: `hive notifications`

### What it does

Two modes:

1. **List mode** (no args) — table of recent notifications showing title, message, status, age. Filterable by cell and read/unread.
2. **Detail mode** (`hive notifications <id>`) — full view of a single notification including details.

### Flags

```go
var notifCell string    // --cell, -c: filter by cell name
var notifUnread bool    // --unread, -u: show only unread
var notifDetail bool    // --detail: show details column in list mode
```

### CLI

```go
var notificationsCmd = &cobra.Command{
    Use:     "notifications [id]",
    Aliases: []string{"notifs"},
    Short:   "List recent notifications or view one in detail",
    Args:    cobra.MaximumNArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        ctx := cmd.Context()

        // Detail mode: view a single notification
        if len(args) == 1 {
            id, err := strconv.ParseInt(args[0], 10, 64)
            if err != nil {
                return fmt.Errorf("invalid notification ID: %s", args[0])
            }
            notif, err := app.NotifRepo.GetByID(ctx, id)
            if err != nil {
                return fmt.Errorf("looking up notification: %w", err)
            }
            if notif == nil {
                return fmt.Errorf("notification %d not found", id)
            }
            printNotificationDetail(notif)
            return nil
        }

        // List mode
        notifs, err := app.NotifRepo.List(ctx, notifCell, notifUnread)
        if err != nil {
            return fmt.Errorf("listing notifications: %w", err)
        }

        if len(notifs) == 0 {
            fmt.Println("No notifications.")
            return nil
        }

        w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)

        if notifDetail {
            fmt.Fprintln(w, "ID\tCELL\tTITLE\tMESSAGE\tDETAILS\tSTATUS\tAGE")
        } else {
            fmt.Fprintln(w, "ID\tCELL\tTITLE\tMESSAGE\tSTATUS\tAGE")
        }

        for _, n := range notifs {
            status := "unread"
            if n.Read {
                status = "read"
            }
            age := formatAge(time.Since(n.CreatedAt))

            title := n.Title
            if title == "" {
                title = "-"
            }
            msg := n.Message
            if len(msg) > 50 {
                msg = msg[:47] + "..."
            }

            if notifDetail {
                details := n.Details
                if details == "" {
                    details = "-"
                } else if len(details) > 50 {
                    details = details[:47] + "..."
                }
                fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\t%s\n",
                    n.ID, n.CellName, title, msg, details, status, age)
            } else {
                fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\n",
                    n.ID, n.CellName, title, msg, status, age)
            }
        }

        w.Flush()
        return nil
    },
}

func printNotificationDetail(n *state.Notification) {
    fmt.Printf("ID:      %d\n", n.ID)
    fmt.Printf("Cell:    %s\n", n.CellName)
    if n.Title != "" {
        fmt.Printf("Title:   %s\n", n.Title)
    }
    fmt.Printf("Message: %s\n", n.Message)
    if n.Details != "" {
        fmt.Printf("Details: %s\n", n.Details)
    }
    status := "unread"
    if n.Read {
        status = "read"
    }
    fmt.Printf("Status:  %s\n", status)
    fmt.Printf("Age:     %s\n", formatAge(time.Since(n.CreatedAt)))
}

func init() {
    notificationsCmd.Flags().StringVarP(&notifCell, "cell", "c", "", "Filter by cell name")
    notificationsCmd.Flags().BoolVarP(&notifUnread, "unread", "u", false, "Show only unread")
    notificationsCmd.Flags().BoolVar(&notifDetail, "detail", false, "Show details column in list")
    rootCmd.AddCommand(notificationsCmd)
}
```

### CLI output

**List mode (default):**

```
$ hive notifications
ID  CELL              TITLE           MESSAGE                            STATUS  AGE
3   myapp-feature-x   Build complete  All 42 tests passed                unread  2m
2   myapp-feature-x   -               Running tests...                   read    15m
1   myapp-bugfix      Deployed        Check staging at https://stag...   read    1h
```

**List mode with details:**

```
$ hive notifications --detail
ID  CELL              TITLE           MESSAGE              DETAILS                                     STATUS  AGE
3   myapp-feature-x   Build complete  All 42 tests passed  See full output at /tmp/test-results.log    unread  2m
2   myapp-feature-x   -               Running tests...     -                                           read    15m
```

**Filtered:**

```
$ hive notifications --unread
ID  CELL              TITLE           MESSAGE              STATUS  AGE
3   myapp-feature-x   Build complete  All 42 tests passed  unread  2m

$ hive notifications --cell myapp-bugfix
ID  CELL            TITLE     MESSAGE                            STATUS  AGE
1   myapp-bugfix    Deployed  Check staging at https://stag...   read    1h
```

**Detail mode (single notification):**

```
$ hive notifications 3
ID:      3
Cell:    myapp-feature-x
Title:   Build complete
Message: All 42 tests passed
Details: See full output at /tmp/test-results.log
Status:  unread
Age:     2m
```

**Empty:**

```
$ hive notifications --unread
No notifications.
```

---

## Part 4: `hive jump`

### What it does

Two things in one command:
1. Mark all notifications for the target cell as read
2. Attach to the cell's tmux session (same as `hive join`)

This is the "respond to notification" flow — you see a notification, you jump to the cell. The notification is automatically marked as read because you've acknowledged it by going there.

### CLI

```go
var jumpCmd = &cobra.Command{
    Use:               "jump <cell>",
    Short:             "Mark notifications read and attach to a cell",
    Args:              cobra.ExactArgs(1),
    ValidArgsFunction: completeCellNames,
    RunE: func(cmd *cobra.Command, args []string) error {
        ctx := cmd.Context()
        name := args[0]

        // Verify cell exists
        cell, err := app.Repo.GetByName(ctx, name)
        if err != nil {
            return fmt.Errorf("looking up cell: %w", err)
        }
        if cell == nil {
            return fmt.Errorf("cell %q not found", name)
        }

        // Mark notifications as read
        count, err := app.NotifRepo.MarkReadByCell(ctx, name)
        if err != nil {
            return fmt.Errorf("marking notifications read: %w", err)
        }
        if count > 0 {
            fmt.Printf("Marked %d notification(s) as read\n", count)
        }

        // Ensure tmux session exists (same logic as join)
        exists, err := app.TmuxMgr.SessionExists(ctx, name)
        if err != nil {
            return fmt.Errorf("checking tmux session: %w", err)
        }
        if !exists {
            if err := app.TmuxMgr.CreateSession(ctx, name, cell.WorktreePath, nil); err != nil {
                return fmt.Errorf("creating tmux session: %w", err)
            }
        }

        // This replaces the current process
        return app.TmuxMgr.JoinSession(name)
    },
}
```

### CLI output

```
$ hive jump myapp-feature-x
Marked 2 notification(s) as read
# (attaches to tmux session — process replaced)

# no unread notifications:
$ hive jump myapp-feature-x
# (attaches to tmux session silently)

# cell not found:
$ hive jump nonexistent
Error: cell "nonexistent" not found
```

### Relationship to `hive join`

`jump` and `join` both attach to a cell's tmux session. The difference:
- `join` — just attach
- `jump` — mark notifications read, then attach

They share the same session-exists/recreate/attach logic. The tmux attachment code in `join` doesn't need to be extracted into a helper — the duplication is small (5 lines) and the two commands may diverge later (e.g., `jump` might show a notification summary before attaching).

---

## Part 5: Unread Badge in `hive status`

### What it does

Show an unread notification count next to cell names in `hive status` output, so you can see at a glance which cells need attention.

### Display format

After the cell name and type annotation, append the unread count if > 0:

```
NAME                        PROJECT   BRANCH      STATUS   TMUX   PORTS  AGE
myapp-feature-x (2 unread)  myapp     feature-x   running  alive  3001   2h
myapp-bugfix                myapp     bugfix      running  alive  3002   1d
myapp [queen]               myapp     main        running  alive  -      3d
```

### Implementation

In `cmd/status.go`, after fetching all cells, batch-query unread counts:

```go
// Get unread counts for all cells in one query
unreadCounts := make(map[string]int)
for _, c := range cells {
    count, err := app.NotifRepo.CountUnread(ctx, c.Name)
    if err == nil && count > 0 {
        unreadCounts[c.Name] = count
    }
}
```

Then in the display loop:

```go
nameDisplay := c.Name
switch c.Type {
case state.TypeQueen:
    nameDisplay += " [queen]"
case state.TypeHeadless:
    nameDisplay += " [headless]"
}
if count, ok := unreadCounts[c.Name]; ok {
    nameDisplay += fmt.Sprintf(" (%d unread)", count)
}
```

**Performance note:** This makes N+1 queries (one per cell). For the expected scale (< 20 cells), this is fine. If it ever matters, replace with a single `GROUP BY cell_name` query returning all counts at once.

---

## Implementation Order

### Step 1: Schema migration and notification model

- Add `title` and `details` columns via `runMigrations` in `internal/state/db.go`
- Add `Notification` struct (with `Title`, `Message`, `Details`) to `internal/state/models.go`
- Create `internal/state/notification_repo.go` with `NotificationRepository`
- Implement `Create`, `List`, `GetByID`, `MarkReadByCell`, `CountUnread`
- Add `NotifRepo` to `App` struct and initialize in `PersistentPreRunE`
- Write unit tests against `:memory:` SQLite

### Step 2: `hive notify`

- Implement `cmd/notify.go` — read `HIVE_CELL`, store notification with title/message/details, fire macOS notification
- Add `--title`/`-t` and `--details`/`-d` flags
- Add `sendSystemNotification` helper (osascript, best-effort)

### Step 3: `hive notifications`

- Implement `cmd/notifications.go` — list with `--cell`, `--unread`, and `--detail` flags
- Detail mode: `hive notifications <id>` shows full notification including details
- Tabwriter output matching `hive status` style
- Add `notifs` alias

### Step 4: `hive jump`

- Implement `cmd/jump.go` — mark read + attach to tmux session
- Same session-exists/recreate pattern as `cmd/join.go`

### Step 5: Unread badge in `hive status`

- Query unread counts per cell
- Append `(N unread)` to name display in status table

### Step 6: Tests

- Unit tests for `NotificationRepository` (Create, List filtering, GetByID, MarkReadByCell, CountUnread)
- Unit tests for `sendSystemNotification` (mock shell, verify osascript args)
- Integration: `notify` with title/details → `notifications` shows in table → `notifications <id>` shows details → `jump` marks read
