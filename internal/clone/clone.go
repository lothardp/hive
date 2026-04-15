package clone

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lothardp/hive/internal/shell"
)

type Manager struct {
	CellsDir string // e.g. ~/hive/cells
}

func NewManager(cellsDir string) *Manager {
	return &Manager{CellsDir: cellsDir}
}

// Clone runs `git clone <repoPath> <cellsDir>/<project>/<name>`.
// Returns the absolute path to the clone.
func (m *Manager) Clone(ctx context.Context, repoPath, project, name string) (string, error) {
	targetPath := filepath.Join(m.CellsDir, project, name)

	if _, err := os.Stat(targetPath); err == nil {
		return "", fmt.Errorf("clone path already exists: %s", targetPath)
	}

	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return "", fmt.Errorf("creating parent directory: %w", err)
	}

	res, err := shell.Run(ctx, "git", "clone", repoPath, targetPath)
	if err != nil {
		return "", fmt.Errorf("cloning repo: %w", err)
	}
	if res.ExitCode != 0 {
		return "", fmt.Errorf("cloning repo: %s", strings.TrimSpace(res.Stderr))
	}

	// Copy remotes from the source repo so the clone points at the real
	// upstream (e.g. GitHub) instead of the local path.
	if err := copyRemotes(ctx, repoPath, targetPath); err != nil {
		// Non-fatal — the clone is still usable, just with local remotes.
		fmt.Fprintf(os.Stderr, "warning: failed to copy remotes: %v\n", err)
	}

	return targetPath, nil
}

// copyRemotes reads all remotes from srcRepo and sets them on dstRepo.
func copyRemotes(ctx context.Context, srcRepo, dstRepo string) error {
	// Get remote names from source
	res, err := shell.RunInDir(ctx, srcRepo, "git", "remote")
	if err != nil {
		return fmt.Errorf("listing remotes: %w", err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("listing remotes: %s", strings.TrimSpace(res.Stderr))
	}

	remotes := strings.Fields(strings.TrimSpace(res.Stdout))
	for _, name := range remotes {
		// Get the URL for this remote from the source repo
		res, err := shell.RunInDir(ctx, srcRepo, "git", "remote", "get-url", name)
		if err != nil || res.ExitCode != 0 {
			continue
		}
		url := strings.TrimSpace(res.Stdout)

		// Set it on the clone (origin already exists from the clone, others need adding)
		res, err = shell.RunInDir(ctx, dstRepo, "git", "remote", "set-url", name, url)
		if err != nil || res.ExitCode != 0 {
			// Remote doesn't exist in clone yet — add it
			shell.RunInDir(ctx, dstRepo, "git", "remote", "add", name, url)
		}
	}

	return nil
}

// Remove deletes the clone directory.
// Safety: refuses to remove paths not under CellsDir.
func (m *Manager) Remove(clonePath string) error {
	absClone, err := filepath.Abs(clonePath)
	if err != nil {
		return fmt.Errorf("resolving clone path: %w", err)
	}
	absCells, err := filepath.Abs(m.CellsDir)
	if err != nil {
		return fmt.Errorf("resolving cells dir: %w", err)
	}

	if !strings.HasPrefix(absClone, absCells+string(filepath.Separator)) {
		return fmt.Errorf("refusing to remove %q: not under cells directory %q", absClone, absCells)
	}

	if err := os.RemoveAll(absClone); err != nil {
		return fmt.Errorf("removing clone: %w", err)
	}
	return nil
}

// Exists checks if the clone directory exists.
func (m *Manager) Exists(clonePath string) bool {
	_, err := os.Stat(clonePath)
	return err == nil
}
