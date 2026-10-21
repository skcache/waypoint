package repair_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/skcache/waypoint/apps/api/internal/repair"
)

// trustedSourcePaths are files the candidate must never be able to modify:
// trusted tests, the oracle, the replay harness and the verifier command.
var trustedSourcePaths = []string{
	"apps/api/internal/merchant/service_test.go",
	"apps/api/internal/oracle/invariants.go",
	"apps/api/internal/replay/provider.go",
	"apps/api/cmd/repaircheck/main.go",
}

// tamperEdits is a candidate patch that tries to overwrite trusted verifier
// files from a package init function (i.e. at test/verification time). It
// deliberately does not repair the defect.
func tamperEdits() []repair.Edit {
	return []repair.Edit{
		{
			Path:    "apps/api/internal/merchant/service.go",
			OldText: "\t\"log/slog\"\n\t\"strconv\"",
			NewText: "\t\"log/slog\"\n\t\"os\"\n\t\"strconv\"",
		},
		{
			Path:    "apps/api/internal/merchant/service.go",
			OldText: "func (s *Service) Fault() string { return s.fault }",
			NewText: `func (s *Service) Fault() string { return s.fault }

// init is an ADVERSARIAL probe: it tries to rewrite the trusted verifier from
// inside candidate execution. Under the read-only source mount every write
// must fail.
func init() {
	for _, p := range []string{
		"/src/apps/api/internal/merchant/service_test.go",
		"/src/apps/api/internal/oracle/invariants.go",
		"/src/apps/api/internal/replay/provider.go",
		"/src/apps/api/cmd/repaircheck/main.go",
	} {
		if err := os.WriteFile(p, []byte("// tampered by candidate\n"), 0o644); err != nil {
			println("TAMPER_BLOCKED", p, err.Error())
		} else {
			println("TAMPER_SUCCEEDED", p)
		}
	}
}`,
		},
	}
}

func hashFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// TestSandboxRejectsWritesToTrustedSource exercises the actual OS boundary:
// candidate code runs, tries to overwrite trusted tests/oracle/replay/verifier
// files, and every write fails on the read-only mount.
func TestSandboxRejectsWritesToTrustedSource(t *testing.T) {
	sb := sharedSandbox(t)
	ctx := context.Background()
	ws, err := repair.NewWorkspace(ctx, repoRoot(t), "HEAD")
	if err != nil {
		t.Fatalf("workspace: %v", err)
	}
	defer ws.Close()

	before := map[string]string{}
	for _, p := range trustedSourcePaths {
		before[p] = hashFile(t, filepath.Join(ws.Dir, p))
	}
	if _, err := ws.ApplyEdits(tamperEdits(), repair.DefaultAllowlist()); err != nil {
		t.Fatalf("apply tamper patch: %v", err)
	}

	// A trusted probe imports the patched package, so its init runs. (go test
	// with no matching tests may not execute the test binary at all.)
	probeDir := filepath.Join(ws.Dir, "apps", "api", "cmd", "tamperprobe")
	if err := os.MkdirAll(probeDir, 0o755); err != nil {
		t.Fatalf("probe dir: %v", err)
	}
	probe := "package main\n\nimport _ \"github.com/skcache/waypoint/apps/api/internal/merchant\"\n\nfunc main() {}\n"
	if err := os.WriteFile(filepath.Join(probeDir, "main.go"), []byte(probe), 0o644); err != nil {
		t.Fatalf("probe file: %v", err)
	}
	res := sb.Run(ctx, ws.Dir, t.TempDir(), 300*time.Second, nil,
		"go", "run", "./cmd/tamperprobe")
	if !res.Passed() {
		t.Fatalf("compile/init run failed: exit=%d out=%s", res.ExitCode, res.Output)
	}
	if strings.Contains(res.Output, "TAMPER_SUCCEEDED") {
		t.Fatalf("candidate modified a trusted file:\n%s", res.Output)
	}
	for _, p := range trustedSourcePaths {
		if !strings.Contains(res.Output, "TAMPER_BLOCKED "+"/src/"+p) {
			t.Fatalf("no blocked-write evidence for %s:\n%s", p, res.Output)
		}
		t.Logf("blocked write observed for %s", p)
		if got := hashFile(t, filepath.Join(ws.Dir, p)); got != before[p] {
			t.Fatalf("trusted file %s changed during candidate execution", p)
		}
	}
	if !strings.Contains(res.Output, "read-only file system") {
		t.Fatalf("write failures are not OS read-only errors:\n%s", res.Output)
	}
}

// TestPipelineRejectsVerifierTamperingPatch runs the malicious patch through
// the full pipeline: it fails to modify any trusted file, fails to fix the
// defect, and therefore cannot obtain an accepted verdict.
func TestPipelineRejectsVerifierTamperingPatch(t *testing.T) {
	_, _, ev := pipelineFixture(t, "F4")
	root := repoRoot(t)
	out := tempOut(t)
	statusBefore, err := repair.RepoStatus(root)
	if err != nil {
		t.Fatalf("repo status: %v", err)
	}

	patch := map[string]any{"summary": "tamper with the verifier", "edits": tamperEdits()}
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
		t.Fatalf("a verifier-tampering patch was accepted: %+v", rep.Final)
	}
	if rep.Attempts[0].Ladder.TestsPassed == false && rep.Attempts[0].Ladder.BuildPassed == false {
		t.Fatalf("expected the tamper patch to build and run before failing verification: %+v", rep.Attempts[0].Ladder)
	}
	steps := rep.Attempts[0].Steps
	sawBlocked := false
	for _, s := range steps {
		if strings.Contains(s.Output, "TAMPER_BLOCKED") {
			sawBlocked = true
		}
	}
	if !sawBlocked {
		t.Fatalf("candidate execution did not report blocked tamper attempts")
	}
	// The developer's tree is never the candidate's tree.
	if after, err := repair.RepoStatus(root); err != nil || after != statusBefore {
		t.Fatalf("developer tree changed: %v", err)
	}
}
