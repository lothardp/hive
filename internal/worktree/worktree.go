package worktree

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/lothardp/hive/internal/shell"
)

type Manager struct {
	BaseDir string
}

func NewManager(baseDir string) *Manager {
	return &Manager{BaseDir: baseDir}
}

func DefaultBaseDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("getting home directory: %w", err)
	}
	return filepath.Join(home, "workspaces"), nil
}

// Create creates a git worktree at BaseDir/<project>/<name>.
// If the branch doesn't exist, it creates one from the current HEAD.
func (m *Manager) Create(ctx context.Context, repoDir, project, name, branch string) (string, error) {
	wtPath := filepath.Join(m.BaseDir, project, name)

	if _, err := os.Stat(wtPath); err == nil {
		return "", fmt.Errorf("worktree path already exists: %s", wtPath)
	}

	if err := os.MkdirAll(filepath.Dir(wtPath), 0o755); err != nil {
		return "", fmt.Errorf("creating parent directory: %w", err)
	}

	// Check if branch exists
	res, err := shell.RunInDir(ctx, repoDir, "git", "rev-parse", "--verify", branch)
	if err != nil {
		return "", fmt.Errorf("checking branch: %w", err)
	}
	if res.ExitCode != 0 {
		slog.Debug("branch does not exist, creating", "branch", branch)
		res, err = shell.RunInDir(ctx, repoDir, "git", "branch", branch)
		if err != nil {
			return "", fmt.Errorf("creating branch: %w", err)
		}
		if res.ExitCode != 0 {
			return "", fmt.Errorf("creating branch %q: %s", branch, res.Stderr)
		}
	}

	// Create worktree
	res, err = shell.RunInDir(ctx, repoDir, "git", "worktree", "add", wtPath, branch)
	if err != nil {
		return "", fmt.Errorf("creating worktree: %w", err)
	}
	if res.ExitCode != 0 {
		return "", fmt.Errorf("creating worktree: %s", res.Stderr)
	}

	slog.Debug("created worktree", "path", wtPath, "branch", branch)
	return wtPath, nil
}

// Remove removes a git worktree and prunes stale entries.
func (m *Manager) Remove(ctx context.Context, repoDir, wtPath string) error {
	res, err := shell.RunInDir(ctx, repoDir, "git", "worktree", "remove", "--force", wtPath)
	if err != nil {
		return fmt.Errorf("removing worktree: %w", err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("removing worktree: %s", res.Stderr)
	}

	// Prune stale worktree entries
	if _, err := shell.RunInDir(ctx, repoDir, "git", "worktree", "prune"); err != nil {
		slog.Warn("failed to prune worktrees", "error", err)
	}

	slog.Debug("removed worktree", "path", wtPath)
	return nil
}

// DeleteBranch deletes a local git branch. It refuses to delete the default
// branch (main/master) or the currently checked-out branch.
func (m *Manager) DeleteBranch(ctx context.Context, repoDir, branch string) error {
	if branch == "main" || branch == "master" {
		return fmt.Errorf("refusing to delete default branch %q", branch)
	}

	// Check we're not on that branch
	res, err := shell.RunInDir(ctx, repoDir, "git", "symbolic-ref", "--short", "HEAD")
	if err == nil && res.ExitCode == 0 {
		current := strings.TrimSpace(res.Stdout)
		if current == branch {
			return fmt.Errorf("branch %q is currently checked out", branch)
		}
	}

	res, err = shell.RunInDir(ctx, repoDir, "git", "branch", "-d", branch)
	if err != nil {
		return fmt.Errorf("deleting branch: %w", err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("deleting branch %q: %s", branch, strings.TrimSpace(res.Stderr))
	}

	slog.Debug("deleted branch", "branch", branch)
	return nil
}

// DetectProject returns the project name from the git remote origin URL,
// falling back to the directory name.
func DetectProject(ctx context.Context, repoDir string) (string, error) {
	res, err := shell.RunInDir(ctx, repoDir, "git", "config", "--get", "remote.origin.url")
	if err == nil && res.ExitCode == 0 {
		url := strings.TrimSpace(res.Stdout)
		if name := projectNameFromURL(url); name != "" {
			return name, nil
		}
	}

	// Fallback to directory name
	return filepath.Base(repoDir), nil
}

// DetectRepoRoot returns the root of the current git repository.
func DetectRepoRoot(ctx context.Context) (string, error) {
	res, err := shell.Run(ctx, "git", "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("detecting git repo: %w", err)
	}
	if res.ExitCode != 0 {
		return "", fmt.Errorf("not in a git repository")
	}
	return strings.TrimSpace(res.Stdout), nil
}

func projectNameFromURL(url string) string {
	// Handle SSH: git@github.com:user/repo.git
	if i := strings.LastIndex(url, ":"); i != -1 && !strings.Contains(url, "://") {
		url = url[i+1:]
	}
	// Handle HTTPS: https://github.com/user/repo.git
	if i := strings.LastIndex(url, "/"); i != -1 {
		url = url[i+1:]
	}
	// Strip .git suffix
	url = strings.TrimSuffix(url, ".git")
	url = strings.TrimSpace(url)
	return url
}
