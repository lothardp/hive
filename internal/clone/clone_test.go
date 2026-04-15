package clone

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/lothardp/hive/internal/shell"
)

func setupSourceRepo(t *testing.T) string {
	t.Helper()
	sourceDir := filepath.Join(t.TempDir(), "source-repo")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("creating source dir: %v", err)
	}

	ctx := context.Background()

	for _, args := range []struct {
		name string
		args []string
	}{
		{"git", []string{"init"}},
		{"git", []string{"config", "user.email", "test@test.com"}},
		{"git", []string{"config", "user.name", "Test"}},
		{"git", []string{"commit", "--allow-empty", "-m", "init"}},
	} {
		res, err := shell.RunInDir(ctx, sourceDir, args.name, args.args...)
		if err != nil {
			t.Fatalf("running %s %v: %v", args.name, args.args, err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("running %s %v: exit %d: %s", args.name, args.args, res.ExitCode, res.Stderr)
		}
	}

	return sourceDir
}

func TestClone(t *testing.T) {
	sourceDir := setupSourceRepo(t)
	cellsDir := t.TempDir()
	mgr := NewManager(cellsDir)
	ctx := context.Background()

	clonePath, err := mgr.Clone(ctx, sourceDir, "myproject", "feat-a")
	if err != nil {
		t.Fatalf("cloning: %v", err)
	}

	expected := filepath.Join(cellsDir, "myproject", "feat-a")
	if clonePath != expected {
		t.Errorf("expected path %s, got %s", expected, clonePath)
	}

	// Verify .git exists in the clone
	if _, err := os.Stat(filepath.Join(clonePath, ".git")); err != nil {
		t.Errorf(".git not found in clone: %v", err)
	}

	// Verify git status works in the clone
	res, err := shell.RunInDir(ctx, clonePath, "git", "status")
	if err != nil {
		t.Errorf("git status in clone failed: %v", err)
	} else if res.ExitCode != 0 {
		t.Errorf("git status in clone failed: exit %d: %s", res.ExitCode, res.Stderr)
	}
}

func TestCloneDuplicatePath(t *testing.T) {
	sourceDir := setupSourceRepo(t)
	cellsDir := t.TempDir()
	mgr := NewManager(cellsDir)
	ctx := context.Background()

	_, err := mgr.Clone(ctx, sourceDir, "proj", "feat")
	if err != nil {
		t.Fatalf("first clone: %v", err)
	}

	_, err = mgr.Clone(ctx, sourceDir, "proj", "feat")
	if err == nil {
		t.Fatal("expected error on duplicate clone path")
	}
}

func TestRemove(t *testing.T) {
	sourceDir := setupSourceRepo(t)
	cellsDir := t.TempDir()
	mgr := NewManager(cellsDir)
	ctx := context.Background()

	clonePath, err := mgr.Clone(ctx, sourceDir, "proj", "feat")
	if err != nil {
		t.Fatalf("cloning: %v", err)
	}

	if err := mgr.Remove(clonePath); err != nil {
		t.Fatalf("removing clone: %v", err)
	}

	if _, err := os.Stat(clonePath); !os.IsNotExist(err) {
		t.Error("expected clone directory to be removed")
	}
}

func TestRemoveSafety(t *testing.T) {
	cellsDir := t.TempDir()
	mgr := NewManager(cellsDir)

	outsidePath := filepath.Join(t.TempDir(), "not-a-cell")
	if err := os.MkdirAll(outsidePath, 0o755); err != nil {
		t.Fatalf("creating outside dir: %v", err)
	}

	err := mgr.Remove(outsidePath)
	if err == nil {
		t.Fatal("expected error when removing path outside cells directory")
	}
	if _, statErr := os.Stat(outsidePath); statErr != nil {
		t.Error("outside directory should still exist after failed remove")
	}
}

func TestExists(t *testing.T) {
	sourceDir := setupSourceRepo(t)
	cellsDir := t.TempDir()
	mgr := NewManager(cellsDir)
	ctx := context.Background()

	clonePath, err := mgr.Clone(ctx, sourceDir, "proj", "feat")
	if err != nil {
		t.Fatalf("cloning: %v", err)
	}

	if !mgr.Exists(clonePath) {
		t.Error("expected Exists to return true for existing clone")
	}

	nonExistent := filepath.Join(cellsDir, "proj", "does-not-exist")
	if mgr.Exists(nonExistent) {
		t.Error("expected Exists to return false for non-existing path")
	}
}
