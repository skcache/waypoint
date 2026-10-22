package bench

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/skcache/waypoint/apps/api/internal/llm"
	"github.com/skcache/waypoint/apps/api/internal/oracle"
	"github.com/skcache/waypoint/apps/api/internal/repair"
	"github.com/skcache/waypoint/apps/api/internal/replay"
)

// Condition identifiers.
const (
	B0 = "B0"
	B1 = "B1"
	B2 = "B2"
)

// Options configures one benchmark execution.
type Options struct {
	RepoRoot      string
	BaseSHA       string
	OutDir        string
	WebDataDir    string
	DSN           string
	Model         llm.Config
	Condition     string // B0, B1, B2, or "" for all
	CaseIDs       []string
	MaxAttemptsB2 int
	// Resume merges this invocation's case results into an existing
	// results.json in OutDir instead of replacing it.
	Resume bool
}

// Result is one case under one condition.
type Result struct {
	CaseID           string   `json:"case_id"`
	Condition        string   `json:"condition"`
	Status           string   `json:"status"` // REPAIRED / SAFE_REFUSAL / REJECTED / ERROR / NOT_CONFIGURED
	Accepted         bool     `json:"accepted"`
	Diagnosis        string   `json:"diagnosis_category"`
	DiagnosisOK      bool     `json:"diagnosis_correct"`
	PatchApplied     bool     `json:"patch_applied"`
	BuildPassed      bool     `json:"build_passed"`
	TestsPassed      bool     `json:"tests_passed"`
	OracleVerdict    string   `json:"oracle_verdict"`
	Attempts         int      `json:"attempts"`
	LatencyMS        int64    `json:"latency_ms"`
	PromptTokens     int      `json:"prompt_tokens"`
	CompletionTokens int      `json:"completion_tokens"`
	EstimatedUSD     *float64 `json:"estimated_usd,omitempty"`
	Error            string   `json:"error,omitempty"`
	Artifacts        string   `json:"artifacts"`
}

// CaseReport bundles the corpus metadata with its runs.
type CaseReport struct {
	Case        Case              `json:"case"`
	MutationSHA string            `json:"mutation_sha"`
	PreRepair   EvalView          `json:"pre_repair"`
	Runs        map[string]Result `json:"runs"`
}

// Dataset is the machine-readable benchmark result.
type Dataset struct {
	SchemaVersion int             `json:"schema_version"`
	BenchmarkID   string          `json:"benchmark_id"`
	Digest        string          `json:"digest"`
	FrozenAt      string          `json:"frozen_at"`
	BaseSHA       string          `json:"base_sha"`
	Model         ModelInfo       `json:"model"`
	Conditions    []ConditionInfo `json:"conditions"`
	Cases         []CaseReport    `json:"cases"`
	Totals        Totals          `json:"totals"`
	Notes         []string        `json:"notes"`
}

type ModelInfo struct {
	ID          string `json:"id,omitempty"`
	BaseURL     string `json:"base_url,omitempty"`
	MaxOutput   int    `json:"max_output_tokens,omitempty"`
	Temperature string `json:"temperature"`
	CostNote    string `json:"cost_note"`
	RateTable   string `json:"rate_table,omitempty"`
}

type ConditionInfo struct {
	ID             string `json:"id"`
	Description    string `json:"description"`
	MaxAttempts    int    `json:"max_attempts"`
	OracleFeedback bool   `json:"oracle_feedback"`
}

type Totals struct {
	HeldOutCases             int                         `json:"held_out_cases"`
	SafeAccepted             int                         `json:"safe_accepted"`
	VerifiedAccepted         int                         `json:"verified_accepted"`
	SafeRefusals             int                         `json:"safe_refusals"`
	Rejected                 int                         `json:"rejected"`
	Errors                   int                         `json:"errors"`
	NotRun                   int                         `json:"not_run"`
	FalseAcceptances         int                         `json:"false_acceptances"`
	UnsafeCandidatesRejected int                         `json:"unsafe_candidates_rejected"`
	PromptTokens             int                         `json:"prompt_tokens"`
	CompletionTokens         int                         `json:"completion_tokens"`
	EstimatedUSD             *float64                    `json:"estimated_usd,omitempty"`
	ByCondition              map[string]*ConditionTotals `json:"by_condition"`
}

