package state

import "time"

type CellStatus string

const (
	StatusProvisioning CellStatus = "provisioning"
	StatusRunning      CellStatus = "running"
	StatusStopped      CellStatus = "stopped"
	StatusError        CellStatus = "error"
)

type Cell struct {
	ID           int64
	Name         string
	Project      string
	Branch       string
	WorktreePath string
	Status       CellStatus
	Ports        string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}
