package tmux

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"syscall"

	"github.com/lothardp/hive/internal/shell"
)

type Manager struct{}

func NewManager() *Manager {
	return &Manager{}
}

// CreateSession creates a new detached tmux session with the given name and working directory.
func (m *Manager) CreateSession(ctx context.Context, name, workDir string) error {
	res, err := shell.Run(ctx, "tmux", "new-session", "-d", "-s", name, "-c", workDir)
	if err != nil {
		return fmt.Errorf("creating tmux session: %w", err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("creating tmux session: %s", strings.TrimSpace(res.Stderr))
	}
	slog.Debug("created tmux session", "name", name, "workDir", workDir)
	return nil
}

// SessionExists checks if a tmux session with the given name exists.
func (m *Manager) SessionExists(ctx context.Context, name string) (bool, error) {
	res, err := shell.Run(ctx, "tmux", "has-session", "-t", name)
	if err != nil {
		return false, fmt.Errorf("checking tmux session: %w", err)
	}
	return res.ExitCode == 0, nil
}

// JoinSession switches to the target session if already inside tmux,
// otherwise attaches to it.
func (m *Manager) JoinSession(name string) error {
	tmuxPath, err := findTmux()
	if err != nil {
		return err
	}
	if os.Getenv("TMUX") != "" {
		return syscall.Exec(tmuxPath, []string{"tmux", "switch-client", "-t", name}, syscall.Environ())
	}
	return syscall.Exec(tmuxPath, []string{"tmux", "attach-session", "-t", name}, syscall.Environ())
}

// KillSession kills the tmux session with the given name.
func (m *Manager) KillSession(ctx context.Context, name string) error {
	exists, err := m.SessionExists(ctx, name)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}

	res, err := shell.Run(ctx, "tmux", "kill-session", "-t", name)
	if err != nil {
		return fmt.Errorf("killing tmux session: %w", err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("killing tmux session: %s", strings.TrimSpace(res.Stderr))
	}
	slog.Debug("killed tmux session", "name", name)
	return nil
}

func findTmux() (string, error) {
	res, err := shell.Run(context.Background(), "which", "tmux")
	if err != nil || res.ExitCode != 0 {
		return "", fmt.Errorf("tmux not found in PATH")
	}
	return strings.TrimSpace(res.Stdout), nil
}