type ConditionTotals struct {
	SafeAccepted     int      `json:"safe_accepted"`
	VerifiedAccepted int      `json:"verified_accepted"`
	SafeRefusals     int      `json:"safe_refusals"`
	Rejected         int      `json:"rejected"`
	Errors           int      `json:"errors"`
	NotRun           int      `json:"not_run"`
	FalseAcceptances int      `json:"false_acceptances"`
	PromptTokens     int      `json:"prompt_tokens"`
	CompletionTokens int      `json:"completion_tokens"`
	EstimatedUSD     *float64 `json:"estimated_usd,omitempty"`
}

// Digest returns a stable digest of the model-facing corpus definition. The
// reference fixes and expected categories are NOT part of the digest exposed
// to the agent; they are included here only so humans can freeze the whole
// evaluator definition. The digest is recorded before any hosted run.
func Digest() string {
	type dCase struct {
		ID        string     `json:"id"`
		Journey   string     `json:"journey"`
		Mode      string     `json:"mode"`
		Mutations []Mutation `json:"mutations"`
	}
	var list []dCase
	for _, c := range Cases() {
		list = append(list, dCase{c.ID, c.Journey, c.Mode, c.Mutations})
	}
	data, _ := json.Marshal(list)
	return "sha256:" + sha256Hex(data)
}

func Run(ctx context.Context, opts Options) (*Dataset, error) {
	if opts.MaxAttemptsB2 <= 0 {
		opts.MaxAttemptsB2 = 2
	}
	ds := &Dataset{
		SchemaVersion: 1,
		BenchmarkID:   "waypoint-m5-heldout-v1",
		Digest:        Digest(),
		FrozenAt:      time.Now().UTC().Format(time.RFC3339),
		BaseSHA:       opts.BaseSHA,
		Model: ModelInfo{
			ID: opts.Model.Model, BaseURL: opts.Model.BaseURL,
			MaxOutput: opts.Model.MaxOutputTokens, Temperature: "0",
			CostNote: "Estimated from the provider's published per-token rates; pricing is not reported by the API call itself.",
		},
		Conditions: []ConditionInfo{
			{ID: B0, Description: "Deterministic rules over the incident evidence; diagnosis only, no patch capability", MaxAttempts: 0},
			{ID: B1, Description: "Single diagnosis + single patch, no oracle feedback", MaxAttempts: 1},
			{ID: B2, Description: "Same initial attempt, then bounded retries with independent failure evidence", MaxAttempts: opts.MaxAttemptsB2, OracleFeedback: true},
		},
		Notes: []string{
			"This is a small held-out corpus, not a statistical benchmark. Counts are raw; no significance is claimed.",
			"Zero observed false acceptances does not establish a zero real-world failure probability.",
			"All provider interaction is tier F (scripted fixture). No PayPal transaction or signed webhook was used.",
			"Exposed cases are reported separately and excluded from held-out totals.",
		},
	}
	if strings.Contains(opts.Model.Model, "deepseek-v4.1-flash") {
		ds.Model.RateTable = "$0.30/M prompt, $1.20/M completion (OpenRouter list price at freeze time)"
	}
	if opts.Model.Model == "" {
		ds.Model.ID = "NOT_CONFIGURED"
	}

	cases := Cases()
	if len(opts.CaseIDs) > 0 {
		var filtered []Case
		for _, id := range opts.CaseIDs {
			c, err := CaseByID(id)
			if err != nil {
				return nil, err
			}
			filtered = append(filtered, c)
		}
		cases = filtered
	}
	for i := range cases {
		cr, err := runCase(ctx, opts, cases[i])
		if err != nil {
			cr = CaseReport{Case: cases[i], Runs: map[string]Result{
				"ERROR": {CaseID: cases[i].ID, Condition: "ERROR", Status: "ERROR", Error: err.Error()},
			}}
		}
		ds.Cases = append(ds.Cases, cr)
	}
	if opts.Resume {
		ds.Cases = mergeCases(loadPrior(opts.OutDir), ds.Cases)
	}
	ds.Totals = totalize(ds.Cases)
	return ds, nil
}

