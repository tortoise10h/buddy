package worktree

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// DeriveWorktreePath returns sibling directory path: <parent>/<repo>-wt-<taskSlug>.
func DeriveWorktreePath(projectPath, taskSlug string) string {
	parentDir := filepath.Dir(projectPath)
	repoName := filepath.Base(projectPath)
	return filepath.Join(parentDir, fmt.Sprintf("%s-wt-%s", repoName, taskSlug))
}

// DeriveBranch returns explicit branch or default feat/<taskSlug>.
func DeriveBranch(taskSlug, explicitBranch string) string {
	if explicitBranch != "" {
		return explicitBranch
	}
	return fmt.Sprintf("feat/%s", taskSlug)
}

// EnsureWorktree creates a git worktree if it does not already exist.
func EnsureWorktree(projectPath, taskSlug, explicitBranch string) (string, string, error) {
	wtPath := DeriveWorktreePath(projectPath, taskSlug)
	branch := DeriveBranch(taskSlug, explicitBranch)

	if _, err := os.Stat(wtPath); os.IsNotExist(err) {
		cmdAdd := exec.Command("git", "-C", projectPath, "worktree", "add", "-b", branch, wtPath)
		if err := cmdAdd.Run(); err != nil {
			// Branch might already exist; try checkout without -b
			cmdFallback := exec.Command("git", "-C", projectPath, "worktree", "add", wtPath, branch)
			if errFallback := cmdFallback.Run(); errFallback != nil {
				return wtPath, branch, fmt.Errorf("git worktree add failed: %w", errFallback)
			}
		}
	}

	return wtPath, branch, nil
}

// RemoveWorktree removes a git worktree.
func RemoveWorktree(projectPath, wtPath string, force bool) error {
	args := []string{"-C", projectPath, "worktree", "remove", wtPath}
	if force {
		args = append(args, "--force")
	}
	cmd := exec.Command("git", args...)
	return cmd.Run()
}
