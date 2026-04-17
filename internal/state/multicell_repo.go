package state

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type MulticellRepository struct {
	db *sql.DB
}

func NewMulticellRepository(db *sql.DB) *MulticellRepository {
	return &MulticellRepository{db: db}
}

func (r *MulticellRepository) AddChild(ctx context.Context, mc *MulticellChild) error {
	now := time.Now()
	result, err := r.db.ExecContext(ctx,
		`INSERT INTO multicell_children (multicell_name, project, clone_path, source_repo, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		mc.MulticellName, mc.Project, mc.ClonePath, mc.SourceRepo, now,
	)
	if err != nil {
		return fmt.Errorf("inserting multicell child: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("getting last insert id: %w", err)
	}
	mc.ID = id
	mc.CreatedAt = now
	return nil
}

func (r *MulticellRepository) ListChildren(ctx context.Context, multicellName string) ([]MulticellChild, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, multicell_name, project, clone_path, source_repo, created_at
		 FROM multicell_children WHERE multicell_name = ? ORDER BY project`,
		multicellName,
	)
	if err != nil {
		return nil, fmt.Errorf("listing multicell children: %w", err)
	}
	defer rows.Close()

	var children []MulticellChild
	for rows.Next() {
		var mc MulticellChild
		if err := rows.Scan(&mc.ID, &mc.MulticellName, &mc.Project, &mc.ClonePath, &mc.SourceRepo, &mc.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning multicell child: %w", err)
		}
		children = append(children, mc)
	}
	return children, rows.Err()
}

func (r *MulticellRepository) DeleteByMulticell(ctx context.Context, multicellName string) error {
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM multicell_children WHERE multicell_name = ?`,
		multicellName,
	)
	if err != nil {
		return fmt.Errorf("deleting multicell children: %w", err)
	}
	return nil
}