func loadPrior(outDir string) []CaseReport {
	raw, err := os.ReadFile(filepath.Join(outDir, "results.json"))
	if err != nil {
		return nil
	}
	var prior Dataset
	if json.Unmarshal(raw, &prior) != nil {
		return nil
	}
	return prior.Cases
}

func mergeCases(prior, fresh []CaseReport) []CaseReport {
	byID := map[string]CaseReport{}
	order := []string{}
	for _, c := range prior {
		byID[c.Case.ID] = c
		order = append(order, c.Case.ID)
	}
	for _, c := range fresh {
		if old, ok := byID[c.Case.ID]; ok {
			for cond, run := range c.Runs {
				old.Runs[cond] = run
			}
			c.Runs = old.Runs
		} else {
			order = append(order, c.Case.ID)
		}
		byID[c.Case.ID] = c
	}
	out := make([]CaseReport, 0, len(order))
	for _, id := range order {
		out = append(out, byID[id])
	}
	return out
}

func runCase(ctx context.Context, opts Options, c Case) (CaseReport, error) {
	cr := CaseReport{Case: c, Runs: map[string]Result{}}
	ws, sha, err := prepareWorkspace(ctx, opts.RepoRoot, opts.BaseSHA, c)
	if err != nil {
		return cr, err
	}
	defer removeWorkspace(opts.RepoRoot, ws)
	cr.MutationSHA = sha

	art := filepath.Join(opts.OutDir, "cases", c.ID)
	preDir := filepath.Join(art, "pre-repair")
	if err := os.MkdirAll(preDir, 0o700); err != nil {
		return cr, err
	}
	pre, err := preRepair(ctx, ws, opts.RepoRoot, opts.DSN, c, preDir)
	if err != nil {
		return cr, fmt.Errorf("pre-repair: %w", err)
	}
	cr.PreRepair = pre

	allow := repair.AllowlistForFault(c.GateFault)
	ev, err := buildEvidence(ctx, opts.DSN, ws, sha, filepath.Join(preDir, "observations.json"), c, pre, allow)
	if err != nil {
		return cr, fmt.Errorf("evidence: %w", err)
	}
	if err := ev.ValidateForModel(); err != nil {
		return cr, fmt.Errorf("evidence validation: %w", err)
	}

	if opts.Condition == "" || opts.Condition == B0 {
		r := runB0(c, pre, ev)
		r.Artifacts = preDir
		cr.Runs[B0] = r
	}
	for _, cond := range []string{B1, B2} {
		if opts.Condition != "" && opts.Condition != cond {
			continue
		}
		if !opts.Model.Configured() && cond != B0 {
			cr.Runs[cond] = Result{CaseID: c.ID, Condition: cond, Status: "NOT_CONFIGURED", Artifacts: art}
			continue
		}
		r := runRepair(ctx, opts, c, ws, sha, ev, allow, cond, art)
		cr.Runs[cond] = r
	}
	return cr, nil
}

