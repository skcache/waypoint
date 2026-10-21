package repair_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/skcache/waypoint/apps/api/internal/repair"
)

// pgxDependency is a module used by cmd/judgecheck (via pgxpool). Poisoning it
// would let candidate-controlled code influence the judge's own dependency.
const pgxDependencyGlob = "/gomodcache/github.com/jackc/pgx/v5@*/pgxpool/pool.go"

// TestSandboxCandidateCannotWriteJudgeDependency proves the preloaded module
// cache is immutable to candidate execution: a write to a dependency used by
// the trusted judge must fail, and the dependency bytes must not change.
func TestSandboxCandidateCannotWriteJudgeDependency(t *testing.T) {
	sb := sharedSandbox(t)
	worktree, artifacts := probeWorktree(t)
	ctx := context.Background()

	before := sb.Run(ctx, worktree, artifacts, 30*time.Second, nil, "sh", "-c",
		"sha256sum "+pgxDependencyGlob+" | awk '{print $1}'")
	if !before.Passed() {
		t.Fatalf("cannot hash the dependency: exit=%d out=%s", before.ExitCode, before.Output)
	}
	// Module cache files are 0444, but a rw mount lets the owning uid chmod
	// them (or create new files inside the package directory). Both are real
	// tampering paths; probe each.
	res := sb.Run(ctx, worktree, artifacts, 30*time.Second, nil, "sh", "-c",
		"f=$(ls "+pgxDependencyGlob+" 2>/dev/null | head -1); "+
			"if [ -n \"$f\" ] && chmod u+w \"$f\" 2>/dev/null && echo '// poisoned' >> \"$f\" 2>/dev/null; then echo FILE_WRITABLE; else echo FILE_READONLY; fi; "+
			"d=$(dirname \"$f\" 2>/dev/null); "+
			"if [ -n \"$d\" ] && echo poison > \"$d/POISON.go\" 2>/dev/null; then echo DIR_WRITABLE; else echo DIR_READONLY; fi")
	if !res.Passed() {
		t.Fatalf("dependency probe failed: exit=%d out=%s", res.ExitCode, res.Output)
	}
	if strings.Contains(res.Output, "FILE_WRITABLE") || strings.Contains(res.Output, "DIR_WRITABLE") {
		t.Fatalf("candidate can tamper with a judge dependency in the module cache:\n%s", res.Output)
	}
	if !strings.Contains(res.Output, "FILE_READONLY") || !strings.Contains(res.Output, "DIR_READONLY") {
		t.Fatalf("module cache tamper probes did not both report read-only:\n%s", res.Output)
	}
	after := sb.Run(ctx, worktree, artifacts, 30*time.Second, nil, "sh", "-c",
		"sha256sum "+pgxDependencyGlob+" | awk '{print $1}'")
	if before.Output != after.Output {
		t.Fatalf("module cache dependency changed:\nbefore=%s\nafter=%s", before.Output, after.Output)
	}
}

// TestSandboxCachesAreIsolatedFromTrustedJudge proves the trusted judge does
// not share writable build state with candidate execution: a marker the
// candidate writes into its build cache must not be visible to a trusted step,
// and a marker in the module cache must be impossible.
func TestSandboxCachesAreIsolatedFromTrustedJudge(t *testing.T) {
	sb := sharedSandbox(t)
	worktree, artifacts := probeWorktree(t)
	ctx := context.Background()

	cand := sb.Run(ctx, worktree, artifacts, 30*time.Second, nil, "sh", "-c",
		"echo poison > /gocache/CACHE_POISON && echo CACHE_WRITTEN; "+
			"if echo x > /gomodcache/MODULE_POISON 2>/dev/null; then echo MODULE_WRITTEN; else echo MODULE_READONLY; fi")
	if !cand.Passed() {
		t.Fatalf("candidate cache probe failed: exit=%d out=%s", cand.ExitCode, cand.Output)
	}
	if !strings.Contains(cand.Output, "CACHE_WRITTEN") {
		t.Fatalf("candidate build cache is not writable for its own build:\n%s", cand.Output)
	}
	if strings.Contains(cand.Output, "MODULE_WRITTEN") {
		t.Fatalf("candidate wrote into the module cache:\n%s", cand.Output)
	}

	trusted := sb.RunTrusted(ctx, worktree, artifacts, 30*time.Second, nil, "sh", "-c",
		"test -e /gocache/CACHE_POISON && echo BUILD_CACHE_LEAK || echo BUILD_CACHE_CLEAN; "+
			"test -e /gomodcache/MODULE_POISON && echo MODULE_CACHE_LEAK || echo MODULE_CACHE_CLEAN")
	if !trusted.Passed() {
		t.Fatalf("trusted cache probe failed: exit=%d out=%s", trusted.ExitCode, trusted.Output)
	}
	if strings.Contains(trusted.Output, "LEAK") {
		t.Fatalf("trusted judge shares writable cache state with the candidate:\n%s", trusted.Output)
	}
	if !strings.Contains(trusted.Output, "BUILD_CACHE_CLEAN") || !strings.Contains(trusted.Output, "MODULE_CACHE_CLEAN") {
		t.Fatalf("trusted cache isolation not observed:\n%s", trusted.Output)
	}
}

// TestPipelineRejectsCachePoisoningPatch runs a patch that tries to poison the
// judge's dependency and build cache from init, without repairing the defect.
// The writes must fail and the repair must not be accepted.
func TestPipelineRejectsCachePoisoningPatch(t *testing.T) {
	_, _, ev := pipelineFixture(t, "F4")
	root := repoRoot(t)
	out := tempOut(t)

	edits := []repair.Edit{
		{
			Path:    "apps/api/internal/merchant/service.go",
			OldText: "\t\"log/slog\"\n\t\"strconv\"",
			NewText: "\t\"log/slog\"\n\t\"os\"\n\t\"path/filepath\"\n\t\"strconv\"",
		},
		{
			Path:    "apps/api/internal/merchant/service.go",
			OldText: "func (s *Service) Fault() string { return s.fault }",
			NewText: `func (s *Service) Fault() string { return s.fault }

func init() {
	if matches, _ := filepath.Glob("` + pgxDependencyGlob + `"); len(matches) > 0 {
		_ = os.WriteFile(matches[0], []byte("// poisoned\n"), 0o644)
	}
	_ = os.WriteFile("/gocache/CACHE_POISON", []byte("x"), 0o644)
}`,
		},
	}
	patch := map[string]any{"summary": "poison the judge caches", "edits": edits}
	patchJSON, _ := json.Marshal(patch)
	model := &scriptedModel{replies: []string{diagnosisReply(), string(patchJSON)}}

	rep := repair.Run(context.Background(), repair.Options{
		RepoRoot: root, BaseRef: "HEAD", Fault: "F4", OutDir: out,
		Runner: sharedSandbox(t),
		Mode:   "oracle", MaxAttempts: 1, Model: model, Evidence: ev,
		Allow: repair.DefaultAllowlist(), Revision: "test-revision", RevisionSource: "test",
		IncidentID: ev.IncidentID,
	})
	if rep.Status == repair.StatusAccepted || rep.Final.Accepted {
		t.Fatalf("a cache-poisoning patch was accepted: %+v", rep.Final)
	}
}
