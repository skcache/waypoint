package repair

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Options configures one bounded repair run.
type Options struct {
	RepoRoot string
	BaseRef  string
	Fault    string
	// CaseID selects an M5 benchmark journey in the trusted verifier. It never
	// reaches the model prompt; the report may name it for human review.
	CaseID           string
	OutDir           string
	Mode             string // "oracle" (feedback loop) or "baseline" (single shot)
	MaxAttempts      int
	Model            Model
	Evidence         Evidence
	Allow            Allowlist
	RepoStatusBefore string
	Revision         string
	RevisionSource   string
	IncidentID       string
	// Runner is the OS-enforced candidate execution boundary. When nil, Run
	// opens a Docker sandbox and closes it; when provided (tests), the caller
	// owns its lifetime. There is no host fallback.
	Runner Runner
}

// Run executes the bounded investigation → patch → verify loop. It never
// applies anything to the developer's tree and never retries forever: the hard
// attempt cap is 3, default 2, and baseline mode is always a single attempt.
func Run(ctx context.Context, opts Options) *Report {
	if opts.Allow.Root == "" {
		opts.Allow = DefaultAllowlist()
	}
	if opts.MaxAttempts <= 0 {
		opts.MaxAttempts = 2
	}
	if opts.MaxAttempts > 3 {
		opts.MaxAttempts = 3
	}
	if opts.Mode == "" {
		opts.Mode = "oracle"
	}
	if opts.Mode == "baseline" {
		opts.MaxAttempts = 1
	}

	rep := &Report{
		SchemaVersion:         1,
		IncidentID:            opts.IncidentID,
		Fault:                 opts.Fault,
		CaseID:                opts.CaseID,
		Mode:                  opts.Mode,
		EvidenceTier:          "F",
		BaseRevision:          opts.Revision,
		RevisionSource:        opts.RevisionSource,
		StartedAt:             time.Now().UTC(),
		Status:                StatusNotVerified,
		Applied:               false,
		Deployed:              false,
		HumanApprovalRequired: true,
	}
	rep.Limitations = append(rep.Limitations,
		"All provider interaction in this run is deterministic scripted-fixture instrumentation (tier F). No real PayPal Sandbox transaction was executed by the repair pipeline.",
		"The verifier judges behavior, not intent: a candidate that deletes the injected fault switch instead of fixing the underlying behavior can still pass if the resulting behavior is correct. Read the diff with that in mind.",
		"Resource bounds are wall-clock timeouts, capped output and an advisory Go memory limit (GOMEMLIMIT); there is no hard OS memory cgroup on this platform.",
		"Token usage is recorded only when the provider reports it; cost is not computed without a pricing source.",
		"The model proposes; the independent oracle and the fixture acceptance rules decide. VERIFIED_FOR_TESTED_SCENARIOS covers only the scenario that actually executed.",
		"Nothing was applied to the developer's working tree, committed, pushed, merged or deployed. A human owns any integration decision.",
	)
	if err := os.MkdirAll(opts.OutDir, 0o700); err != nil {
		rep.Limitations = append(rep.Limitations, "artifact directory could not be created: "+err.Error())
		return rep
	}
	attemptsDir := filepath.Join(opts.OutDir, "attempts")
	_ = os.MkdirAll(attemptsDir, 0o700)

	// 1. Diagnosis.
	sys := diagnosisSystem()
	user := diagnosisUser(opts.Evidence)
	reply, err := opts.Model.Complete(ctx, "diagnosis", sys, user)
	meta := reply.Meta
	if meta.Model == "" {
		meta.Model = opts.Model.Name()
	}
	rep.ModelCalls = append(rep.ModelCalls, meta)
	rep.aggregate(meta)
	if err != nil {
		rep.Status = StatusError
		rep.Error = "the model call failed: " + err.Error()
		rep.Limitations = append(rep.Limitations, rep.Error)
		return finish(rep, opts)
	}
	_ = os.WriteFile(filepath.Join(opts.OutDir, "diagnosis-reply.txt"), []byte(redact(reply.Text, "")), 0o600)
	diag, err := ParseDiagnosis(reply.Text, opts.Evidence, opts.Allow)
	if err != nil {
		rep.Status = StatusError
		rep.Error = "the diagnosis reply failed schema validation: " + err.Error()
		rep.Limitations = append(rep.Limitations, rep.Error)
		return finish(rep, opts)
	}
	rep.Diagnosis = diag
	if data, err := json.MarshalIndent(diag, "", "  "); err == nil {
		_ = os.WriteFile(filepath.Join(opts.OutDir, "diagnosis.json"), append(data, '\n'), 0o600)
	}

	// 2. Candidate execution boundary. Fail closed if it cannot be opened.
	runner := opts.Runner
	owned := false
	if runner == nil {
		r, err := OpenSandbox(ctx, opts.RepoRoot)
		if err != nil {
			rep.Status = StatusError
			rep.Error = "the candidate execution sandbox is unavailable; refusing to run model-generated code on the host (fail closed): " + err.Error()
			rep.Limitations = append(rep.Limitations, rep.Error)
			return finish(rep, opts)
		}
		runner = r
		owned = true
	}
	if owned {
		defer runner.Close()
	}
	rep.Sandbox = runner.Describe()

	// 3. Bounded attempts.
	feedback := ""
	for n := 1; n <= opts.MaxAttempts; n++ {
		a := Attempt{Number: n, StartedAt: time.Now().UTC()}
		a.Ladder.DiagnosisProduced = rep.Diagnosis != nil
		dir := filepath.Join(attemptsDir, fmt.Sprintf("attempt-%d", n))
		_ = os.MkdirAll(dir, 0o700)

		patchReply, err := opts.Model.Complete(ctx, "patch", patchSystem(opts.Allow), patchUser(opts.Evidence, diag, feedback))
		pmeta := patchReply.Meta
		if pmeta.Model == "" {
			pmeta.Model = opts.Model.Name()
		}
		rep.ModelCalls = append(rep.ModelCalls, pmeta)
		rep.aggregate(pmeta)
		_ = os.WriteFile(filepath.Join(dir, "patch-reply.txt"), []byte(redact(patchReply.Text, "")), 0o600)
		if err != nil {
			a.Error = "the model call failed: " + err.Error()
			a.FinishedAt = time.Now().UTC()
			rep.Attempts = append(rep.Attempts, a)
			rep.Status = StatusError
			rep.Error = a.Error
			break
		}

		patch, err := ParsePatch(patchReply.Text, opts.Allow)
		if err != nil {
			a.Error = err.Error()
			a.FinishedAt = time.Now().UTC()
			rep.Attempts = append(rep.Attempts, a)
			if opts.Mode == "baseline" {
				break
			}
			feedback = "The previous patch was rejected before execution: " + err.Error() + "\nProduce a valid patch that satisfies the schema and the writable surface.\n"
			continue
		}
		a.Patch = patch
		a.Ladder.PatchGenerated = true
		if data, err := json.MarshalIndent(patch, "", "  "); err == nil {
			_ = os.WriteFile(filepath.Join(dir, "patch.json"), append(data, '\n'), 0o600)
		}

		ws, err := NewWorkspace(ctx, opts.RepoRoot, opts.BaseRef)
		if err != nil {
			a.Error = err.Error()
			a.FinishedAt = time.Now().UTC()
			rep.Attempts = append(rep.Attempts, a)
			rep.Status = StatusError
			rep.Error = a.Error
			break
		}
		changed, err := ws.ApplyEdits(patch.Edits, opts.Allow)
		if err == nil {
			a.Changed = changed
			a.Ladder.PatchApplied = true
			if diff, derr := ws.Diff(); derr == nil {
				a.Diff = diff
				a.PatchDigest = patchDigest(diff)
				_ = os.WriteFile(filepath.Join(dir, "patch.diff"), []byte(diff), 0o600)
			}
			v := (&Verifier{
				Workspace: ws, Runner: runner, Fault: opts.Fault, CaseID: opts.CaseID, ArtifactDir: dir,
				Changed: changed, BaseRevision: opts.Revision, PatchDigest: a.PatchDigest,
			}).Run(ctx)
			a.Steps = v.Steps
			a.Eval = v.Eval
			a.Ladder.OracleVerdict = v.OracleVerdict
			for _, s := range v.Steps {
				switch s.Step {
				case "build":
					a.Ladder.BuildPassed = s.Passed()
				case "tests":
					a.Ladder.TestsPassed = s.Passed()
				}
			}
			if v.Eval != nil && v.Eval.Accepted && allPassed(v.Steps) {
				a.Ladder.Accepted = true
			}
			if !a.Ladder.Accepted {
				feedback = Feedback(n, v)
				_ = os.WriteFile(filepath.Join(dir, "feedback.txt"), []byte(feedback), 0o600)
			}
		} else {
			a.Error = err.Error()
			feedback = "The previous patch could not be applied: " + err.Error() + "\nProduce a new patch whose old_text matches exactly once and stays inside the writable surface.\n"
		}
		ws.Close()
		a.FinishedAt = time.Now().UTC()
		rep.Attempts = append(rep.Attempts, a)

		if a.Ladder.Accepted {
			rep.PatchDigest = a.PatchDigest
			break
		}
		if opts.Mode == "baseline" {
			break
		}
	}

	// 4. Isolation proof: the developer's tree must be byte-for-byte unchanged.
	if opts.RepoStatusBefore != "" {
		if after, err := RepoStatus(opts.RepoRoot); err != nil || after != opts.RepoStatusBefore {
			rep.Status = StatusError
			rep.Limitations = append(rep.Limitations,
				"the developer's working tree changed during the run; this is a pipeline defect and the results must not be trusted")
			return finish(rep, opts)
		}
	}
	return finish(rep, opts)
}