// prepareWorkspace creates a detached worktree at base, applies the mutation,
// and commits it so the repair pipeline can use the exact revision as BaseRef.
func prepareWorkspace(ctx context.Context, repo, base string, c Case) (string, string, error) {
	dir, err := os.MkdirTemp("", "waypoint-bench-"+c.ID+"-")
	if err != nil {
		return "", "", err
	}
	os.Remove(dir)
	if out, err := git(ctx, repo, nil, "worktree", "add", "--detach", dir, base); err != nil {
		return "", "", fmt.Errorf("worktree: %v: %s", err, out)
	}
	for _, m := range c.Mutations {
		full := filepath.Join(dir, filepath.FromSlash(m.Path))
		data, err := os.ReadFile(full)
		if err != nil {
			removeWorkspace(repo, dir)
			return "", "", err
		}
		if strings.Count(string(data), m.Old) != 1 {
			removeWorkspace(repo, dir)
			return "", "", fmt.Errorf("mutation for %s occurs %d times (want 1)", m.Path, strings.Count(string(data), m.Old))
		}
		if err := os.WriteFile(full, []byte(strings.Replace(string(data), m.Old, m.New, 1)), 0o644); err != nil {
			removeWorkspace(repo, dir)
			return "", "", err
		}
	}
	env := []string{
		"GIT_AUTHOR_NAME=Waypoint Bench", "GIT_AUTHOR_EMAIL=bench@waypoint.local",
		"GIT_COMMITTER_NAME=Waypoint Bench", "GIT_COMMITTER_EMAIL=bench@waypoint.local",
		"GIT_AUTHOR_DATE=2026-10-22T00:00:00Z", "GIT_COMMITTER_DATE=2026-10-22T00:00:00Z",
	}
	if out, err := git(ctx, dir, env, "add", "-A"); err != nil {
		return "", "", fmt.Errorf("git add: %v: %s", err, out)
	}
	if out, err := git(ctx, dir, env, "commit", "-m", "bench: "+c.ID); err != nil {
		return "", "", fmt.Errorf("git commit: %v: %s", err, out)
	}
	sha, err := revParse(ctx, dir)
	if err != nil {
		return "", "", err
	}
	if out, err := goRun(ctx, filepath.Join(dir, "apps/api"), "build", "./..."); err != nil {
		removeWorkspace(repo, dir)
		return "", "", fmt.Errorf("mutated workspace does not build: %v: %s", err, out)
	}
	return dir, sha, nil
}

type EvalView struct {
	Accepted    bool     `json:"accepted"`
	Reason      string   `json:"reason"`
	OrderStatus string   `json:"order_status"`
	Verdict     string   `json:"verdict"`
	Failing     []string `json:"failing,omitempty"`
}

func preRepair(ctx context.Context, ws, repo, dsn string, c Case, outDir string) (EvalView, error) {
	// Candidate-linked fixture run from the mutated workspace.
	if out, err := goRun(ctx, filepath.Join(ws, "apps/api"), "run", "./cmd/fixturecheck", "-case", c.ID, "-out", outDir, "-dsn", dsn); err != nil {
		return EvalView{}, fmt.Errorf("fixturecheck: %v: %s", err, out)
	}
	// Trusted judge from the unmutated repository.
	judgeOut, judgeErr := goRun(ctx, filepath.Join(repo, "apps/api"), "run", "./cmd/judgecheck", "-case", c.ID,
		"-in", filepath.Join(outDir, "observations.json"), "-out", outDir, "-dsn", dsn)
	if judgeErr != nil {
		// Expected for a defective revision; the eval file is still the verdict.
		_ = judgeOut
	}
	raw, err := os.ReadFile(filepath.Join(outDir, "eval.json"))
	if err != nil {
		return EvalView{}, err
	}
	var e struct {
		Accepted    bool   `json:"accepted"`
		Reason      string `json:"reason"`
		OrderStatus string `json:"order_status"`
		Oracle      struct {
			Verdict    string                                 `json:"verdict"`
			Invariants []struct{ ID, Status, Summary string } `json:"invariants"`
		} `json:"oracle"`
	}
	if err := json.Unmarshal(raw, &e); err != nil {
		return EvalView{}, err
	}
	v := EvalView{Accepted: e.Accepted, Reason: e.Reason, OrderStatus: e.OrderStatus, Verdict: e.Oracle.Verdict}
	for _, inv := range e.Oracle.Invariants {
		if inv.Status == "FAIL" {
			v.Failing = append(v.Failing, inv.ID+": "+inv.Summary)
		}
	}
	return v, nil
}

