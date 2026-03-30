package worktree

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func setupTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// Initialize a git repo with one commit
	for _, args := range [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
	}

	// Create a file and commit
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"git", "add", "."},
		{"git", "commit", "-m", "initial"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
	}

	return dir
}

func TestCreateAndRemove(t *testing.T) {
	repoDir := setupTestRepo(t)
	baseDir := t.TempDir()
	mgr := NewManager(baseDir)
	ctx := context.Background()

	wtPath, err := mgr.Create(ctx, repoDir, "testproject", "feat-a", "feat-a")
	if err != nil {
		t.Fatalf("creating worktree: %v", err)
	}

	expected := filepath.Join(baseDir, "testproject", "feat-a")
	if wtPath != expected {
		t.Errorf("expected path %s, got %s", expected, wtPath)
	}

	// Verify worktree exists and is valid
	if _, err := os.Stat(filepath.Join(wtPath, "README.md")); err != nil {
		t.Errorf("README.md not found in worktree: %v", err)
	}

	// Verify git status works in worktree
	cmd := exec.Command("git", "status")
	cmd.Dir = wtPath
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Errorf("git status in worktree failed: %v: %s", err, out)
	}

	// Verify branch
	cmd = exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = wtPath
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git rev-parse failed: %v: %s", err, out)
	}
	branch := string(out[:len(out)-1]) // trim newline
	if branch != "feat-a" {
		t.Errorf("expected branch feat-a, got %s", branch)
	}

	// Remove
	if err := mgr.Remove(ctx, repoDir, wtPath); err != nil {
		t.Fatalf("removing worktree: %v", err)
	}

	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Error("expected worktree directory to be removed")
	}
}

func TestCreateDuplicatePath(t *testing.T) {
	repoDir := setupTestRepo(t)
	baseDir := t.TempDir()
	mgr := NewManager(baseDir)
	ctx := context.Background()

	_, err := mgr.Create(ctx, repoDir, "proj", "feat", "feat")
	if err != nil {
		t.Fatalf("first create: %v", err)
	}

	_, err = mgr.Create(ctx, repoDir, "proj", "feat", "feat")
	if err == nil {
		t.Fatal("expected error on duplicate worktree path")
	}
}

func TestProjectNameFromURL(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"git@github.com:user/myrepo.git", "myrepo"},
		{"https://github.com/user/myrepo.git", "myrepo"},
		{"git@github.com:user/myrepo", "myrepo"},
		{"https://github.com/user/myrepo", "myrepo"},
	}

	for _, tt := range tests {
		got := projectNameFromURL(tt.url)
		if got != tt.want {
			t.Errorf("projectNameFromURL(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}
}
