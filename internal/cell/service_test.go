package cell

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/lothardp/hive/internal/clone"
	"github.com/lothardp/hive/internal/shell"
	"github.com/lothardp/hive/internal/state"
	"github.com/lothardp/hive/internal/tmux"
)

// setupSourceRepo creates a minimal git repo in a temp directory for cloning.
func setupSourceRepo(t *testing.T) string {
	t.Helper()
	sourceDir := filepath.Join(t.TempDir(), "source-repo")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("creating source dir: %v", err)
	}

	ctx := context.Background()
	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
		{"git", "commit", "--allow-empty", "-m", "init"},
	}
	for _, args := range cmds {
		res, err := shell.RunInDir(ctx, sourceDir, args[0], args[1:]...)
		if err != nil {
			t.Fatalf("running %v: %v", args, err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("running %v: exit %d: %s", args, res.ExitCode, res.Stderr)
		}
	}
	return sourceDir
}

// newTestService creates a Service backed by an in-memory DB and temp directories.
func newTestService(t *testing.T) *Service {
	t.Helper()
	db, err := state.Open(":memory:")
	if err != nil {
		t.Fatalf("opening in-memory db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	cellsDir := filepath.Join(t.TempDir(), "cells")
	if err := os.MkdirAll(cellsDir, 0o755); err != nil {
		t.Fatalf("creating cells dir: %v", err)
	}

	hiveDir := filepath.Join(t.TempDir(), "hive")
	if err := os.MkdirAll(hiveDir, 0o755); err != nil {
		t.Fatalf("creating hive dir: %v", err)
	}

	return &Service{
		CellRepo: state.NewCellRepository(db),
		CloneMgr: clone.NewManager(cellsDir),
		TmuxMgr:  tmux.NewManager(),
		HiveDir:  hiveDir,
		DB:       db,
	}
}

func TestCreateAndKill(t *testing.T) {
	sourceDir := setupSourceRepo(t)
	svc := newTestService(t)
	ctx := context.Background()

	result, err := svc.Create(ctx, CreateOpts{
		Project:  "myproject",
		Name:     "feat-a",
		RepoPath: sourceDir,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if result.CellName != "myproject-feat-a" {
		t.Errorf("expected cell name %q, got %q", "myproject-feat-a", result.CellName)
	}

	// Verify clone directory exists.
	if _, err := os.Stat(result.ClonePath); err != nil {
		t.Errorf("clone path should exist: %v", err)
	}

	// Verify DB record exists.
	cell, err := svc.CellRepo.GetByName(ctx, "myproject-feat-a")
	if err != nil {
		t.Fatalf("GetByName: %v", err)
	}
	if cell == nil {
		t.Fatal("expected DB record to exist")
	}
	if cell.Status != state.StatusRunning {
		t.Errorf("expected status %q, got %q", state.StatusRunning, cell.Status)
	}
	if cell.Type != state.TypeNormal {
		t.Errorf("expected type %q, got %q", state.TypeNormal, cell.Type)
	}

	// Verify tmux session exists.
	exists, err := svc.TmuxMgr.SessionExists(ctx, "myproject-feat-a")
	if err != nil {
		t.Fatalf("SessionExists: %v", err)
	}
	if !exists {
		t.Error("expected tmux session to exist after Create")
	}

	// Now kill it.
	if err := svc.Kill(ctx, "myproject-feat-a"); err != nil {
		t.Fatalf("Kill: %v", err)
	}

	// Verify clone directory is gone.
	if _, err := os.Stat(result.ClonePath); !os.IsNotExist(err) {
		t.Error("expected clone directory to be removed after Kill")
	}

	// Verify DB record is gone.
	cell, err = svc.CellRepo.GetByName(ctx, "myproject-feat-a")
	if err != nil {
		t.Fatalf("GetByName after kill: %v", err)
	}
	if cell != nil {
		t.Error("expected DB record to be removed after Kill")
	}

	// Verify tmux session is gone.
	exists, err = svc.TmuxMgr.SessionExists(ctx, "myproject-feat-a")
	if err != nil {
		t.Fatalf("SessionExists after kill: %v", err)
	}
	if exists {
		t.Error("expected tmux session to be gone after Kill")
	}
}

func TestCreateDuplicate(t *testing.T) {
	sourceDir := setupSourceRepo(t)
	svc := newTestService(t)
	ctx := context.Background()

	_, err := svc.Create(ctx, CreateOpts{
		Project:  "proj",
		Name:     "dup",
		RepoPath: sourceDir,
	})
	if err != nil {
		t.Fatalf("first Create: %v", err)
	}
	t.Cleanup(func() {
		_ = svc.Kill(ctx, "proj-dup")
	})

	_, err = svc.Create(ctx, CreateOpts{
		Project:  "proj",
		Name:     "dup",
		RepoPath: sourceDir,
	})
	if err == nil {
		t.Fatal("expected error on duplicate create")
	}
}

func TestCreateHeadless(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	result, err := svc.CreateHeadless(ctx, HeadlessOpts{Name: "scratch"})
	if err != nil {
		t.Fatalf("CreateHeadless: %v", err)
	}
	t.Cleanup(func() {
		_ = svc.Kill(ctx, "scratch")
	})

	if result.CellName != "scratch" {
		t.Errorf("expected cell name %q, got %q", "scratch", result.CellName)
	}

	// Verify DB record exists with headless type.
	cell, err := svc.CellRepo.GetByName(ctx, "scratch")
	if err != nil {
		t.Fatalf("GetByName: %v", err)
	}
	if cell == nil {
		t.Fatal("expected DB record to exist")
	}
	if cell.Type != state.TypeHeadless {
		t.Errorf("expected type %q, got %q", state.TypeHeadless, cell.Type)
	}
	if cell.Status != state.StatusRunning {
		t.Errorf("expected status %q, got %q", state.StatusRunning, cell.Status)
	}

	// Verify tmux session exists.
	exists, err := svc.TmuxMgr.SessionExists(ctx, "scratch")
	if err != nil {
		t.Fatalf("SessionExists: %v", err)
	}
	if !exists {
		t.Error("expected tmux session to exist for headless cell")
	}
}

func TestKillNonexistent(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	err := svc.Kill(ctx, "does-not-exist")
	if err == nil {
		t.Fatal("expected error when killing nonexistent cell")
	}
}

func TestList(t *testing.T) {
	sourceDir := setupSourceRepo(t)
	svc := newTestService(t)
	ctx := context.Background()

	// List should be empty initially.
	cells, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(cells) != 0 {
		t.Errorf("expected 0 cells, got %d", len(cells))
	}

	// Create two cells.
	_, err = svc.Create(ctx, CreateOpts{
		Project:  "proj",
		Name:     "one",
		RepoPath: sourceDir,
	})
	if err != nil {
		t.Fatalf("Create one: %v", err)
	}
	t.Cleanup(func() { _ = svc.Kill(ctx, "proj-one") })

	_, err = svc.CreateHeadless(ctx, HeadlessOpts{Name: "two"})
	if err != nil {
		t.Fatalf("CreateHeadless two: %v", err)
	}
	t.Cleanup(func() { _ = svc.Kill(ctx, "two") })

	cells, err = svc.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(cells) != 2 {
		t.Errorf("expected 2 cells, got %d", len(cells))
	}
}
