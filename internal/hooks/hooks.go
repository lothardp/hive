package hooks

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lothardp/hive/internal/shell"
)

type HookError struct {
	Command string
	Index   int
	Stderr  string
	Err     error
}

func (e *HookError) Error() string {
	return fmt.Sprintf("hook %d (%s) failed: %v", e.Index, e.Command, e.Err)
}

type Result struct {
	Ran    int
	Total  int
	Failed *HookError
}

type Runner struct{}

func NewRunner() *Runner {
	return &Runner{}
}

func (r *Runner) Run(ctx context.Context, workDir string, hooks []string) *Result {
	result := &Result{Total: len(hooks)}

	var lines []string
	for i, cmd := range hooks {
		result.Ran = i + 1

		res, err := shell.RunInDir(ctx, workDir, "sh", "-c", cmd)
		if err != nil {
			hookErr := &HookError{Command: cmd, Index: i, Stderr: "", Err: err}
			result.Failed = hookErr
			lines = append(lines, fmt.Sprintf("FAIL [%d] %s\n  error: %v", i, cmd, err))
			break
		}
		if res.ExitCode != 0 {
			hookErr := &HookError{
				Command: cmd,
				Index:   i,
				Stderr:  strings.TrimSpace(res.Stderr),
				Err:     fmt.Errorf("exit code %d", res.ExitCode),
			}
			result.Failed = hookErr
			lines = append(lines, fmt.Sprintf("FAIL [%d] %s\n  exit code: %d\n  stderr: %s", i, cmd, res.ExitCode, res.Stderr))
			break
		}
		lines = append(lines, fmt.Sprintf("OK   [%d] %s", i, cmd))
	}

	// Write results file
	if len(lines) > 0 {
		content := strings.Join(lines, "\n") + "\n"
		_ = os.WriteFile(filepath.Join(workDir, "hook_results.txt"), []byte(content), 0o644)
	}

	return result
}
