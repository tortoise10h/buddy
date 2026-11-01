package worktree

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func createTempGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	repoPath := filepath.Join(dir, "myrepo")
	if err := os.MkdirAll(repoPath, 0755); err != nil {
		t.Fatal(err)
	}

	runGit := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", repoPath}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\nOutput: %s", args, err, string(out))
		}
	}

	runGit("init")
	runGit("config", "user.email", "test@example.com")
	runGit("config", "user.name", "Test User")
	runGit("commit", "--allow-empty", "-m", "initial commit")

	return repoPath
}

func TestDeriveWorktreePath_And_Branch(t *testing.T) {
	proj := "/Users/test/Projects/myrepo"
	task := "auth-v2"

	wtPath := DeriveWorktreePath(proj, task)
	expectedPath := filepath.Join("/Users/test/Projects", "myrepo-wt-auth-v2")
	if wtPath != expectedPath {
		t.Fatalf("expected %q, got %q", expectedPath, wtPath)
	}

	defBranch := DeriveBranch(task, "")
	if defBranch != "feat/auth-v2" {
		t.Fatalf("expected feat/auth-v2, got %q", defBranch)
	}

	expBranch := DeriveBranch(task, "custom-fix")
	if expBranch != "custom-fix" {
		t.Fatalf("expected custom-fix, got %q", expBranch)
	}
}

func TestEnsureWorktree_Create(t *testing.T) {
	repoPath := createTempGitRepo(t)

	wtPath, branch, err := EnsureWorktree(repoPath, "task-1", "")
	if err != nil {
		t.Fatalf("EnsureWorktree failed: %v", err)
	}

	expectedPrefix := filepath.Join(filepath.Dir(repoPath), "myrepo-wt-task-1")
	if wtPath != expectedPrefix {
		t.Fatalf("expected worktree path %q, got %q", expectedPrefix, wtPath)
	}
	if branch != "feat/task-1" {
		t.Fatalf("expected branch feat/task-1, got %q", branch)
	}

	// Verify worktree exists on disk
	fi, err := os.Stat(wtPath)
	if err != nil || !fi.IsDir() {
		t.Fatalf("expected worktree directory to exist: %v", err)
	}

	// Verify git worktree list contains wtPath
	cmd := exec.Command("git", "-C", repoPath, "worktree", "list")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git worktree list failed: %v", err)
	}
	if !strings.Contains(string(out), wtPath) {
		t.Fatalf("git worktree list does not contain %s:\n%s", wtPath, string(out))
	}
}

func TestEnsureWorktree_ReuseExistingBranch(t *testing.T) {
	repoPath := createTempGitRepo(t)

	// Pre-create the branch feat/task-reuse
	cmd := exec.Command("git", "-C", repoPath, "branch", "feat/task-reuse")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to pre-create branch: %v\nOutput: %s", err, string(out))
	}

	// EnsureWorktree should catch -b failure and succeed via fallback without -b
	wtPath, branch, err := EnsureWorktree(repoPath, "task-reuse", "")
	if err != nil {
		t.Fatalf("EnsureWorktree reuse branch failed: %v", err)
	}

	if branch != "feat/task-reuse" {
		t.Fatalf("expected branch feat/task-reuse, got %q", branch)
	}

	fi, err := os.Stat(wtPath)
	if err != nil || !fi.IsDir() {
		t.Fatalf("expected reused worktree directory to exist: %v", err)
	}
}

func TestEnsureWorktree_AlreadyExists(t *testing.T) {
	repoPath := createTempGitRepo(t)

	wtPath1, _, err := EnsureWorktree(repoPath, "task-dup", "")
	if err != nil {
		t.Fatalf("first EnsureWorktree failed: %v", err)
	}

	// Second call should return early since directory already exists
	wtPath2, _, err := EnsureWorktree(repoPath, "task-dup", "")
	if err != nil {
		t.Fatalf("second EnsureWorktree failed: %v", err)
	}
	if wtPath1 != wtPath2 {
		t.Fatalf("paths differ: %s vs %s", wtPath1, wtPath2)
	}
}

func TestEnsureWorktree_ErrorNotGitRepo(t *testing.T) {
	notRepo := t.TempDir()

	_, _, err := EnsureWorktree(notRepo, "some-task", "")
	if err == nil {
		t.Fatalf("expected error on non-git directory, got nil")
	}
}

func TestRemoveWorktree(t *testing.T) {
	repoPath := createTempGitRepo(t)

	wtPath, _, err := EnsureWorktree(repoPath, "task-del", "")
	if err != nil {
		t.Fatal(err)
	}

	if err := RemoveWorktree(repoPath, wtPath, true); err != nil {
		t.Fatalf("RemoveWorktree failed: %v", err)
	}

	// Check git worktree list no longer contains wtPath
	cmd := exec.Command("git", "-C", repoPath, "worktree", "list")
	out, _ := cmd.CombinedOutput()
	if strings.Contains(string(out), wtPath) {
		t.Fatalf("worktree still listed after removal: %s", string(out))
	}
}
