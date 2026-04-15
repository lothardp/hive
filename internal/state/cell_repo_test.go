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
		Name:      "test-feature",
		Project:   "myapp",
		ClonePath: "/tmp/workspaces/myapp/test-feature",
		Status:    StatusRunning,
		Ports:     "{}",
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
	if got.Status != StatusRunning {
		t.Errorf("expected status running, got %s", got.Status)
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

	cell := &Cell{Name: "dup", Project: "p", ClonePath: "/tmp/a", Status: StatusRunning, Ports: "{}"}
	if err := repo.Create(ctx, cell); err != nil {
		t.Fatalf("first create: %v", err)
	}

	cell2 := &Cell{Name: "dup", Project: "p", ClonePath: "/tmp/b", Status: StatusRunning, Ports: "{}"}
	if err := repo.Create(ctx, cell2); err == nil {
		t.Fatal("expected error on duplicate name")
	}
}

func TestList(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	for _, name := range []string{"a", "b", "c"} {
		cell := &Cell{Name: name, Project: "p", ClonePath: "/tmp/" + name, Status: StatusRunning, Ports: "{}"}
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
		cell := &Cell{Name: name, Project: "p", ClonePath: "/tmp/" + name, Status: status, Ports: "{}"}
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

func TestDelete(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	cell := &Cell{Name: "del", Project: "p", ClonePath: "/tmp/del", Status: StatusRunning, Ports: "{}"}
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

func TestCellTypeDefaultsToNormal(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	cell := &Cell{Name: "no-type", Project: "p", ClonePath: "/tmp/x", Status: StatusRunning, Ports: "{}"}
	if err := repo.Create(ctx, cell); err != nil {
		t.Fatalf("creating cell: %v", err)
	}

	got, err := repo.GetByName(ctx, "no-type")
	if err != nil {
		t.Fatalf("getting cell: %v", err)
	}
	if got.Type != TypeNormal {
		t.Errorf("expected type %q, got %q", TypeNormal, got.Type)
	}
}

func TestCellTypePersistedThroughCreateScan(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	for _, tc := range []struct {
		name     string
		cellType CellType
	}{
		{"normal-cell", TypeNormal},
		{"headless-cell", TypeHeadless},
	} {
		cell := &Cell{
			Name:      tc.name,
			Project:   "proj",
			ClonePath: "/tmp/" + tc.name,
			Status:    StatusStopped,
			Ports:     "{}",
			Type:      tc.cellType,
		}
		if err := repo.Create(ctx, cell); err != nil {
			t.Fatalf("creating %s: %v", tc.name, err)
		}

		got, err := repo.GetByName(ctx, tc.name)
		if err != nil {
			t.Fatalf("getting %s: %v", tc.name, err)
		}
		if got.Type != tc.cellType {
			t.Errorf("%s: expected type %q, got %q", tc.name, tc.cellType, got.Type)
		}
	}
}

func TestCountByProject(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	for _, c := range []Cell{
		{Name: "feat-a", Project: "myapp", ClonePath: "/a", Status: StatusRunning, Ports: "{}", Type: TypeNormal},
		{Name: "feat-b", Project: "myapp", ClonePath: "/b", Status: StatusStopped, Ports: "{}", Type: TypeNormal},
		{Name: "headless-c", Project: "myapp", ClonePath: "/c", Status: StatusRunning, Ports: "{}", Type: TypeHeadless},
	} {
		c := c
		if err := repo.Create(ctx, &c); err != nil {
			t.Fatalf("creating %s: %v", c.Name, err)
		}
	}

	count, err := repo.CountByProject(ctx, "myapp")
	if err != nil {
		t.Fatalf("counting: %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3 cells, got %d", count)
	}

	// Different project should give 0
	count, err = repo.CountByProject(ctx, "other")
	if err != nil {
		t.Fatalf("counting: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 for other project, got %d", count)
	}
}

func TestListIncludesType(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	cells := []Cell{
		{Name: "normal", Project: "p", ClonePath: "/tmp/n", Status: StatusStopped, Ports: "{}", Type: TypeNormal},
		{Name: "headless", Project: "", ClonePath: "/tmp/h", Status: StatusStopped, Ports: "{}", Type: TypeHeadless},
	}
	for i := range cells {
		if err := repo.Create(ctx, &cells[i]); err != nil {
			t.Fatalf("creating %s: %v", cells[i].Name, err)
		}
	}

	list, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 cells, got %d", len(list))
	}

	typeMap := make(map[string]CellType)
	for _, c := range list {
		typeMap[c.Name] = c.Type
	}
	if typeMap["normal"] != TypeNormal {
		t.Errorf("normal: expected type normal, got %s", typeMap["normal"])
	}
	if typeMap["headless"] != TypeHeadless {
		t.Errorf("headless: expected type headless, got %s", typeMap["headless"])
	}
}
