package state

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type CellRepository struct {
	db *sql.DB
}

func NewCellRepository(db *sql.DB) *CellRepository {
	return &CellRepository{db: db}
}

func (r *CellRepository) Create(ctx context.Context, cell *Cell) error {
	now := time.Now()
	result, err := r.db.ExecContext(ctx,
		`INSERT INTO cells (name, project, branch, worktree_path, status, ports, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		cell.Name, cell.Project, cell.Branch, cell.WorktreePath, cell.Status, cell.Ports, now, now,
	)
	if err != nil {
		return fmt.Errorf("inserting cell: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("getting last insert id: %w", err)
	}
	cell.ID = id
	cell.CreatedAt = now
	cell.UpdatedAt = now
	return nil
}

func (r *CellRepository) GetByName(ctx context.Context, name string) (*Cell, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, name, project, branch, worktree_path, status, ports, created_at, updated_at
		 FROM cells WHERE name = ?`, name,
	)
	return scanCell(row)
}

func (r *CellRepository) List(ctx context.Context) ([]Cell, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, project, branch, worktree_path, status, ports, created_at, updated_at
		 FROM cells ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("listing cells: %w", err)
	}
	defer rows.Close()
	return scanCells(rows)
}

func (r *CellRepository) ListByStatus(ctx context.Context, status CellStatus) ([]Cell, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, project, branch, worktree_path, status, ports, created_at, updated_at
		 FROM cells WHERE status = ? ORDER BY created_at DESC`, status,
	)
	if err != nil {
		return nil, fmt.Errorf("listing cells by status: %w", err)
	}
	defer rows.Close()
	return scanCells(rows)
}

func (r *CellRepository) UpdateStatus(ctx context.Context, name string, status CellStatus) error {
	result, err := r.db.ExecContext(ctx,
		`UPDATE cells SET status = ?, updated_at = ? WHERE name = ?`,
		status, time.Now(), name,
	)
	if err != nil {
		return fmt.Errorf("updating cell status: %w", err)
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

func (r *CellRepository) Delete(ctx context.Context, name string) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM cells WHERE name = ?`, name)
	if err != nil {
		return fmt.Errorf("deleting cell: %w", err)
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

func scanCell(row *sql.Row) (*Cell, error) {
	var c Cell
	err := row.Scan(&c.ID, &c.Name, &c.Project, &c.Branch, &c.WorktreePath, &c.Status, &c.Ports, &c.CreatedAt, &c.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scanning cell: %w", err)
	}
	return &c, nil
}

func scanCells(rows *sql.Rows) ([]Cell, error) {
	var cells []Cell
	for rows.Next() {
		var c Cell
		if err := rows.Scan(&c.ID, &c.Name, &c.Project, &c.Branch, &c.WorktreePath, &c.Status, &c.Ports, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning cell: %w", err)
		}
		cells = append(cells, c)
	}
	return cells, rows.Err()
}
