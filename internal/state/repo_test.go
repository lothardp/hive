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

func TestCellTypeDefaultsToNormal(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	cell := &Cell{Name: "no-type", Project: "p", Branch: "b", WorktreePath: "/tmp/x", Status: StatusRunning, Ports: "{}"}
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
		{"queen-cell", TypeQueen},
		{"headless-cell", TypeHeadless},
	} {
		cell := &Cell{
			Name:         tc.name,
			Project:      "proj",
			Branch:       "b",
			WorktreePath: "/tmp/" + tc.name,
			Status:       StatusStopped,
			Ports:        "{}",
			Type:         tc.cellType,
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

func TestGetQueen(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	// Create a normal cell and a queen for same project
	normal := &Cell{Name: "feat", Project: "myapp", Branch: "feat", WorktreePath: "/tmp/feat", Status: StatusStopped, Ports: "{}", Type: TypeNormal}
	queen := &Cell{Name: "myapp-queen", Project: "myapp", Branch: "main", WorktreePath: "/home/user/myapp", Status: StatusStopped, Ports: "{}", Type: TypeQueen}
	if err := repo.Create(ctx, normal); err != nil {
		t.Fatalf("creating normal: %v", err)
	}
	if err := repo.Create(ctx, queen); err != nil {
		t.Fatalf("creating queen: %v", err)
	}

	got, err := repo.GetQueen(ctx, "myapp")
	if err != nil {
		t.Fatalf("getting queen: %v", err)
	}
	if got == nil {
		t.Fatal("expected queen, got nil")
	}
	if got.Name != "myapp-queen" {
		t.Errorf("expected name myapp-queen, got %s", got.Name)
	}
	if got.Type != TypeQueen {
		t.Errorf("expected type queen, got %s", got.Type)
	}
}

func TestGetQueenNotFound(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	got, err := repo.GetQueen(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
}

func TestCountByProject(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	// Create queen + 2 normal cells for "myapp"
	for _, c := range []Cell{
		{Name: "myapp-queen", Project: "myapp", Branch: "main", WorktreePath: "/q", Status: StatusStopped, Ports: "{}", Type: TypeQueen},
		{Name: "feat-a", Project: "myapp", Branch: "a", WorktreePath: "/a", Status: StatusStopped, Ports: "{}", Type: TypeNormal},
		{Name: "feat-b", Project: "myapp", Branch: "b", WorktreePath: "/b", Status: StatusStopped, Ports: "{}", Type: TypeNormal},
	} {
		c := c
		if err := repo.Create(ctx, &c); err != nil {
			t.Fatalf("creating %s: %v", c.Name, err)
		}
	}

	// Excluding queen should give 2
	count, err := repo.CountByProject(ctx, "myapp", TypeQueen)
	if err != nil {
		t.Fatalf("counting: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 non-queen cells, got %d", count)
	}

	// Excluding normal should give 1 (the queen)
	count, err = repo.CountByProject(ctx, "myapp", TypeNormal)
	if err != nil {
		t.Fatalf("counting: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 non-normal cell, got %d", count)
	}

	// Different project should give 0
	count, err = repo.CountByProject(ctx, "other", TypeQueen)
	if err != nil {
		t.Fatalf("counting: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 for other project, got %d", count)
	}
}

func TestMigrationIdempotent(t *testing.T) {
	// Simulate opening DB twice (upgrade scenario)
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("first open: %v", err)
	}

	// runMigrations is called by Open, call it again to verify idempotency
	runMigrations(db)

	// Verify we can still create cells with type
	repo := NewCellRepository(db)
	ctx := context.Background()
	cell := &Cell{Name: "test", Project: "p", Branch: "b", WorktreePath: "/tmp/t", Status: StatusRunning, Ports: "{}", Type: TypeQueen}
	if err := repo.Create(ctx, cell); err != nil {
		t.Fatalf("creating cell after double migration: %v", err)
	}

	got, err := repo.GetByName(ctx, "test")
	if err != nil {
		t.Fatalf("getting cell: %v", err)
	}
	if got.Type != TypeQueen {
		t.Errorf("expected queen type, got %s", got.Type)
	}
	db.Close()
}

func TestListIncludesType(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	cells := []Cell{
		{Name: "queen", Project: "p", Branch: "main", WorktreePath: "/tmp/q", Status: StatusStopped, Ports: "{}", Type: TypeQueen},
		{Name: "normal", Project: "p", Branch: "feat", WorktreePath: "/tmp/n", Status: StatusStopped, Ports: "{}", Type: TypeNormal},
		{Name: "headless", Project: "", Branch: "", WorktreePath: "/tmp/h", Status: StatusStopped, Ports: "{}", Type: TypeHeadless},
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
	if len(list) != 3 {
		t.Fatalf("expected 3 cells, got %d", len(list))
	}

	typeMap := make(map[string]CellType)
	for _, c := range list {
		typeMap[c.Name] = c.Type
	}
	if typeMap["queen"] != TypeQueen {
		t.Errorf("queen: expected type queen, got %s", typeMap["queen"])
	}
	if typeMap["normal"] != TypeNormal {
		t.Errorf("normal: expected type normal, got %s", typeMap["normal"])
	}
	if typeMap["headless"] != TypeHeadless {
		t.Errorf("headless: expected type headless, got %s", typeMap["headless"])
	}
}
