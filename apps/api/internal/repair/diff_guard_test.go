package repair

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", "a.txt")
	run("commit", "-q", "-m", "base")
	return dir
}

func TestCaptureReviewableDiffFailsClosedWithoutGit(t *testing.T) {
	ws := &Workspace{Dir: t.TempDir()}
	if _, _, err := captureReviewableDiff(ws, t.TempDir()); err == nil {
		t.Fatal("expected a diff error outside a git worktree")
	}
}

func TestCaptureReviewableDiffWritesExactDigest(t *testing.T) {
	repo := gitTestRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("two\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	diff, digest, err := captureReviewableDiff(&Workspace{Dir: repo}, out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "+two") {
		t.Fatalf("diff does not contain the change:\n%s", diff)
	}
	if !strings.HasPrefix(digest, "sha256:") {
		t.Fatalf("digest = %q", digest)
	}
	if data, err := os.ReadFile(filepath.Join(out, "patch.diff")); err != nil || string(data) != diff {
		t.Fatalf("patch.diff mismatch: %v", err)
	}

	// A no-op candidate has no reviewable diff and must fail closed.
	clean := gitTestRepo(t)
	if _, _, err := captureReviewableDiff(&Workspace{Dir: clean}, out); err == nil {
		t.Fatal("expected an empty diff to be refused")
	}
}

func TestVerifyRepoUnchangedRunsForCleanTree(t *testing.T) {
	repo := gitTestRepo(t)
	before, err := RepoStatus(repo)
	if err != nil {
		t.Fatal(err)
	}
	if before != "" {
		t.Fatalf("expected a clean tree, status = %q", before)
	}
	if err := os.WriteFile(filepath.Join(repo, "untracked.txt"), []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyRepoUnchanged(repo, "", true); err == nil {
		t.Fatal("a clean before-state must still be compared when the check is enabled")
	}
	if err := verifyRepoUnchanged(repo, before, false); err != nil {
		t.Fatalf("disabled check returned an error: %v", err)
	}
}
