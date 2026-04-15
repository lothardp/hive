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
	clone_path    TEXT NOT NULL,
	status        TEXT NOT NULL DEFAULT 'running',
	ports         TEXT NOT NULL DEFAULT '{}',
	type          TEXT NOT NULL DEFAULT 'normal',
	created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_cells_status ON cells(status);
CREATE INDEX IF NOT EXISTS idx_cells_project ON cells(project);

CREATE TABLE IF NOT EXISTS notifications (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	cell_name  TEXT NOT NULL,
	title      TEXT NOT NULL DEFAULT '',
	message    TEXT NOT NULL,
	details    TEXT NOT NULL DEFAULT '',
	read       BOOLEAN NOT NULL DEFAULT 0,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	FOREIGN KEY (cell_name) REFERENCES cells(name) ON DELETE CASCADE
);
`

func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(wal)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("creating schema: %w", err)
	}

	return db, nil
}