func buildEvidence(ctx context.Context, dsn, ws, sha, obsPath string, c Case, pre EvalView, allow repair.Allowlist) (repair.Evidence, error) {
	h, err := replay.OpenHarness(ctx, dsn)
	if err != nil {
		return repair.Evidence{}, err
	}
	defer h.Close()
	raw, err := os.ReadFile(obsPath)
	if err != nil {
		return repair.Evidence{}, err
	}
	var of struct {
		OrderID      string               `json:"order_id"`
		Observations []oracle.ProviderObs `json:"observations"`
	}
	if err := json.Unmarshal(raw, &of); err != nil {
		return repair.Evidence{}, err
	}
	obs := make([]replay.Observation, 0, len(of.Observations))
	for _, o := range of.Observations {
		obs = append(obs, replay.Observation{
			Seq: o.Seq, At: o.At, Method: o.Method, OrderRef: o.OrderRef, RequestID: o.RequestID,
			DurableBeforeCall: o.DurableBeforeCall, SentAmount: o.SentAmount, SentCurrency: o.SentCurrency,
			Applied: o.Applied, Replayed: o.Replayed, ResponseDropped: o.ResponseDropped,
			DefiniteRefusal: o.DefiniteRefusal, OrderStatus: o.OrderStatus, CaptureID: o.CaptureID,
			CaptureStatus: o.CaptureStatus, CaptureAmount: o.CaptureAmount, CaptureCurrency: o.CaptureCurrency,
			Stale: o.Stale, Source: o.Source, Tier: o.Tier,
		})
	}
	run := replay.FixtureRun{
		Fault: "benchmark", OrderID: of.OrderID, OrderStatus: pre.OrderStatus,
		Observations: obs, Facts: map[string]string{},
	}
	ev, err := repair.BuildEvidence(ctx, h, run, ws, sha, allow)
	if err != nil {
		return repair.Evidence{}, err
	}
	// Neutral label: the case id and expected outcome never enter a prompt.
	ev.Fault = "payment integration incident"
	return ev, nil
}

func runB0(c Case, pre EvalView, ev repair.Evidence) Result {
	cat := b0Category(pre)
	return Result{
		CaseID: c.ID, Condition: B0, Diagnosis: cat, DiagnosisOK: cat == c.ExpectedCategory,
		OracleVerdict: pre.Verdict, Status: "DIAGNOSIS_ONLY",
		Error: "B0 has no patch capability; it cannot repair code and is not scored as a repair system",
	}
}

var currencyToken = regexp.MustCompile(`\b[A-Z]{3}\b`)

func distinctCurrencies(s string) int {
	seen := map[string]bool{}
	for _, m := range currencyToken.FindAllString(s, -1) {
		seen[m] = true
	}
	return len(seen)
}

func b0Category(pre EvalView) string {
	joined := strings.Join(pre.Failing, " ")
	switch {
	case strings.Contains(joined, "I13") || strings.Contains(joined, "I14"):
		return "fulfillment"
	case strings.Contains(joined, "I3"):
		return "authorization"
	case strings.Contains(joined, "currency") || strings.Contains(joined, "I2"):
		return "monetary"
	case strings.Contains(joined, "I4") || strings.Contains(joined, "I7"):
		return "identity"
	case strings.Contains(joined, "I5"):
		return "ambiguity"
	case strings.Contains(joined, "I6"):
		if distinctCurrencies(joined) > 1 {
			return "monetary"
		}
		return "status"
	}
	return "unknown"
}

