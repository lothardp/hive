package shell

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
)

type RunResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

func Run(ctx context.Context, name string, args ...string) (*RunResult, error) {
	return RunInDir(ctx, "", name, args...)
}

func RunInDir(ctx context.Context, dir string, name string, args ...string) (*RunResult, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	result := &RunResult{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}

	if exitErr, ok := err.(*exec.ExitError); ok {
		result.ExitCode = exitErr.ExitCode()
		return result, nil
	}
	if err != nil {
		return nil, fmt.Errorf("executing %s: %w", name, err)
	}
	return result, nil
}
