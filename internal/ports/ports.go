package ports

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// Allocator assigns unique ports per cell from a fixed range.
type Allocator struct {
	db    *sql.DB
	start int
	end   int
}

func NewAllocator(db *sql.DB) *Allocator {
	return &Allocator{
		db:    db,
		start: 3001,
		end:   9999,
	}
}

// Allocate finds unused ports for the given variable names.
func (a *Allocator) Allocate(ctx context.Context, varNames []string) (map[string]int, error) {
	used, err := a.usedPorts(ctx)
	if err != nil {
		return nil, err
	}

	result := make(map[string]int, len(varNames))
	next := a.start
	for _, name := range varNames {
		for next <= a.end && used[next] {
			next++
		}
		if next > a.end {
			return nil, fmt.Errorf("no available ports in range %d-%d", a.start, a.end)
		}
		result[name] = next
		used[next] = true
		next++
	}
	return result, nil
}

// usedPorts returns a set of all ports currently allocated to any cell.
func (a *Allocator) usedPorts(ctx context.Context) (map[int]bool, error) {
	rows, err := a.db.QueryContext(ctx, `SELECT ports FROM cells WHERE ports != '{}'`)
	if err != nil {
		return nil, fmt.Errorf("querying cell ports: %w", err)
	}
	defer rows.Close()

	used := make(map[int]bool)
	for rows.Next() {
		var portsJSON string
		if err := rows.Scan(&portsJSON); err != nil {
			return nil, fmt.Errorf("scanning ports: %w", err)
		}
		var portMap map[string]int
		if err := json.Unmarshal([]byte(portsJSON), &portMap); err != nil {
			continue // skip malformed entries
		}
		for _, port := range portMap {
			used[port] = true
		}
	}
	return used, rows.Err()
}
