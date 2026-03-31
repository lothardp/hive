package state

import (
	"context"
	"database/sql"
	"fmt"
)

type ConfigRepository struct {
	db *sql.DB
}

func NewConfigRepository(db *sql.DB) *ConfigRepository {
	return &ConfigRepository{db: db}
}

func (r *ConfigRepository) Get(ctx context.Context, key string) (string, error) {
	var value string
	err := r.db.QueryRowContext(ctx, `SELECT value FROM global_config WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("getting config %q: %w", key, err)
	}
	return value, nil
}

func (r *ConfigRepository) Set(ctx context.Context, key, value string) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO global_config (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = ?`,
		key, value, value,
	)
	if err != nil {
		return fmt.Errorf("setting config %q: %w", key, err)
	}
	return nil
}

func (r *ConfigRepository) Delete(ctx context.Context, key string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM global_config WHERE key = ?`, key)
	if err != nil {
		return fmt.Errorf("deleting config %q: %w", key, err)
	}
	return nil
}

func (r *ConfigRepository) All(ctx context.Context) (map[string]string, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT key, value FROM global_config`)
	if err != nil {
		return nil, fmt.Errorf("listing config: %w", err)
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, fmt.Errorf("scanning config row: %w", err)
		}
		result[key] = value
	}
	return result, rows.Err()
}
