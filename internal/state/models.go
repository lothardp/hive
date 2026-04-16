package state

import "time"

type CellStatus string

const (
	StatusRunning CellStatus = "running"
	StatusStopped CellStatus = "stopped"
	StatusError   CellStatus = "error"
)

type CellType string

const (
	TypeNormal   CellType = "normal"
	TypeHeadless CellType = "headless"
)

type Cell struct {
	ID        int64
	Name      string
	Project   string
	ClonePath string
	Status    CellStatus
	Ports     string
	Type      CellType
	CreatedAt time.Time
	UpdatedAt time.Time
}

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