var categoryRules = []struct {
	cat string
	re  *regexp.Regexp
}{
	{"fulfillment", regexp.MustCompile(`(?i)webhook|fulfill|duplicate|event id`)},
	{"authorization", regexp.MustCompile(`(?i)approval|approve|created`)},
	{"monetary", regexp.MustCompile(`(?i)currency|amount|money`)},
	{"status", regexp.MustCompile(`(?i)status|pending|completed|status read`)},
	{"ambiguity", regexp.MustCompile(`(?i)ambiguous|unknown|reconcil`)},
	{"identity", regexp.MustCompile(`(?i)request id|identity|idempot|retry`)},
}

func classifyDiagnosis(d *repair.Diagnosis) string {
	if d == nil {
		return "unknown"
	}
	text := d.RootCause + " " + d.Mechanism + " " + d.ProposedFix + " " + strings.Join(d.SuspectedFiles, " ")
	for _, r := range categoryRules {
		if r.re.MatchString(text) {
			return r.cat
		}
	}
	return "unknown"
}

func runRepair(ctx context.Context, opts Options, c Case, ws, sha string, ev repair.Evidence, allow repair.Allowlist, cond, art string) Result {
	mode := "oracle"
	attempts := opts.MaxAttemptsB2
	if cond == B1 {
		mode = "baseline"
		attempts = 1
	}
	out := filepath.Join(art, cond)
	_ = os.MkdirAll(out, 0o700)
	start := time.Now()
	rep := repair.Run(ctx, repair.Options{
		RepoRoot: opts.RepoRoot, BaseRef: sha, Fault: "benchmark", CaseID: c.ID,
		OutDir: out, Mode: mode, MaxAttempts: attempts, Model: benchModel{cfg: opts.Model},
		Evidence: ev, Allow: allow, RepoStatusBefore: "", Revision: sha, RevisionSource: "bench-mutation",
		IncidentID: "M5-INC-" + c.ID,
	})
	r := Result{CaseID: c.ID, Condition: cond, Attempts: len(rep.Attempts), Artifacts: out}
	r.LatencyMS = time.Since(start).Milliseconds()
	r.OracleVerdict = rep.Final.OracleVerdict
	r.PromptTokens, r.CompletionTokens = rep.PromptTokens, rep.CompletionTokens
	r.Status = rep.Status
	r.Accepted = rep.Final.Accepted
	r.PatchApplied = rep.Final.PatchApplied
	r.BuildPassed = rep.Final.BuildPassed
	r.TestsPassed = rep.Final.TestsPassed
	r.Diagnosis = classifyDiagnosis(rep.Diagnosis)
	r.DiagnosisOK = r.Diagnosis == c.ExpectedCategory
	if len(rep.Attempts) > 0 {
		for _, a := range rep.Attempts {
			if a.Eval != nil && a.Eval.OracleVerdict != "" {
				r.OracleVerdict = a.Eval.OracleVerdict
			}
		}
	}
	if r.Accepted {
		if c.Mode == "refused" {
			r.Status = "SAFE_REFUSAL"
		} else {
			r.Status = "REPAIRED"
		}
	} else if rep.Status == "ERROR" {
		r.Status = "ERROR"
		r.Error = rep.Error
		if r.Error == "" {
			r.Error = lastError(rep.Attempts)
		}
	} else {
		r.Status = "REJECTED"
		if len(rep.Attempts) > 0 {
			r.Error = lastError(rep.Attempts)
		}
	}
	r.EstimatedUSD = estimateUSD(opts.Model.Model, r.PromptTokens, r.CompletionTokens)
	return r
}

func lastError(as []repair.Attempt) string {
	for i := len(as) - 1; i >= 0; i-- {
		if as[i].Error != "" {
			return as[i].Error
		}
	}
	return ""
}

func estimateUSD(model string, in, out int) *float64 {
	if !strings.Contains(model, "deepseek-v4.1-flash") || (in == 0 && out == 0) {
		return nil
	}
	cost := float64(in)*0.30/1e6 + float64(out)*1.20/1e6
	return &cost
}

