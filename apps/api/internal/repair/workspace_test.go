package repair_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/skcache/waypoint/apps/api/internal/repair"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Skipf("not inside a git repository: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// TestWorkspaceAppliesOnlyTheCandidateAndLeavesTheDeveloperTreeAlone proves the
// core isolation claim: the patch lands in a disposable worktree, the developer
// tree's status is unchanged, and a non-allowlisted path cannot escape.
func TestWorkspaceAppliesOnlyTheCandidateAndLeavesTheDeveloperTreeAlone(t *testing.T) {
	root := repoRoot(t)
	before, err := repair.RepoStatus(root)
	if err != nil {
		t.Fatalf("status before: %v", err)
	}

	ws, err := repair.NewWorkspace(context.Background(), root, "HEAD")
	if err != nil {
		t.Fatalf("new workspace: %v", err)
	}
	defer ws.Close()

	allow := repair.DefaultAllowlist()
	changed, err := ws.ApplyEdits([]repair.Edit{{
		Path:    "apps/api/internal/merchant/zz_probe.go",
		NewText: "package merchant\n\n// probe\n",
	}}, allow)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(changed) != 1 {
		t.Fatalf("changed = %v", changed)
	}
	diff, err := ws.Diff()
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if !strings.Contains(diff, "zz_probe.go") {
		t.Fatalf("diff does not contain the candidate file:\n%s", diff)
	}

	// Escape attempts are refused before anything is written.
	for _, bad := range []repair.Edit{
		{Path: "apps/api/internal/merchant/../../../tmp/escape.go", NewText: "package x"},
		{Path: "apps/api/internal/merchant/store.go", OldText: "package merchant", NewText: "package merchant"},
		{Path: "apps/api/internal/merchant/service.go", OldText: "package", NewText: "package"},
	} {
		if _, err := ws.ApplyEdits([]repair.Edit{bad}, allow); err == nil {
			t.Fatalf("escape edit %q was accepted", bad.Path)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "apps/api/internal/merchant/zz_probe.go")); !os.IsNotExist(err) {
		t.Fatalf("candidate file leaked into the developer tree: %v", err)
	}

	after, err := repair.RepoStatus(root)
	if err != nil {
		t.Fatalf("status after: %v", err)
	}
	if after != before {
		t.Fatalf("developer tree changed:\nbefore=%q\nafter=%q", before, after)
	}

	dir := ws.Dir
	ws.Close()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("workspace directory survived Close: %v", err)
	}
}

func TestWorkspaceRejectsDeletionAndRename(t *testing.T) {
	root := repoRoot(t)
	ws, err := repair.NewWorkspace(context.Background(), root, "HEAD")
	if err != nil {
		t.Fatalf("new workspace: %v", err)
	}
	defer ws.Close()
	if err := os.Remove(filepath.Join(ws.Dir, "apps/api/internal/merchant/incident.go")); err != nil {
		t.Skipf("cannot stage a deletion: %v", err)
	}
	if _, err := ws.ApplyEdits([]repair.Edit{{
		Path:    "apps/api/internal/merchant/service.go",
		OldText: "package merchant",
		NewText: "package merchant",
	}}, repair.DefaultAllowlist()); err == nil {
		t.Fatal("a workspace containing an unexpected deletion was accepted")
	}
}
