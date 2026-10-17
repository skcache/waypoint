package domain

import (
	"reflect"
	"testing"
)

// wantEdges is the expected PRD §7.4 matrix, written out independently of the
// implementation table so the two must agree by test, not by construction.
var wantEdges = map[TransactionState][]TransactionState{
	StateProposed:               {StateVerifying},
	StateVerifying:              {StateDenied, StateReviewRequired, StateAuthorized},
	StateAuthorized:             {StateOrderCreating, StateFailed},
	StateOrderCreating:          {StateAwaitingBuyerApproval, StateReconciliationRequired},
	StateAwaitingBuyerApproval:  {StateCancelled, StateCapturePending},
	StateCapturePending:         {StateCompleted, StateReconciliationRequired},
	StateReconciliationRequired: {StateCompleted, StateFailed},
}

func allEvidence() []EvidenceKind {
	return []EvidenceKind{
		EvidenceAuthorizationDecision,
		EvidenceOrderIdentity,
		EvidenceBuyerApproval,
		EvidenceProviderCompletion,
		EvidenceProviderFailure,
	}
}

func isWanted(from, to TransactionState) bool {
	for _, next := range wantEdges[from] {
		if next == to {
			return true
		}
	}
	return false
}

func TestStateVocabularyIsComplete(t *testing.T) {
	got := States()
	if len(got) != 12 {
		t.Fatalf("States() returned %d states, want 12", len(got))
	}
	seen := map[TransactionState]bool{}
	for _, s := range got {
		if !s.Valid() {
			t.Errorf("state %q is listed but not valid", s)
		}
		if seen[s] {
			t.Errorf("state %q listed twice", s)
		}
		seen[s] = true
	}
	if TransactionState("TELEPORTED").Valid() {
		t.Error("invented state must not be valid")
	}
}

func TestEveryLegalEdgeIsAllowed(t *testing.T) {
	for from, tos := range wantEdges {
		for _, to := range tos {
			out := EvaluateTransition(TransitionRequest{From: from, To: to, Evidence: allEvidence()})
			if !out.Allowed || out.Code != TransitionOK {
				t.Errorf("%s -> %s: got allowed=%v code=%s, want ok", from, to, out.Allowed, out.Code)
			}
		}
	}
}

func TestEveryIllegalEdgeIsDenied(t *testing.T) {
	for _, from := range States() {
		for _, to := range States() {
			if from == to {
				continue // idempotent case, covered separately
			}
			out := EvaluateTransition(TransitionRequest{From: from, To: to, Evidence: allEvidence()})
			wantAllowed := isWanted(from, to)
			if out.Allowed != wantAllowed {
				t.Errorf("%s -> %s: allowed=%v, want %v", from, to, out.Allowed, wantAllowed)
				continue
			}
			if wantAllowed {
				continue
			}
			wantCode := TransitionIllegal
			if from.Terminal() {
				wantCode = TransitionFromTerminal
			}
			if out.Code != wantCode {
				t.Errorf("%s -> %s: code=%s, want %s", from, to, out.Code, wantCode)
			}
		}
	}
}

func TestLegalNextMatchesMatrix(t *testing.T) {
	for _, from := range States() {
		got := LegalNext(from)
		want := wantEdges[from]
		if len(want) == 0 {
			want = nil
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("LegalNext(%s) = %v, want %v", from, got, want)
		}
	}
}

func TestRepeatedTransitionsAreIdempotent(t *testing.T) {
	for _, s := range States() {
		out := EvaluateTransition(TransitionRequest{From: s, To: s})
		if !out.Allowed || !out.Idempotent || out.Code != TransitionIdempotent {
			t.Errorf("%s -> %s: got allowed=%v idempotent=%v code=%s", s, s, out.Allowed, out.Idempotent, out.Code)
		}
	}
}

func TestTerminalStatesHaveNoOutgoingEdges(t *testing.T) {
	for _, from := range States() {
		if !from.Terminal() {
			if len(LegalNext(from)) == 0 {
				t.Errorf("non-terminal %s has no outgoing edges", from)
			}
			continue
		}
		if len(LegalNext(from)) != 0 {
			t.Errorf("terminal %s has outgoing edges %v", from, LegalNext(from))
		}
		for _, to := range States() {
			if to == from {
				continue
			}
			out := EvaluateTransition(TransitionRequest{From: from, To: to, Evidence: allEvidence()})
			if out.Allowed || out.Code != TransitionFromTerminal {
				t.Errorf("%s (terminal) -> %s: allowed=%v code=%s, want terminal_state", from, to, out.Allowed, out.Code)
			}
		}
	}
}

func TestDeniedNeverBecomesAuthorized(t *testing.T) {
	out := EvaluateTransition(TransitionRequest{From: StateDenied, To: StateAuthorized, Evidence: allEvidence()})
	if out.Allowed {
		t.Fatal("DENIED -> AUTHORIZED must be impossible")
	}
}

