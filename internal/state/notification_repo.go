package state

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type NotificationRepository struct {
	db *sql.DB
}

func NewNotificationRepository(db *sql.DB) *NotificationRepository {
	return &NotificationRepository{db: db}
}

// Create inserts a notification. Sets n.ID and n.CreatedAt on success.
func (r *NotificationRepository) Create(ctx context.Context, n *Notification) error {
	now := time.Now()
	result, err := r.db.ExecContext(ctx,
		`INSERT INTO notifications (cell_name, title, message, details, source_pane, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		n.CellName, n.Title, n.Message, n.Details, n.SourcePane, now,
	)
	if err != nil {
		return fmt.Errorf("inserting notification: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("getting last insert id: %w", err)
	}
	n.ID = id
	n.CreatedAt = now
	return nil
}

// List returns notifications ordered by created_at DESC.
// If cellName is non-empty, filters to that cell.
// If unreadOnly is true, filters to read = 0.
func (r *NotificationRepository) List(ctx context.Context, cellName string, unreadOnly bool) ([]Notification, error) {
	query := `SELECT id, cell_name, title, message, details, read, source_pane, created_at FROM notifications WHERE 1=1`
	var args []any

	if cellName != "" {
		query += ` AND cell_name = ?`
		args = append(args, cellName)
	}
	if unreadOnly {
		query += ` AND read = 0`
	}
	query += ` ORDER BY created_at DESC`

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing notifications: %w", err)
	}
	defer rows.Close()

	var notifs []Notification
	for rows.Next() {
		var n Notification
		if err := rows.Scan(&n.ID, &n.CellName, &n.Title, &n.Message, &n.Details, &n.Read, &n.SourcePane, &n.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning notification: %w", err)
		}
		notifs = append(notifs, n)
	}
	return notifs, rows.Err()
}

// GetByID returns a single notification by ID, or nil if not found.
func (r *NotificationRepository) GetByID(ctx context.Context, id int64) (*Notification, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, cell_name, title, message, details, read, source_pane, created_at
		 FROM notifications WHERE id = ?`, id,
	)
	var n Notification
	err := row.Scan(&n.ID, &n.CellName, &n.Title, &n.Message, &n.Details, &n.Read, &n.SourcePane, &n.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scanning notification: %w", err)
	}
	return &n, nil
}

// MarkReadByCell marks all notifications for a cell as read. Returns count of rows updated.
func (r *NotificationRepository) MarkReadByCell(ctx context.Context, cellName string) (int, error) {
	result, err := r.db.ExecContext(ctx,
		`UPDATE notifications SET read = 1 WHERE cell_name = ? AND read = 0`, cellName,
	)
	if err != nil {
		return 0, fmt.Errorf("marking notifications read: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("checking rows affected: %w", err)
	}
	return int(n), nil
}

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

// CountUnread returns the number of unread notifications, optionally filtered by cell.
func (r *NotificationRepository) CountUnread(ctx context.Context, cellName string) (int, error) {
	query := `SELECT COUNT(*) FROM notifications WHERE read = 0`
	var args []any

	if cellName != "" {
		query += ` AND cell_name = ?`
		args = append(args, cellName)
	}

	var count int
	err := r.db.QueryRowContext(ctx, query, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("counting unread notifications: %w", err)
	}
	return count, nil
}
