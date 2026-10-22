package bench

import (
	"encoding/json"

	"github.com/skcache/waypoint/apps/api/internal/repair"
)

// PublicResult is the sanitized run record for the review workbench. It keeps
// the reviewable outcome and usage numbers; evaluator-local artifact paths are
// deliberately absent.
type PublicResult struct {
	CaseID           string   `json:"case_id"`
	Condition        string   `json:"condition"`
	Status           string   `json:"status"`
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
}

// PublicRun adds the reviewable artifacts: the generated diff, the trusted
// judge's eval, the pre-repair eval, the model replies and provider
// observations. None of these contain evaluator-only expectations.
type PublicRun struct {
	PublicResult
	Report       *repair.Report  `json:"report,omitempty"`
	Diff         string          `json:"diff,omitempty"`
	PreEval      json.RawMessage `json:"pre_eval,omitempty"`
	Eval         json.RawMessage `json:"eval,omitempty"`
	Diag         json.RawMessage `json:"diagnosis_reply,omitempty"`
	Patch        json.RawMessage `json:"patch_reply,omitempty"`
	Observations json.RawMessage `json:"observations,omitempty"`
}

// PublicCase is the only case metadata the workbench receives. Mutations,
// reference fixes, expected categories, gate configuration and prompt facts
// stay evaluator-only.
type PublicCase struct {
	ID      string `json:"id"`
	Family  string `json:"family"`
	Title   string `json:"title"`
	Mode    string `json:"mode"`
	Exposed bool   `json:"exposed"`
}

type PublicCaseRow struct {
	Case        PublicCase              `json:"case"`
	MutationSHA string                  `json:"mutation_sha"`
	PreRepair   EvalView                `json:"pre_repair"`
	Runs        map[string]PublicResult `json:"runs"`
}

// PublicDoc is the sanitized projection consumed by apps/web. It is built from
// the dataset and the per-run artifacts; it never embeds the corpus
// definitions.
type PublicDoc struct {
	SchemaVersion int                              `json:"schema_version"`
	BenchmarkID   string                           `json:"benchmark_id"`
	Digest        string                           `json:"digest"`
	FrozenAt      string                           `json:"frozen_at"`
	BaseSHA       string                           `json:"base_sha"`
	Model         ModelInfo                        `json:"model"`
	Conditions    []ConditionInfo                  `json:"conditions"`
	Cases         []PublicCaseRow                  `json:"cases"`
	Totals        Totals                           `json:"totals"`
	Notes         []string                         `json:"notes"`
	Views         map[string]map[string]*PublicRun `json:"views"`
}

// PublicResultFrom converts one evaluator result into the public projection.
func PublicResultFrom(r Result) PublicResult {
	return PublicResult{
		CaseID: r.CaseID, Condition: r.Condition, Status: r.Status, Accepted: r.Accepted,
		Diagnosis: r.Diagnosis, DiagnosisOK: r.DiagnosisOK,
		PatchApplied: r.PatchApplied, BuildPassed: r.BuildPassed, TestsPassed: r.TestsPassed,
		OracleVerdict: r.OracleVerdict, Attempts: r.Attempts, LatencyMS: r.LatencyMS,
		PromptTokens: r.PromptTokens, CompletionTokens: r.CompletionTokens,
		EstimatedUSD: r.EstimatedUSD, Error: r.Error,
	}
}

// Project builds the explicit sanitized document. It copies only fields that
// are safe for a public, unauthenticated review surface.
func Project(ds *Dataset, views map[string]map[string]*PublicRun) *PublicDoc {
	doc := &PublicDoc{
		SchemaVersion: ds.SchemaVersion,
		BenchmarkID:   ds.BenchmarkID,
		Digest:        ds.Digest,
		FrozenAt:      ds.FrozenAt,
		BaseSHA:       ds.BaseSHA,
		Model:         ds.Model,
		Conditions:    ds.Conditions,
		Totals:        ds.Totals,
		Notes:         ds.Notes,
		Views:         views,
	}
	for _, cr := range ds.Cases {
		row := PublicCaseRow{
			Case: PublicCase{
				ID: cr.Case.ID, Family: cr.Case.Family, Title: cr.Case.Title,
				Mode: cr.Case.Mode, Exposed: cr.Case.Exposed,
			},
			MutationSHA: cr.MutationSHA,
			PreRepair:   cr.PreRepair,
			Runs:        make(map[string]PublicResult, len(cr.Runs)),
		}
		for cond, run := range cr.Runs {
			row.Runs[cond] = PublicResultFrom(run)
		}
		doc.Cases = append(doc.Cases, row)
	}
	return doc
}
