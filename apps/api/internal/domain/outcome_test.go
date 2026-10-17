package domain

import "testing"

func TestAggregateDecisionMatrix(t *testing.T) {
	// The locked PRD §7.3 table.
	tests := []struct {
		policy   PolicyResult
		semantic SemanticClass
		want     Decision
	}{
		{PolicyAllow, SemanticSupported, DecisionAllow},
		{PolicyAllow, SemanticContradicted, DecisionDeny},
		{PolicyAllow, SemanticUnknown, DecisionReview},

		{PolicyDeny, SemanticSupported, DecisionDeny},
		{PolicyDeny, SemanticContradicted, DecisionDeny},
		{PolicyDeny, SemanticUnknown, DecisionDeny},

		{PolicyIndeterminate, SemanticSupported, DecisionReview},
		{PolicyIndeterminate, SemanticContradicted, DecisionReview},
		{PolicyIndeterminate, SemanticUnknown, DecisionReview},
	}
	for _, tc := range tests {
		got := AggregateDecision(tc.policy, tc.semantic)
		if got != tc.want {
			t.Errorf("AggregateDecision(%s, %s) = %s, want %s", tc.policy, tc.semantic, got, tc.want)
		}
	}
}

func TestHardDenyDominatesEvenInvalidSemantic(t *testing.T) {
	if got := AggregateDecision(PolicyDeny, SemanticClass("GARBAGE")); got != DecisionDeny {
		t.Fatalf("hard DENY must dominate a garbage model result, got %s", got)
	}
}

func TestAggregateDecisionFailsClosed(t *testing.T) {
	if got := AggregateDecision(PolicyResult("MAYBE"), SemanticSupported); got != DecisionReview {
		t.Fatalf("invalid policy must review, got %s", got)
	}
	if got := AggregateDecision(PolicyAllow, SemanticClass("")); got != DecisionReview {
		t.Fatalf("invalid semantic must review, got %s", got)
	}
	if got := AggregateDecision("", ""); got != DecisionReview {
		t.Fatalf("fully invalid inputs must review, got %s", got)
	}
}

func TestOutcomeValidators(t *testing.T) {
	for _, c := range []SemanticClass{SemanticSupported, SemanticContradicted, SemanticUnknown} {
		if !c.Valid() {
			t.Errorf("SemanticClass(%q) should be valid", c)
		}
	}
	for _, c := range []SemanticClass{"", "SUPPORTED ", "maybe"} {
		if c.Valid() {
			t.Errorf("SemanticClass(%q) should be invalid", c)
		}
	}
	for _, p := range []PolicyResult{PolicyAllow, PolicyDeny, PolicyIndeterminate} {
		if !p.Valid() {
			t.Errorf("PolicyResult(%q) should be valid", p)
		}
	}
	if PolicyResult("ALLOWED").Valid() {
		t.Error("PolicyResult(ALLOWED) should be invalid")
	}
	for _, d := range []Decision{DecisionAllow, DecisionDeny, DecisionReview} {
		if !d.Valid() {
			t.Errorf("Decision(%q) should be valid", d)
		}
	}
	if Decision("").Valid() {
		t.Error("empty decision should be invalid")
	}
}

func TestDecisionStateMapping(t *testing.T) {
	tests := []struct {
		d    Decision
		want TransactionState
	}{
		{DecisionAllow, StateAuthorized},
		{DecisionDeny, StateDenied},
		{DecisionReview, StateReviewRequired},
	}
	for _, tc := range tests {
		got, err := tc.d.State()
		if err != nil || got != tc.want {
			t.Errorf("Decision(%s).State() = %s, %v; want %s", tc.d, got, err, tc.want)
		}
		if !got.Valid() {
			t.Errorf("Decision(%s).State() returned invalid state", tc.d)
		}
	}
	if _, err := Decision("NONSENSE").State(); err == nil {
		t.Error("unknown decision must error, not default")
	}
}