func totalize(cases []CaseReport) Totals {
	t := Totals{ByCondition: map[string]*ConditionTotals{}}
	for _, cond := range []string{B0, B1, B2} {
		t.ByCondition[cond] = &ConditionTotals{}
	}
	for _, cr := range cases {
		if cr.Case.Exposed {
			continue
		}
		t.HeldOutCases++
		for _, cond := range []string{B1, B2} {
			if _, ok := cr.Runs[cond]; !ok {
				t.ByCondition[cond].NotRun++
				t.NotRun++
			}
		}
		for cond, r := range cr.Runs {
			ct := t.ByCondition[cond]
			if ct == nil {
				ct = &ConditionTotals{}
				t.ByCondition[cond] = ct
			}
			if cond != B0 {
				t.PromptTokens += r.PromptTokens
				t.CompletionTokens += r.CompletionTokens
				ct.PromptTokens += r.PromptTokens
				ct.CompletionTokens += r.CompletionTokens
				if r.EstimatedUSD != nil {
					addUSD(&t.EstimatedUSD, *r.EstimatedUSD)
					addUSD(&ct.EstimatedUSD, *r.EstimatedUSD)
				}
			}
			switch r.Status {
			case "REPAIRED":
				ct.VerifiedAccepted++
				if cond != B0 {
					t.VerifiedAccepted++
				}
			case "SAFE_REFUSAL":
				ct.SafeRefusals++
				if cond != B0 {
					t.SafeRefusals++
				}
			case "REJECTED":
				ct.Rejected++
				if cond != B0 {
					t.Rejected++
				}
			case "ERROR":
				ct.Errors++
				if cond != B0 {
					t.Errors++
				}
			}
			if r.Accepted && !cr.PreRepair.Accepted {
				ct.SafeAccepted++
				if cond != B0 {
					t.SafeAccepted++
				}
			}
			if r.Accepted && r.OracleVerdict == string(oracle.VerdictUnsafe) {
				ct.FalseAcceptances++
				if cond != B0 {
					t.FalseAcceptances++
				}
			}
		}
	}
	return t
}

func addUSD(dst **float64, v float64) {
	if *dst == nil {
		*dst = new(float64)
	}
	**dst += v
}

func git(ctx context.Context, dir string, env []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	if env != nil {
		cmd.Env = append(os.Environ(), env...)
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func revParse(ctx context.Context, dir string) (string, error) {
	out, err := git(ctx, dir, nil, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("rev-parse: %v: %s", err, out)
	}
	return strings.TrimSpace(out), nil
}

func removeWorkspace(repo, dir string) {
	_, _ = git(context.Background(), repo, nil, "worktree", "remove", "--force", dir)
}

func goRun(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if len(out) > 12000 {
		out = out[len(out)-12000:]
	}
	return string(out), err
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// benchModel adapts llm.Config to repair.Model.
type benchModel struct{ cfg llm.Config }

func (m benchModel) Name() string { return m.cfg.Model }

func (m benchModel) Complete(ctx context.Context, purpose, system, user string) (repair.Reply, error) {
	if m.cfg == (llm.Config{}) {
		return repair.Reply{}, fmt.Errorf("bench: no model configured")
	}
	client, err := llm.New(m.cfg)
	if err != nil {
		return repair.Reply{}, err
	}
	start := time.Now()
	c, err := client.GenerateDetail(ctx, system, user)
	meta := repair.CallMeta{Purpose: purpose, Model: m.cfg.Model, PromptBytes: len(system) + len(user), LatencyMS: time.Since(start).Milliseconds()}
	if err != nil {
		return repair.Reply{}, err
	}
	meta.ResponseBytes = len(c.Text)
	meta.PromptTokens = c.PromptTokens
	meta.CompletionTokens = c.CompletionTokens
	meta.UsageReported = c.UsageReported
	return repair.Reply{Text: c.Text, Meta: meta}, nil
}

var _ = sort.Strings
