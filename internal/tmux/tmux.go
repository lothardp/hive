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
// Environment variables are set via -e flags so they apply to the initial window too.
func (m *Manager) CreateSession(ctx context.Context, name, workDir string, env map[string]string) error {
	args := []string{"new-session", "-d", "-s", name, "-c", workDir}
	for k, v := range env {
		args = append(args, "-e", k+"="+v)
	}
	res, err := shell.Run(ctx, "tmux", args...)
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
	// Use -t=name (not -t name) for exact match — without = tmux matches prefixes.
	res, err := shell.Run(ctx, "tmux", "has-session", "-t="+name)
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

	// Use -t=name for exact match (see SessionExists).
	res, err := shell.Run(ctx, "tmux", "kill-session", "-t="+name)
	if err != nil {
		return fmt.Errorf("killing tmux session: %w", err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("killing tmux session: %s", strings.TrimSpace(res.Stderr))
	}
	slog.Debug("killed tmux session", "name", name)
	return nil
}

// ListSessions returns the names of all running tmux sessions.
func (m *Manager) ListSessions(ctx context.Context) ([]string, error) {
	res, err := shell.Run(ctx, "tmux", "list-sessions", "-F", "#{session_name}")
	if err != nil {
		return nil, fmt.Errorf("listing tmux sessions: %w", err)
	}
	if res.ExitCode != 0 {
		// No server running = no sessions
		return nil, nil
	}
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(res.Stdout), "\n") {
		if line != "" {
			names = append(names, line)
		}
	}
	return names, nil
}

func findTmux() (string, error) {
	res, err := shell.Run(context.Background(), "which", "tmux")
	if err != nil || res.ExitCode != 0 {
		return "", fmt.Errorf("tmux not found in PATH")
	}
	return strings.TrimSpace(res.Stdout), nil
}