func (r *Report) aggregate(m CallMeta) {
	r.PromptTokens += m.PromptTokens
	r.CompletionTokens += m.CompletionTokens
	if m.UsageReported {
		r.UsageReported = true
	}
}

// patchDigest is sha256 over the exact applied unified diff, prefixed for
// readability. It binds the proof artifact to the bytes a human reviews.
func patchDigest(diff string) string {
	sum := sha256.Sum256([]byte(diff))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func allPassed(steps []StepResult) bool {
	for _, s := range steps {
		if !s.Passed() {
			return false
		}
	}
	return true
}

func finish(rep *Report, opts Options) *Report {
	rep.FinishedAt = time.Now().UTC()
	if len(rep.Attempts) > 0 {
		rep.Final = rep.Attempts[len(rep.Attempts)-1].Ladder
	}
	rep.Final.DiagnosisProduced = rep.Diagnosis != nil
	for _, a := range rep.Attempts {
		if a.Ladder.Accepted {
			rep.Final = a.Ladder
			rep.Status = StatusAccepted
			break
		}
	}
	if rep.Status != StatusAccepted && rep.Status != StatusError {
		rep.Status = StatusNotVerified
	}
	if rep.Status != StatusAccepted {
		rep.Limitations = append(rep.Limitations,
			"No repair candidate passed independent verification in this run; the incident remains open.")
	}
	_ = writeReportArtifacts(rep, opts.OutDir)
	return rep
}

// ---- reporting -------------------------------------------------------------

func writeReportArtifacts(rep *Report, outDir string) error {
	sanitizeReport(rep)
	if data, err := MarshalReport(rep); err == nil {
		data = []byte(redactText(string(data)))
		_ = os.WriteFile(filepath.Join(outDir, "report.json"), data, 0o600)
	}
	md := renderMarkdown(rep)
	_ = os.WriteFile(filepath.Join(outDir, "report.md"), []byte(md), 0o600)
	// Attempt logs (bounded, already redacted).
	for _, a := range rep.Attempts {
		dir := filepath.Join(outDir, "attempts", fmt.Sprintf("attempt-%d", a.Number))
		var log strings.Builder
		for _, s := range a.Steps {
			fmt.Fprintf(&log, "$ %s\n(exit %d, %dms, timed_out=%v)\n%s\n\n",
				strings.Join(redactCommand(s.Command), " "), s.ExitCode, s.DurationMS, s.TimedOut, redactText(s.Output))
		}
		_ = os.WriteFile(filepath.Join(dir, "steps.log"), []byte(log.String()), 0o600)
	}
	return nil
}

// renderMarkdown is the human review surface. It is deliberately explicit about
// what each rung means.
func renderMarkdown(rep *Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Waypoint repair report\n\n")
	fmt.Fprintf(&b, "**Incident:** %s  \n**Fault family:** %s (evidence tier %s)  \n**Base revision:** %s (%s)  \n**Mode:** %s  \n**Status:** `%s`\n\n",
		rep.IncidentID, rep.Fault, rep.EvidenceTier, rep.BaseRevision, rep.RevisionSource, rep.Mode, rep.Status)

	if rep.PatchDigest != "" {
		fmt.Fprintf(&b, "**Candidate base revision:** `%s`  \n**Patch digest:** `%s`\n\n", rep.BaseRevision, rep.PatchDigest)
	} else {
		fmt.Fprintf(&b, "**Candidate base revision:** `%s`\n\n", rep.BaseRevision)
	}
	b.WriteString("## Verification ladder\n\n| Stage | Result |\n|---|---|\n")
	b.WriteString(fmt.Sprintf("| Diagnosis produced | %v |\n", rep.Diagnosis != nil))
	b.WriteString(fmt.Sprintf("| Patch generated | %v |\n", rep.Final.PatchGenerated))
	b.WriteString(fmt.Sprintf("| Patch applied | %v |\n", rep.Final.PatchApplied))
	b.WriteString(fmt.Sprintf("| Build passed | %v |\n", rep.Final.BuildPassed))
	b.WriteString(fmt.Sprintf("| Trusted tests passed | %v |\n", rep.Final.TestsPassed))
	b.WriteString(fmt.Sprintf("| Oracle verdict | %s |\n", rep.Final.OracleVerdict))
	b.WriteString(fmt.Sprintf("| Accepted for human review | %v |\n\n", rep.Final.Accepted))

	if rep.Diagnosis != nil {
		b.WriteString("## Diagnosis\n\n")
		fmt.Fprintf(&b, "**Root cause:** %s\n\n**Mechanism:** %s\n\n**Proposed fix:** %s\n\n", rep.Diagnosis.RootCause, rep.Diagnosis.Mechanism, rep.Diagnosis.ProposedFix)
		fmt.Fprintf(&b, "- Suspected files: %s\n- Evidence: %s\n- Invariants at risk: %s\n- Uncertainties: %s\n\n",
			strings.Join(rep.Diagnosis.SuspectedFiles, ", "),
			strings.Join(rep.Diagnosis.SupportingEvidence, ", "),
			strings.Join(rep.Diagnosis.InvariantsAtRisk, ", "),
			strings.Join(rep.Diagnosis.Uncertainties, "; "))
	}

	for _, a := range rep.Attempts {
		fmt.Fprintf(&b, "## Attempt %d — `%s`\n\n", a.Number, a.Ladder.Stage())
		if a.Error != "" {
			fmt.Fprintf(&b, "**Rejected:** %s\n\n", a.Error)
		}
		if a.Patch != nil {
			fmt.Fprintf(&b, "**Patch summary:** %s\n\n", a.Patch.Summary)
		}
		if a.Diff != "" {
			b.WriteString("```diff\n")
			b.WriteString(strings.TrimRight(a.Diff, "\n"))
			b.WriteString("\n```\n\n")
		}
		if a.Eval != nil {
			fmt.Fprintf(&b, "**Fixture acceptance:** %v — %s\n\n", a.Eval.Accepted, a.Eval.Reason)
			fmt.Fprintf(&b, "**Oracle:** %s  \n**Order status:** %s  \n**Claimed capture:** %s  \n**Provider capture:** %s\n\n",
				a.Eval.OracleVerdict, a.Eval.OrderStatus, a.Eval.ClaimedCaptureID, a.Eval.ProviderCaptureID)
			if len(a.Eval.FailingInvariants) > 0 {
				b.WriteString("Failing invariants:\n")
				for _, f := range a.Eval.FailingInvariants {
					fmt.Fprintf(&b, "- %s\n", f)
				}
				b.WriteString("\n")
			}
		}
		for _, s := range a.Steps {
			fmt.Fprintf(&b, "- `%s`: exit %d, %dms%s\n", s.Step, s.ExitCode, s.DurationMS, map[bool]string{true: ", timed out"}[s.TimedOut])
		}
		b.WriteString("\n")
	}

	if rep.Baseline != nil {
		b.WriteString("## Baseline comparison\n\n")
		fmt.Fprintf(&b, "| Mode | Attempts | Accepted | Stage | Latency |\n|---|---|---|---|---|\n")
		fmt.Fprintf(&b, "| %s | %d | %v | %s | %dms |\n\n", rep.Baseline.Mode, rep.Baseline.Attempts, rep.Baseline.Accepted, rep.Baseline.Stage, rep.Baseline.LatencyMS)
	}

	b.WriteString("## Model usage\n\n")
	for _, m := range rep.ModelCalls {
		fmt.Fprintf(&b, "- %s — %s, %dms, prompt %dB, reply %dB", m.Purpose, m.Model, m.LatencyMS, m.PromptBytes, m.ResponseBytes)
		if m.UsageReported {
			fmt.Fprintf(&b, ", %d+%d tokens", m.PromptTokens, m.CompletionTokens)
		} else {
			b.WriteString(", tokens not reported")
		}
		b.WriteString("\n")
	}
	if rep.Sandbox != "" {
		fmt.Fprintf(&b, "\nCandidate sandbox: %s\n", rep.Sandbox)
	}
	if rep.CostUSD != nil {
		fmt.Fprintf(&b, "\nCost: $%.4f\n", *rep.CostUSD)
	} else {
		b.WriteString("\nCost: not computed (the provider did not report pricing).\n")
	}

	b.WriteString("\n## What was applied or deployed\n\n")
	fmt.Fprintf(&b, "- Applied to the developer's working tree: **%v**\n- Committed/pushed/merged: **false**\n- Deployed: **%v**\n- Human approval required before any integration: **%v**\n\n",
		rep.Applied, rep.Deployed, rep.HumanApprovalRequired)

	b.WriteString("## Known limitations\n\n")
	for _, l := range rep.Limitations {
		fmt.Fprintf(&b, "- %s\n", l)
	}

	b.WriteString("\n## Human review checklist\n\n")
	b.WriteString("- [ ] Read the diff and confirm it fixes behavior rather than suppressing detection.\n")
	b.WriteString("- [ ] Run `make test-integration` and the oracle suite on the patched revision.\n")
	b.WriteString("- [ ] Confirm the evidence tier is understood (F = scripted, R = real Sandbox).\n")
	b.WriteString("- [ ] Decide whether a real Sandbox checkout is required before integration.\n")
	return b.String()
}

// NotConfigured builds the honest artifact for a run that could not call a
// hosted model. It never fabricates a diagnosis or a patch.
func NotConfigured(opts Options, cause error) *Report {
	rep := &Report{
		SchemaVersion:         1,
		IncidentID:            opts.IncidentID,
		Fault:                 opts.Fault,
		CaseID:                opts.CaseID,
		Mode:                  opts.Mode,
		EvidenceTier:          "F",
		BaseRevision:          opts.Revision,
		RevisionSource:        opts.RevisionSource,
		StartedAt:             time.Now().UTC(),
		Status:                StatusNotConfigured,
		HumanApprovalRequired: true,
		Limitations: []string{
			"No hosted model is configured (LLM_API_BASE_URL / LLM_API_KEY / LLM_MODEL), so no diagnosis or patch was requested. The live AI evaluation is BLOCKED; nothing here is a simulated model result.",
			"All provider interaction in this run is deterministic scripted-fixture instrumentation (tier F). No real PayPal Sandbox transaction was executed.",
			"Nothing was applied, committed, pushed, merged or deployed.",
		},
	}
	if cause != nil {
		rep.Limitations = append(rep.Limitations, "configuration check: "+cause.Error())
	}
	_ = writeReportArtifacts(rep, opts.OutDir)
	return rep
}

// WriteReport re-renders the report artifacts (used after attaching a baseline
// comparison).
func WriteReport(rep *Report, outDir string) error { return writeReportArtifacts(rep, outDir) }
