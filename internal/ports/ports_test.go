package ports

import (
	"context"
	"testing"

	"github.com/lothardp/hive/internal/state"
)

func setupTestAllocator(t *testing.T) (*Allocator, *state.CellRepository) {
	t.Helper()
	db, err := state.Open(":memory:")
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return NewAllocator(db), state.NewCellRepository(db)
}

func TestAllocateEmptyDB(t *testing.T) {
	alloc, _ := setupTestAllocator(t)
	ctx := context.Background()

	ports, err := alloc.Allocate(ctx, []string{"PORT", "DB_PORT"})
	if err != nil {
		t.Fatalf("allocating ports: %v", err)
	}
	if ports["PORT"] != 3001 {
		t.Errorf("expected PORT=3001, got %d", ports["PORT"])
	}
	if ports["DB_PORT"] != 3002 {
		t.Errorf("expected DB_PORT=3002, got %d", ports["DB_PORT"])
	}
}

func TestAllocateAvoidsUsedPorts(t *testing.T) {
	alloc, repo := setupTestAllocator(t)
	ctx := context.Background()

	// Create a cell with some ports already allocated
	cell := &state.Cell{
		Name:         "existing",
		Project:      "p",
		Branch:       "b",
		WorktreePath: "/tmp/existing",
		Status:       state.StatusRunning,
		Ports:        `{"PORT":3001,"DB_PORT":3002}`,
	}
	if err := repo.Create(ctx, cell); err != nil {
		t.Fatalf("creating cell: %v", err)
	}

	ports, err := alloc.Allocate(ctx, []string{"PORT", "DB_PORT"})
	if err != nil {
		t.Fatalf("allocating ports: %v", err)
	}
	if ports["PORT"] != 3003 {
		t.Errorf("expected PORT=3003, got %d", ports["PORT"])
	}
	if ports["DB_PORT"] != 3004 {
		t.Errorf("expected DB_PORT=3004, got %d", ports["DB_PORT"])
	}
}

func TestAllocateRangeExhaustion(t *testing.T) {
	alloc, _ := setupTestAllocator(t)
	// Shrink range to make exhaustion testable
	alloc.start = 3001
	alloc.end = 3002
	ctx := context.Background()

	_, err := alloc.Allocate(ctx, []string{"A", "B", "C"})
	if err == nil {
		t.Fatal("expected error on range exhaustion")
	}
}

func TestAllocateNoVars(t *testing.T) {
	alloc, _ := setupTestAllocator(t)
	ctx := context.Background()

	ports, err := alloc.Allocate(ctx, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ports) != 0 {
		t.Errorf("expected empty map, got %v", ports)
	}
}
