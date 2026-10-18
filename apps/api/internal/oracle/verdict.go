package oracle

import (
	"fmt"
	"time"
)

// Result is the complete evaluation output for one scenario run.
type Result struct {
	Scenario    ScenarioMeta
	Candidate   string
	Verdict     Verdict
	Invariants  []InvariantResult
	Limitations []string
	EvaluatedAt time.Time
}

// Evaluate runs the frozen invariant registry and combines the verdict.
func Evaluate(ev *Evidence) Result {
	results := RunInvariants(ev)
	return Result{
		Scenario:    ev.Scenario,
		Candidate:   ev.Candidate,
		Verdict:     CombineVerdict(results),
		Invariants:  results,
		Limitations: defaultLimitations(ev),
		EvaluatedAt: time.Now().UTC(),
	}
}

// Skips lists the NOT_APPLICABLE invariants with their reasons.
func (r Result) Skips() []InvariantResult {
	var out []InvariantResult
	for _, inv := range r.Invariants {
		if inv.Status == StatusNotApplicable {
			out = append(out, inv)
		}
	}
	return out
}

// Counterexamples lists the failing invariants with their counterexamples.
func (r Result) Counterexamples() []InvariantResult {
	var out []InvariantResult
	for _, inv := range r.Invariants {
		if inv.Status == StatusFail {
			out = append(out, inv)
		}
	}
	return out
}

func defaultLimitations(ev *Evidence) []string {
	return []string{
		fmt.Sprintf("Verdict scope: candidate %q under scenario %q only; it says nothing about unexecuted scenarios, unseen code paths, or production traffic.", ev.Candidate, ev.Scenario.ID),
		fmt.Sprintf("Evidence tier: %s. Scenario evidence is deterministic fixture instrumentation; it is not a real PayPal Sandbox execution.", ev.Scenario.EvidenceTier),
		"Provider interactions are scripted-fixture observations (tier F); previously recorded real Sandbox evidence (tier R) is referenced by the M1 record but is not re-executed here.",
		"No deployed system was modified: the verdict is advisory and human-gated (I11).",
	}
}
