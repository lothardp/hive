package hooks

import (
	"context"
	"fmt"
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

func (r *Runner) Run(ctx context.Context, workDir string, hooks []string, env map[string]string) *Result {
	result := &Result{Total: len(hooks)}

	for i, cmd := range hooks {
		result.Ran = i + 1

		res, err := shell.RunInDirWithEnv(ctx, workDir, env, "sh", "-c", cmd)
		if err != nil {
			result.Failed = &HookError{Command: cmd, Index: i, Stderr: "", Err: err}
			break
		}
		if res.ExitCode != 0 {
			result.Failed = &HookError{
				Command: cmd,
				Index:   i,
				Stderr:  strings.TrimSpace(res.Stderr),
				Err:     fmt.Errorf("exit code %d", res.ExitCode),
			}
			break
		}
	}

	return result
}
