package state

import (
	"context"
	"testing"
)

func setupTestDB(t *testing.T) *CellRepository {
	t.Helper()
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return NewCellRepository(db)
}

func TestCreateAndGetByName(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	cell := &Cell{
		Name:         "test-feature",
		Project:      "myapp",
		Branch:       "test-feature",
		WorktreePath: "/tmp/workspaces/myapp/test-feature",
		Status:       StatusProvisioning,
		Ports:        "{}",
	}

	if err := repo.Create(ctx, cell); err != nil {
		t.Fatalf("creating cell: %v", err)
	}
	if cell.ID == 0 {
		t.Fatal("expected non-zero ID after create")
	}

	got, err := repo.GetByName(ctx, "test-feature")
	if err != nil {
		t.Fatalf("getting cell: %v", err)
	}
	if got == nil {
		t.Fatal("expected cell, got nil")
	}
	if got.Name != "test-feature" {
		t.Errorf("expected name test-feature, got %s", got.Name)
	}
	if got.Project != "myapp" {
		t.Errorf("expected project myapp, got %s", got.Project)
	}
	if got.Status != StatusProvisioning {
		t.Errorf("expected status provisioning, got %s", got.Status)
	}
}

func TestGetByNameNotFound(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	got, err := repo.GetByName(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
}

func TestCreateDuplicateName(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	cell := &Cell{Name: "dup", Project: "p", Branch: "b", WorktreePath: "/tmp/a", Status: StatusRunning, Ports: "{}"}
	if err := repo.Create(ctx, cell); err != nil {
		t.Fatalf("first create: %v", err)
	}

	cell2 := &Cell{Name: "dup", Project: "p", Branch: "b", WorktreePath: "/tmp/b", Status: StatusRunning, Ports: "{}"}
	if err := repo.Create(ctx, cell2); err == nil {
		t.Fatal("expected error on duplicate name")
	}
}

func TestList(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	for _, name := range []string{"a", "b", "c"} {
		cell := &Cell{Name: name, Project: "p", Branch: name, WorktreePath: "/tmp/" + name, Status: StatusRunning, Ports: "{}"}
		if err := repo.Create(ctx, cell); err != nil {
			t.Fatalf("creating cell %s: %v", name, err)
		}
	}

	cells, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("listing cells: %v", err)
	}
	if len(cells) != 3 {
		t.Fatalf("expected 3 cells, got %d", len(cells))
	}
}

func TestListByStatus(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	for i, name := range []string{"running1", "stopped1", "running2"} {
		status := StatusRunning
		if i == 1 {
			status = StatusStopped
		}
		cell := &Cell{Name: name, Project: "p", Branch: name, WorktreePath: "/tmp/" + name, Status: status, Ports: "{}"}
		if err := repo.Create(ctx, cell); err != nil {
			t.Fatalf("creating cell: %v", err)
		}
	}

	running, err := repo.ListByStatus(ctx, StatusRunning)
	if err != nil {
		t.Fatalf("listing by status: %v", err)
	}
	if len(running) != 2 {
		t.Fatalf("expected 2 running cells, got %d", len(running))
	}

	stopped, err := repo.ListByStatus(ctx, StatusStopped)
	if err != nil {
		t.Fatalf("listing by status: %v", err)
	}
	if len(stopped) != 1 {
		t.Fatalf("expected 1 stopped cell, got %d", len(stopped))
	}
}

func TestUpdateStatus(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	cell := &Cell{Name: "s", Project: "p", Branch: "b", WorktreePath: "/tmp/s", Status: StatusProvisioning, Ports: "{}"}
	if err := repo.Create(ctx, cell); err != nil {
		t.Fatalf("creating cell: %v", err)
	}

	if err := repo.UpdateStatus(ctx, "s", StatusRunning); err != nil {
		t.Fatalf("updating status: %v", err)
	}

	got, _ := repo.GetByName(ctx, "s")
	if got.Status != StatusRunning {
		t.Errorf("expected running, got %s", got.Status)
	}
}

func TestUpdateStatusNotFound(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	if err := repo.UpdateStatus(ctx, "ghost", StatusRunning); err == nil {
		t.Fatal("expected error updating nonexistent cell")
	}
}

func TestDelete(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	cell := &Cell{Name: "del", Project: "p", Branch: "b", WorktreePath: "/tmp/del", Status: StatusRunning, Ports: "{}"}
	if err := repo.Create(ctx, cell); err != nil {
		t.Fatalf("creating cell: %v", err)
	}

	if err := repo.Delete(ctx, "del"); err != nil {
		t.Fatalf("deleting cell: %v", err)
	}

	got, _ := repo.GetByName(ctx, "del")
	if got != nil {
		t.Fatal("expected nil after delete")
	}
}

func TestDeleteNotFound(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	if err := repo.Delete(ctx, "ghost"); err == nil {
		t.Fatal("expected error deleting nonexistent cell")
	}
}