func TestProposedCannotJumpAhead(t *testing.T) {
	for _, to := range []TransactionState{
		StateAuthorized, StateOrderCreating, StateAwaitingBuyerApproval,
		StateCapturePending, StateCompleted, StateFailed,
	} {
		out := EvaluateTransition(TransitionRequest{From: StateProposed, To: to, Evidence: allEvidence()})
		if out.Allowed {
			t.Errorf("PROPOSED -> %s must be impossible", to)
		}
	}
}

func TestEvidenceGates(t *testing.T) {
	tests := []struct {
		name    string
		from    TransactionState
		to      TransactionState
		ev      []EvidenceKind
		wantOK  bool
		missing []EvidenceKind
	}{
		{
			name: "authorization without decision",
			from: StateVerifying, to: StateAuthorized,
			wantOK: false, missing: []EvidenceKind{EvidenceAuthorizationDecision},
		},
		{
			name: "authorization with decision",
			from: StateVerifying, to: StateAuthorized,
			ev:     []EvidenceKind{EvidenceAuthorizationDecision},
			wantOK: true,
		},
		{
			name: "authorization with wrong evidence",
			from: StateVerifying, to: StateAuthorized,
			ev:     []EvidenceKind{EvidenceOrderIdentity, EvidenceBuyerApproval},
			wantOK: false, missing: []EvidenceKind{EvidenceAuthorizationDecision},
		},
		{
			name: "order creation without bound order identity",
			from: StateOrderCreating, to: StateAwaitingBuyerApproval,
			wantOK: false, missing: []EvidenceKind{EvidenceOrderIdentity},
		},
		{
			name: "order creation with bound order identity",
			from: StateOrderCreating, to: StateAwaitingBuyerApproval,
			ev:     []EvidenceKind{EvidenceOrderIdentity},
			wantOK: true,
		},
		{
			name: "capture without buyer approval",
			from: StateAwaitingBuyerApproval, to: StateCapturePending,
			wantOK: false, missing: []EvidenceKind{EvidenceBuyerApproval},
		},
		{
			name: "capture with buyer approval",
			from: StateAwaitingBuyerApproval, to: StateCapturePending,
			ev:     []EvidenceKind{EvidenceBuyerApproval},
			wantOK: true,
		},
		{
			name: "completion without provider confirmation",
			from: StateCapturePending, to: StateCompleted,
			wantOK: false, missing: []EvidenceKind{EvidenceProviderCompletion},
		},
		{
			name: "completion with provider confirmation",
			from: StateCapturePending, to: StateCompleted,
			ev:     []EvidenceKind{EvidenceProviderCompletion},
			wantOK: true,
		},
		{
			name: "failure from authorized without provider evidence",
			from: StateAuthorized, to: StateFailed,
			wantOK: false, missing: []EvidenceKind{EvidenceProviderFailure},
		},
		{
			name: "failure from authorized with provider evidence",
			from: StateAuthorized, to: StateFailed,
			ev:     []EvidenceKind{EvidenceProviderFailure},
			wantOK: true,
		},
		{
			name: "failure from reconciliation without provider evidence",
			from: StateReconciliationRequired, to: StateFailed,
			wantOK: false, missing: []EvidenceKind{EvidenceProviderFailure},
		},
		{
			name: "failure from reconciliation with provider evidence",
			from: StateReconciliationRequired, to: StateFailed,
			ev:     []EvidenceKind{EvidenceProviderFailure},
			wantOK: true,
		},
		{
			name: "reconciliation completion without provider confirmation",
			from: StateReconciliationRequired, to: StateCompleted,
			wantOK: false, missing: []EvidenceKind{EvidenceProviderCompletion},
		},
		{
			name: "reconciliation completion with provider confirmation",
			from: StateReconciliationRequired, to: StateCompleted,
			ev:     []EvidenceKind{EvidenceProviderCompletion},
			wantOK: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out := EvaluateTransition(TransitionRequest{From: tc.from, To: tc.to, Evidence: tc.ev})
			if out.Allowed != tc.wantOK {
				t.Fatalf("allowed=%v code=%s, want allowed=%v", out.Allowed, out.Code, tc.wantOK)
			}
			if tc.wantOK {
				if out.Code != TransitionOK {
					t.Fatalf("code=%s, want ok", out.Code)
				}
				return
			}
			if out.Code != TransitionMissingEvidence {
				t.Fatalf("code=%s, want missing_evidence", out.Code)
			}
			if !reflect.DeepEqual(out.Missing, tc.missing) {
				t.Fatalf("missing=%v, want %v", out.Missing, tc.missing)
			}
		})
	}
}

