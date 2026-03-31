package state

import "time"

type CellStatus string

const (
	StatusProvisioning CellStatus = "provisioning"
	StatusRunning      CellStatus = "running"
	StatusStopped      CellStatus = "stopped"
	StatusError        CellStatus = "error"
)

type CellType string

const (
	TypeNormal   CellType = "normal"
	TypeQueen    CellType = "queen"
	TypeHeadless CellType = "headless"
)

type Cell struct {
	ID           int64
	Name         string
	Project      string
	Branch       string
	WorktreePath string
	Status       CellStatus
	Ports        string
	Type         CellType
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Notification struct {
	ID        int64
	CellName  string
	Title     string
	Message   string
	Details   string
	Read      bool
	CreatedAt time.Time
}

type Repo struct {
	ID            int64
	Name          string
	Path          string
	RemoteURL     string
	DefaultBranch string
	Config        string // JSON blob of ProjectConfig
	CreatedAt     time.Time
	UpdatedAt     time.Time
}
