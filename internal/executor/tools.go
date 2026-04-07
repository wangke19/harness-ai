package executor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// allowedCommands is the prefix allowlist for run_command.
var allowedCommands = map[string]bool{
	"go": true, "make": true, "golangci-lint": true, "git": true,
}

// RunCommand executes a shell command in the given working directory.
// Only commands whose first word is in allowedCommands are permitted.
// Timeout is enforced via context.
func RunCommand(ctx context.Context, workdir, cmd string, timeout time.Duration) (stdout, stderr string, exit int, err error) {
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return "", "", -1, fmt.Errorf("empty command")
	}
	if !allowedCommands[parts[0]] {
		return "", "", -1, fmt.Errorf("command %q not in allowlist", parts[0])
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	c := exec.CommandContext(ctx, parts[0], parts[1:]...)
	c.Dir = workdir
	var outBuf, errBuf strings.Builder
	c.Stdout = &outBuf
	c.Stderr = &errBuf

	runErr := c.Run()
	stdout = outBuf.String()
	stderr = errBuf.String()
	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			return stdout, stderr, exitErr.ExitCode(), nil
		}
		return stdout, stderr, -1, runErr
	}
	return stdout, stderr, 0, nil
}

// CreateWorktree creates a new git worktree at /tmp/harness-<branch>.
func CreateWorktree(ctx context.Context, repoPath, branch string) (string, error) {
	worktreePath := filepath.Join(os.TempDir(), "harness-"+branch)
	args := []string{"worktree", "add", "-b", branch, worktreePath, "origin/main"}
	c := exec.CommandContext(ctx, "git", args...)
	c.Dir = repoPath
	out, err := c.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git worktree add: %w\n%s", err, out)
	}
	return worktreePath, nil
}

// DeleteWorktree removes a git worktree.
func DeleteWorktree(ctx context.Context, repoPath, worktreePath string) error {
	c := exec.CommandContext(ctx, "git", "worktree", "remove", "--force", worktreePath)
	c.Dir = repoPath
	out, err := c.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree remove: %w\n%s", err, out)
	}
	return nil
}

// WriteFile writes content to a file, creating parent directories as needed.
func WriteFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

// ReadFile reads a file and returns its contents.
func ReadFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
