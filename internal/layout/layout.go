package layout

import (
	"context"
	"fmt"
	"strings"

	"github.com/lothardp/hive/internal/config"
	"github.com/lothardp/hive/internal/shell"
)

// Apply configures the tmux session with the given layout.
// The session must already exist with one default window.
func Apply(ctx context.Context, sessionName string, workDir string, layout config.Layout) error {
	if len(layout.Windows) == 0 {
		return nil
	}

	// TODO: detect base-index from tmux instead of hardcoding 1
	baseIndex := "1"

	// Rename the existing first window
	first := layout.Windows[0]
	if err := tmuxRun(ctx, "rename-window", "-t", sessionName+":"+baseIndex, first.Name); err != nil {
		return fmt.Errorf("renaming first window: %w", err)
	}
	if err := applyPanes(ctx, sessionName, first.Name, workDir, first.Panes); err != nil {
		return err
	}

	// Create additional windows
	for i := 1; i < len(layout.Windows); i++ {
		w := layout.Windows[i]
		if err := tmuxRun(ctx, "new-window", "-t", sessionName, "-n", w.Name, "-c", workDir); err != nil {
			return fmt.Errorf("creating window %q: %w", w.Name, err)
		}
		if err := applyPanes(ctx, sessionName, w.Name, workDir, w.Panes); err != nil {
			return err
		}
	}

	// Select the first window
	if err := tmuxRun(ctx, "select-window", "-t", sessionName+":"+baseIndex); err != nil {
		return fmt.Errorf("selecting first window: %w", err)
	}

	return nil
}

func applyPanes(ctx context.Context, session, window, workDir string, panes []config.Pane) error {
	target := session + ":" + window

	// First pane already exists (created with the window). Start from index 1.
	for i := 1; i < len(panes); i++ {
		p := panes[i]
		splitFlag := "-v" // vertical split (top/bottom) is default
		if p.Split == "horizontal" {
			splitFlag = "-h" // horizontal split (side by side)
		}
		if err := tmuxRun(ctx, "split-window", splitFlag, "-t", target, "-c", workDir); err != nil {
			return fmt.Errorf("splitting pane %d in window %q: %w", i, window, err)
		}
	}

	// Send commands to each pane
	for i, p := range panes {
		if p.Command == "" {
			continue
		}
		paneTarget := fmt.Sprintf("%s.%d", target, i)
		if err := tmuxRun(ctx, "send-keys", "-t", paneTarget, p.Command, "Enter"); err != nil {
			return fmt.Errorf("sending command to pane %d in window %q: %w", i, window, err)
		}
	}

	return nil
}

func tmuxRun(ctx context.Context, args ...string) error {
	res, err := shell.Run(ctx, "tmux", args...)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("tmux %s: %s", args[0], strings.TrimSpace(res.Stderr))
	}
	return nil
}
