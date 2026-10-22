package repair

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Verifier executes the trusted checks against the CANDIDATE worktree: the
// code compiled and run here is the patched merchant package, never the
// reference implementation sitting in the developer's tree. Every command runs
// inside the OS-enforced sandbox; there is no host fallback.
type Verifier struct {
	Workspace   *Workspace
	Runner      Runner
	Fault       string
	ArtifactDir string
	Changed     []string
	// BaseRevision is the exact commit the candidate was applied to (resolved
	// by the trusted orchestrator, never by the model). PatchDigest is sha256
	// of the applied diff. Both are recorded in the proof bundle.
	BaseRevision string
	PatchDigest  string
}

// Verification is the evidence from one verification run.
type Verification struct {
	Steps         []StepResult
	Eval          *EvalSummary
	OracleVerdict string
	Failing       []string
}

type evalFile struct {
	Accepted          bool   `json:"accepted"`
	Reason            string `json:"reason"`
	OrderStatus       string `json:"order_status"`
	ClaimedCaptureID  string `json:"claimed_capture_id"`
	ProviderCaptureID string `json:"provider_capture_id"`
	Oracle            struct {
		Verdict    string `json:"verdict"`
		Invariants []struct {
			ID      string `json:"id"`
			Status  string `json:"status"`
			Summary string `json:"summary"`
		} `json:"invariants"`
	} `json:"oracle"`
}

