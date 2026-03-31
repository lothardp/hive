package layout

import (
	"context"
	"testing"

	"github.com/lothardp/hive/internal/config"
)

func TestApply_EmptyLayout(t *testing.T) {
	// Empty layout should be a no-op (no tmux calls)
	err := Apply(context.Background(), "test-session", "/tmp", config.Layout{})
	if err != nil {
		t.Errorf("empty layout should not error, got: %v", err)
	}
}

func TestApply_NilWindows(t *testing.T) {
	err := Apply(context.Background(), "test-session", "/tmp", config.Layout{Windows: nil})
	if err != nil {
		t.Errorf("nil windows should not error, got: %v", err)
	}
}
