package state

import (
	"context"
	"testing"
)

func setupConfigRepo(t *testing.T) *ConfigRepository {
	t.Helper()
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return NewConfigRepository(db)
}

func TestConfigSetAndGet(t *testing.T) {
	repo := setupConfigRepo(t)
	ctx := context.Background()

	if err := repo.Set(ctx, "projects_dir", "/home/user/projects"); err != nil {
		t.Fatalf("setting config: %v", err)
	}

	val, err := repo.Get(ctx, "projects_dir")
	if err != nil {
		t.Fatalf("getting config: %v", err)
	}
	if val != "/home/user/projects" {
		t.Errorf("expected /home/user/projects, got %s", val)
	}
}

func TestConfigGetNotFound(t *testing.T) {
	repo := setupConfigRepo(t)
	ctx := context.Background()

	val, err := repo.Get(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "" {
		t.Errorf("expected empty string, got %s", val)
	}
}

func TestConfigSetOverwrite(t *testing.T) {
	repo := setupConfigRepo(t)
	ctx := context.Background()

	if err := repo.Set(ctx, "key", "v1"); err != nil {
		t.Fatalf("first set: %v", err)
	}
	if err := repo.Set(ctx, "key", "v2"); err != nil {
		t.Fatalf("second set: %v", err)
	}

	val, _ := repo.Get(ctx, "key")
	if val != "v2" {
		t.Errorf("expected v2, got %s", val)
	}
}

func TestConfigDelete(t *testing.T) {
	repo := setupConfigRepo(t)
	ctx := context.Background()

	_ = repo.Set(ctx, "key", "val")
	if err := repo.Delete(ctx, "key"); err != nil {
		t.Fatalf("deleting config: %v", err)
	}

	val, _ := repo.Get(ctx, "key")
	if val != "" {
		t.Errorf("expected empty after delete, got %s", val)
	}
}

func TestConfigAll(t *testing.T) {
	repo := setupConfigRepo(t)
	ctx := context.Background()

	_ = repo.Set(ctx, "a", "1")
	_ = repo.Set(ctx, "b", "2")

	all, err := repo.All(ctx)
	if err != nil {
		t.Fatalf("listing config: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(all))
	}
	if all["a"] != "1" || all["b"] != "2" {
		t.Errorf("unexpected values: %v", all)
	}
}