// Run executes gofmt → build → trusted tests → fixture verification.
func (v *Verifier) Run(ctx context.Context) Verification {
	var out Verification
	if v.Runner == nil {
		out.Steps = append(out.Steps, StepResult{Step: "sandbox", ExitCode: -1, Output: "no sandbox runner: refusing to execute candidate code on the host"})
		return out
	}
	noEnv := map[string]string{}
	testEnv := map[string]string{
		"TEST_DATABASE_URL":      v.Runner.DSN(),
		"REQUIRE_DATABASE_TESTS": "1",
	}

	// 1. The patch must be gofmt-clean. Commands run in /work/apps/api, so
	// quote paths relative to the module root.
	fmtArgs := []string{"-l"}
	for _, c := range v.Changed {
		fmtArgs = append(fmtArgs, strings.TrimPrefix(c, "apps/api/"))
	}
	gofmt := v.Runner.Run(ctx, v.Workspace.Dir, v.ArtifactDir, 60*time.Second, noEnv, "gofmt", fmtArgs...)
	gofmt.Step = "gofmt"
	out.Steps = append(out.Steps, gofmt)
	if !gofmt.Passed() || strings.TrimSpace(gofmt.Output) != "" {
		return out
	}

	// 2. Build the whole module.
	build := v.Runner.Run(ctx, v.Workspace.Dir, v.ArtifactDir, 240*time.Second, noEnv, "go", "build", "./...")
	build.Step = "build"
	out.Steps = append(out.Steps, build)
	if !build.Passed() {
		return out
	}

	// 3. Trusted regressions (tests live outside the writable surface, so a
	// candidate cannot edit them into passing).
	testArgs := []string{"test", "-count=1", "-timeout", "240s"}
	switch v.Fault {
	case "F1":
		// The fixture intentionally contains the F1 defect, so its correctness
		// gate lives behind the f1_repair tag; pre-repair reproduction is a
		// separate (fault_reproduction) test run against the pinned base.
		testArgs = append(testArgs, "-tags", "f1_repair")
	case "F2", "F3":
		// M4 correctness gates live behind m4_repair for the same reason: the
		// base revision deliberately wires the defective policy/handler.
		testArgs = append(testArgs, "-tags", "m4_repair")
	}
	testArgs = append(testArgs, "./internal/merchant/", "./internal/domain/")
	tests := v.Runner.Run(ctx, v.Workspace.Dir, v.ArtifactDir, 300*time.Second, testEnv, "go", testArgs...)
	tests.Step = "tests"
	out.Steps = append(out.Steps, tests)
	if !tests.Passed() {
		return out
	}

	// 4. Independent financial verification on the candidate's own compiled
	// fixture: build and run repaircheck from THIS workspace.
	if err := os.MkdirAll(v.ArtifactDir, 0o700); err != nil {
		out.Steps = append(out.Steps, StepResult{Step: "eval", ExitCode: -1, Output: err.Error()})
		return out
	}
	// 4a. Candidate-linked fixture run. It records ONLY provider observations.
	// Any eval.json it may have forged is deleted before the judge runs.
	_ = os.Remove(filepath.Join(v.ArtifactDir, "eval.json"))
	fixture := v.Runner.Run(ctx, v.Workspace.Dir, v.ArtifactDir, 300*time.Second, testEnv,
		"go", "run", "./cmd/fixturecheck", "-fault", v.Fault, "-out", "/out", "-dsn", v.Runner.DSN())
	fixture.Step = "fixture"
	out.Steps = append(out.Steps, fixture)
	if !fixture.Passed() {
		return out
	}

	// 4b. Trusted judge. It imports the oracle only — no merchant/replay code —
	// so candidate code cannot run in this process, forge its output, or exit
	// it early. It re-reads the journal itself and writes eval.json on every
	// path.
	judge := v.Runner.RunTrusted(ctx, v.Workspace.Dir, v.ArtifactDir, 300*time.Second, testEnv,
		"go", "run", "./cmd/judgecheck", "-fault", v.Fault,
		"-in", "/out/observations.json", "-out", "/out", "-dsn", v.Runner.DSN(),
		"-base-revision", v.BaseRevision, "-patch-digest", v.PatchDigest)
	judge.Step = "judge"
	out.Steps = append(out.Steps, judge)

	data, err := os.ReadFile(filepath.Join(v.ArtifactDir, "eval.json"))
	if err != nil {
		out.Eval = &EvalSummary{Accepted: false, Reason: "the verifier produced no eval.json: " + err.Error()}
		return out
	}
	var ef evalFile
	if err := json.Unmarshal(data, &ef); err != nil {
		out.Eval = &EvalSummary{Accepted: false, Reason: "eval.json is unreadable: " + err.Error()}
		return out
	}
	out.OracleVerdict = ef.Oracle.Verdict
	for _, inv := range ef.Oracle.Invariants {
		if inv.Status == "FAIL" {
			out.Failing = append(out.Failing, fmt.Sprintf("%s: %s", inv.ID, inv.Summary))
		}
	}
	out.Eval = &EvalSummary{
		Accepted:          ef.Accepted,
		Reason:            ef.Reason,
		OrderStatus:       ef.OrderStatus,
		ClaimedCaptureID:  ef.ClaimedCaptureID,
		ProviderCaptureID: ef.ProviderCaptureID,
		OracleVerdict:     ef.Oracle.Verdict,
		FailingInvariants: out.Failing,
	}
	return out
}

// Feedback renders the failure evidence handed back to the model on a retry.
// It is bounded and redacted like every other artifact.
func Feedback(attempt int, v Verification) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Attempt %d failed independent verification.\n", attempt)
	for _, s := range v.Steps {
		if s.Passed() && s.Step != "eval" {
			continue
		}
		if s.Step == "eval" && v.Eval != nil {
			fmt.Fprintf(&b, "\n[eval] oracle verdict: %s\n", v.OracleVerdict)
			fmt.Fprintf(&b, "[eval] accepted: %v — %s\n", v.Eval.Accepted, v.Eval.Reason)
			for _, f := range v.Failing {
				fmt.Fprintf(&b, "[eval] invariant failure: %s\n", f)
			}
			continue
		}
		fmt.Fprintf(&b, "\n[%s] exit=%d timed_out=%v\n%s\n", s.Step, s.ExitCode, s.TimedOut, redactText(tail(s.Output, 4096)))
	}
	b.WriteString("\nProduce a NEW complete patch that fixes the root cause. Do not repeat the same edit; the verifier will run it again from scratch.\n")
	return b.String()
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}
