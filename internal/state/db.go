package state

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS cells (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	name          TEXT UNIQUE NOT NULL,
	project       TEXT NOT NULL,
	branch        TEXT NOT NULL,
	worktree_path TEXT NOT NULL,
	status        TEXT NOT NULL DEFAULT 'provisioning',
	ports         TEXT NOT NULL DEFAULT '{}',
	created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_cells_status ON cells(status);
CREATE INDEX IF NOT EXISTS idx_cells_project ON cells(project);

CREATE TABLE IF NOT EXISTS notifications (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	cell_name  TEXT NOT NULL,
	message    TEXT NOT NULL,
	read       BOOLEAN NOT NULL DEFAULT 0,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	FOREIGN KEY (cell_name) REFERENCES cells(name) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS global_config (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS repos (
	id             INTEGER PRIMARY KEY AUTOINCREMENT,
	name           TEXT UNIQUE NOT NULL,
	path           TEXT UNIQUE NOT NULL,
	remote_url     TEXT NOT NULL DEFAULT '',
	default_branch TEXT NOT NULL DEFAULT 'main',
	config         TEXT NOT NULL DEFAULT '{}',
	created_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_repos_path ON repos(path);
`

func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(wal)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	runMigrations(db)

	return db, nil
}

func runMigrations(db *sql.DB) {
	stmts := []string{
		`ALTER TABLE cells ADD COLUMN type TEXT NOT NULL DEFAULT 'normal'`,
		`ALTER TABLE notifications ADD COLUMN title TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE notifications ADD COLUMN details TEXT NOT NULL DEFAULT ''`,
	}
	for _, stmt := range stmts {
		_, _ = db.Exec(stmt) // ignore "duplicate column" errors
	}
}
