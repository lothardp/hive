package state

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type RepoRepository struct {
	db *sql.DB
}

func NewRepoRepository(db *sql.DB) *RepoRepository {
	return &RepoRepository{db: db}
}

func (r *RepoRepository) Create(ctx context.Context, repo *Repo) error {
	now := time.Now()
	result, err := r.db.ExecContext(ctx,
		`INSERT INTO repos (name, path, remote_url, default_branch, config, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		repo.Name, repo.Path, repo.RemoteURL, repo.DefaultBranch, repo.Config, now, now,
	)
	if err != nil {
		return fmt.Errorf("inserting repo: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("getting last insert id: %w", err)
	}
	repo.ID = id
	repo.CreatedAt = now
	repo.UpdatedAt = now
	return nil
}

func (r *RepoRepository) GetByName(ctx context.Context, name string) (*Repo, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, name, path, remote_url, default_branch, config, created_at, updated_at
		 FROM repos WHERE name = ?`, name,
	)
	return scanRepo(row)
}

func (r *RepoRepository) GetByPath(ctx context.Context, path string) (*Repo, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, name, path, remote_url, default_branch, config, created_at, updated_at
		 FROM repos WHERE path = ?`, path,
	)
	return scanRepo(row)
}

func (r *RepoRepository) List(ctx context.Context) ([]Repo, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, path, remote_url, default_branch, config, created_at, updated_at
		 FROM repos ORDER BY name ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("listing repos: %w", err)
	}
	defer rows.Close()
	return scanRepos(rows)
}

func (r *RepoRepository) Update(ctx context.Context, repo *Repo) error {
	now := time.Now()
	result, err := r.db.ExecContext(ctx,
		`UPDATE repos SET name = ?, path = ?, remote_url = ?, default_branch = ?, config = ?, updated_at = ?
		 WHERE id = ?`,
		repo.Name, repo.Path, repo.RemoteURL, repo.DefaultBranch, repo.Config, now, repo.ID,
	)
	if err != nil {
		return fmt.Errorf("updating repo: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("repo %q not found", repo.Name)
	}
	repo.UpdatedAt = now
	return nil
}

func (r *RepoRepository) Delete(ctx context.Context, name string) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM repos WHERE name = ?`, name)
	if err != nil {
		return fmt.Errorf("deleting repo: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("repo %q not found", name)
	}
	return nil
}

func scanRepo(row *sql.Row) (*Repo, error) {
	var r Repo
	err := row.Scan(&r.ID, &r.Name, &r.Path, &r.RemoteURL, &r.DefaultBranch, &r.Config, &r.CreatedAt, &r.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scanning repo: %w", err)
	}
	return &r, nil
}

func scanRepos(rows *sql.Rows) ([]Repo, error) {
	var repos []Repo
	for rows.Next() {
		var r Repo
		if err := rows.Scan(&r.ID, &r.Name, &r.Path, &r.RemoteURL, &r.DefaultBranch, &r.Config, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning repo: %w", err)
		}
		repos = append(repos, r)
	}
	return repos, rows.Err()
}