func TestUnknownPaymentOutcomeIsNeverDefinitiveFailure(t *testing.T) {
	// An unresolved capture outcome must not be recorded as FAILED: there is
	// no such edge at all, even with every piece of evidence supplied.
	out := EvaluateTransition(TransitionRequest{From: StateCapturePending, To: StateFailed, Evidence: allEvidence()})
	if out.Allowed {
		t.Fatal("CAPTURE_PENDING -> FAILED must be impossible")
	}
	if out.Code != TransitionIllegal {
		t.Fatalf("code=%s, want illegal_transition", out.Code)
	}

	// The unresolved-outcome path is RECONCILIATION_REQUIRED...
	out = EvaluateTransition(TransitionRequest{From: StateCapturePending, To: StateReconciliationRequired})
	if !out.Allowed {
		t.Fatalf("CAPTURE_PENDING -> RECONCILIATION_REQUIRED must be allowed: %s", out.Code)
	}
	out = EvaluateTransition(TransitionRequest{From: StateOrderCreating, To: StateReconciliationRequired})
	if !out.Allowed {
		t.Fatalf("ORDER_CREATING -> RECONCILIATION_REQUIRED must be allowed: %s", out.Code)
	}

	// ...and only authoritative provider evidence turns it into FAILED.
	out = EvaluateTransition(TransitionRequest{From: StateReconciliationRequired, To: StateFailed})
	if out.Allowed || out.Code != TransitionMissingEvidence {
		t.Fatalf("failure from reconciliation must require provider evidence, got allowed=%v code=%s", out.Allowed, out.Code)
	}
}

// TestEveryCompletionEdgeRequiresProviderEvidence audits the transition table
// itself: every edge targeting COMPLETED must demand authoritative provider
// confirmation, and only the two intended sources may ever reach it.
func TestEveryCompletionEdgeRequiresProviderEvidence(t *testing.T) {
	sources := map[TransactionState]bool{}
	for from, tos := range legalTransitions {
		for _, to := range tos {
			if to != StateCompleted {
				continue
			}
			sources[from] = true
			gated := false
			for _, k := range requiredEvidence[transitionKey{from, to}] {
				if k == EvidenceProviderCompletion {
					gated = true
				}
			}
			if !gated {
				t.Errorf("edge %s -> COMPLETED does not require provider completion", from)
			}
		}
	}
	want := map[TransactionState]bool{StateCapturePending: true, StateReconciliationRequired: true}
	if !reflect.DeepEqual(sources, want) {
		t.Fatalf("edges into COMPLETED come from %v, want exactly %v", sources, want)
	}
}

// TestReconciliationFromOrderCreationCannotCompleteSilently walks the exact
// scenario from the review: an order that lands in reconciliation straight
// from ORDER_CREATING — buyer approval never happened — must still be unable
// to record COMPLETED without provider confirmation, and wrong evidence must
// not substitute for the right kind.
func TestReconciliationFromOrderCreationCannotCompleteSilently(t *testing.T) {
	setup := []TransitionRequest{
		{From: StateProposed, To: StateVerifying},
		{From: StateVerifying, To: StateAuthorized, Evidence: []EvidenceKind{EvidenceAuthorizationDecision}},
		{From: StateAuthorized, To: StateOrderCreating},
		{From: StateOrderCreating, To: StateReconciliationRequired},
	}
	for _, step := range setup {
		if out := EvaluateTransition(step); !out.Allowed {
			t.Fatalf("setup step %s -> %s rejected: %s", step.From, step.To, out.Code)
		}
	}

	out := EvaluateTransition(TransitionRequest{From: StateReconciliationRequired, To: StateCompleted})
	if out.Allowed || out.Code != TransitionMissingEvidence {
		t.Fatalf("completion without evidence: allowed=%v code=%s", out.Allowed, out.Code)
	}
	if !reflect.DeepEqual(out.Missing, []EvidenceKind{EvidenceProviderCompletion}) {
		t.Fatalf("missing=%v, want [PROVIDER_COMPLETION]", out.Missing)
	}

	out = EvaluateTransition(TransitionRequest{
		From: StateReconciliationRequired, To: StateCompleted,
		Evidence: []EvidenceKind{EvidenceProviderFailure},
	})
	if out.Allowed {
		t.Fatal("provider failure must not substitute for provider completion")
	}

	out = EvaluateTransition(TransitionRequest{
		From: StateReconciliationRequired, To: StateCompleted,
		Evidence: []EvidenceKind{EvidenceProviderCompletion},
	})
	if !out.Allowed {
		t.Fatalf("provider-confirmed completion refused: %s", out.Code)
	}
}

func TestUnknownStatesFailClosed(t *testing.T) {
	bogus := TransactionState("BOGUS")
	tests := []TransitionRequest{
		{From: bogus, To: StateVerifying},
		{From: StateProposed, To: bogus},
		{From: bogus, To: bogus},
		{From: "", To: StateVerifying},
		{From: StateProposed, To: ""},
	}
	for _, req := range tests {
		out := EvaluateTransition(req)
		if out.Allowed || out.Code != TransitionUnknownState {
			t.Errorf("%q -> %q: allowed=%v code=%s, want unknown_state", req.From, req.To, out.Allowed, out.Code)
		}
	}
}

func TestPaymentEligibilityOnlyFromAuthorized(t *testing.T) {
	for _, s := range States() {
		want := s == StateAuthorized
		if s.PaymentEligible() != want {
			t.Errorf("%s.PaymentEligible() = %v, want %v", s, s.PaymentEligible(), want)
		}
	}
}

func TestEvidenceKindValidity(t *testing.T) {
	for _, k := range allEvidence() {
		if !k.Valid() {
			t.Errorf("declared evidence kind %q is not valid", k)
		}
	}
	if EvidenceKind("VIBES").Valid() {
		t.Error("invented evidence kind must not be valid")
	}
}
