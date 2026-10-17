package domain

// EvidenceKind names a fact that must be established before certain
// transitions — gates that must not be bypassed.
type EvidenceKind string

const (
	// EvidenceAuthorizationDecision: the aggregated decision is ALLOW — the
	// deterministic checks (and, from the semantic phase onward, the separate
	// semantic verifier gate) did not block. The verifier plugs in here; it
	// never bypasses the deterministic checks.
	EvidenceAuthorizationDecision EvidenceKind = "AUTHORIZATION_DECISION"
	// EvidenceOrderIdentity: a provider order exists and its identifier is
	// durably bound to this transaction.
	EvidenceOrderIdentity EvidenceKind = "PROVIDER_ORDER_IDENTITY"
	// EvidenceBuyerApproval: the buyer approved the bound order — provider-
	// reported APPROVED, which is not COMPLETED.
	EvidenceBuyerApproval EvidenceKind = "BUYER_APPROVAL"
	// EvidenceProviderCompletion: the provider itself confirms the capture.
	EvidenceProviderCompletion EvidenceKind = "PROVIDER_COMPLETION"
	// EvidenceProviderFailure: authoritative provider evidence of terminal
	// failure. A timeout or any other unresolved outcome never qualifies.
	EvidenceProviderFailure EvidenceKind = "PROVIDER_FAILURE"
)

// Valid reports whether the evidence kind is known.
func (k EvidenceKind) Valid() bool {
	switch k {
	case EvidenceAuthorizationDecision, EvidenceOrderIdentity, EvidenceBuyerApproval,
		EvidenceProviderCompletion, EvidenceProviderFailure:
		return true
	}
	return false
}

type transitionKey struct{ from, to TransactionState }

// legalTransitions is the locked PRD §7.4 state machine. Every edge not
// listed here is forbidden.
var legalTransitions = map[TransactionState][]TransactionState{
	StateProposed:               {StateVerifying},
	StateVerifying:              {StateDenied, StateReviewRequired, StateAuthorized},
	StateAuthorized:             {StateOrderCreating, StateFailed},
	StateOrderCreating:          {StateAwaitingBuyerApproval, StateReconciliationRequired},
	StateAwaitingBuyerApproval:  {StateCancelled, StateCapturePending},
	StateCapturePending:         {StateCompleted, StateReconciliationRequired},
	StateReconciliationRequired: {StateCompleted, StateFailed},
}

// requiredEvidence gates the transitions that must not be driven by wishful
// thinking: authorization needs a decision, capture needs buyer approval,
// failure needs authoritative provider evidence, and NO path — direct or
// through reconciliation — may record a completed payment without the
// provider itself confirming it.
var requiredEvidence = map[transitionKey][]EvidenceKind{
	{StateVerifying, StateAuthorized}:                 {EvidenceAuthorizationDecision},
	{StateOrderCreating, StateAwaitingBuyerApproval}:  {EvidenceOrderIdentity},
	{StateAwaitingBuyerApproval, StateCapturePending}: {EvidenceBuyerApproval},
	{StateAuthorized, StateFailed}:                    {EvidenceProviderFailure},
	{StateCapturePending, StateCompleted}:             {EvidenceProviderCompletion},
	{StateReconciliationRequired, StateCompleted}:     {EvidenceProviderCompletion},
	{StateReconciliationRequired, StateFailed}:        {EvidenceProviderFailure},
}

// Machine-readable outcome codes for transition evaluation.
const (
	// TransitionOK: the move is legal and every required gate is satisfied.
	TransitionOK = "ok"
	// TransitionIdempotent: the request repeats the current state; a no-op
	// success (retries stay safe), no evidence required.
	TransitionIdempotent = "idempotent"
	// TransitionUnknownState: at least one state is outside the vocabulary.
	TransitionUnknownState = "unknown_state"
	// TransitionIllegal: the edge does not exist.
	TransitionIllegal = "illegal_transition"
	// TransitionFromTerminal: the move leaves a terminal state.
	TransitionFromTerminal = "terminal_state"
	// TransitionMissingEvidence: the edge exists but its gates are unmet.
	TransitionMissingEvidence = "missing_evidence"
)

// TransitionRequest is a pure query: may the transaction move from one state
// to another, given the evidence at hand? It performs no mutation.
type TransitionRequest struct {
	From TransactionState
	To   TransactionState
	// Evidence lists the facts established so far; nil means none.
	Evidence []EvidenceKind
}

// TransitionOutcome is the decision. Callers persist it; this package never
// touches a database or provider.
type TransitionOutcome struct {
	Allowed    bool
	Idempotent bool
	Code       string
	// Missing lists required evidence that was absent (Code ==
	// TransitionMissingEvidence), in the order the edge requires it.
	Missing []EvidenceKind
}

// EvaluateTransition is the single authoritative decision point for state
// movement. It is total: known states and edges produce ok/idempotent, and
// everything else fails closed with a specific code.
func EvaluateTransition(req TransitionRequest) TransitionOutcome {
	if !req.From.Valid() || !req.To.Valid() {
		return TransitionOutcome{Code: TransitionUnknownState}
	}
	if req.From == req.To {
		// A repeated transition request is an idempotent no-op success for
		// any known state — including terminal ones, so retried
		// confirmations of COMPLETED/FAILED stay safe.
		return TransitionOutcome{Allowed: true, Idempotent: true, Code: TransitionIdempotent}
	}
	legal := false
	for _, next := range legalTransitions[req.From] {
		if next == req.To {
			legal = true
			break
		}
	}
	if !legal {
		if req.From.Terminal() {
			return TransitionOutcome{Code: TransitionFromTerminal}
		}
		return TransitionOutcome{Code: TransitionIllegal}
	}

	required := requiredEvidence[transitionKey{from: req.From, to: req.To}]
	if len(required) == 0 {
		return TransitionOutcome{Allowed: true, Code: TransitionOK}
	}
	present := make(map[EvidenceKind]bool, len(req.Evidence))
	for _, e := range req.Evidence {
		present[e] = true
	}
	var missing []EvidenceKind
	for _, need := range required {
		if !present[need] {
			missing = append(missing, need)
		}
	}
	if len(missing) > 0 {
		return TransitionOutcome{Code: TransitionMissingEvidence, Missing: missing}
	}
	return TransitionOutcome{Allowed: true, Code: TransitionOK}
}

// LegalNext returns the states reachable from `from` in one legal step (nil
// for unknown or terminal states). Read-only copy, safe against caller
// mutation of the internal table.
func LegalNext(from TransactionState) []TransactionState {
	next := legalTransitions[from]
	if len(next) == 0 {
		return nil
	}
	out := make([]TransactionState, len(next))
	copy(out, next)
	return out
}
