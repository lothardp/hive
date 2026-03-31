package hooks

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunner_AllPass(t *testing.T) {
	dir := t.TempDir()
	runner := NewRunner()

	result := runner.Run(context.Background(), dir, []string{
		"echo hello",
		"echo world",
	})

	if result.Ran != 2 {
		t.Errorf("Ran = %d, want 2", result.Ran)
	}
	if result.Total != 2 {
		t.Errorf("Total = %d, want 2", result.Total)
	}
	if result.Failed != nil {
		t.Errorf("Failed should be nil, got %v", result.Failed)
	}

	// Check hook_results.txt was written
	content, err := os.ReadFile(filepath.Join(dir, "hook_results.txt"))
	if err != nil {
		t.Fatalf("reading hook_results.txt: %v", err)
	}
	if !strings.Contains(string(content), "OK   [0] echo hello") {
		t.Errorf("hook_results.txt missing OK line, got: %s", content)
	}
}

func TestRunner_FailAtSecond(t *testing.T) {
	dir := t.TempDir()
	runner := NewRunner()

	result := runner.Run(context.Background(), dir, []string{
		"echo first",
		"exit 1",
		"echo third",
	})

	if result.Ran != 2 {
		t.Errorf("Ran = %d, want 2", result.Ran)
	}
	if result.Total != 3 {
		t.Errorf("Total = %d, want 3", result.Total)
	}
	if result.Failed == nil {
		t.Fatal("Failed should not be nil")
	}
	if result.Failed.Index != 1 {
		t.Errorf("Failed.Index = %d, want 1", result.Failed.Index)
	}
}

func TestRunner_EmptyHooks(t *testing.T) {
	dir := t.TempDir()
	runner := NewRunner()

	result := runner.Run(context.Background(), dir, []string{})

	if result.Ran != 0 {
		t.Errorf("Ran = %d, want 0", result.Ran)
	}
	if result.Failed != nil {
		t.Errorf("Failed should be nil")
	}
}

func TestRunner_WorkDirIsUsed(t *testing.T) {
	dir := t.TempDir()
	runner := NewRunner()

	// Create a file via hook to prove workdir is set
	result := runner.Run(context.Background(), dir, []string{
		"touch marker.txt",
	})

	if result.Failed != nil {
		t.Fatalf("unexpected failure: %v", result.Failed)
	}
	if _, err := os.Stat(filepath.Join(dir, "marker.txt")); os.IsNotExist(err) {
		t.Error("marker.txt should have been created in workdir")
	}
}
