package domain

// TransactionState mirrors the transactions.state vocabulary locked by the
// database constraint transactions_state_valid (0001_init.sql). Adding a
// state requires a new migration AND a matching addition here — the
// integration test TestDomainStatesMatchDatabaseConstraint keeps the two in
// lockstep.
type TransactionState string

const (
	StateProposed               TransactionState = "PROPOSED"
	StateVerifying              TransactionState = "VERIFYING"
	StateDenied                 TransactionState = "DENIED"
	StateReviewRequired         TransactionState = "REVIEW_REQUIRED"
	StateAuthorized             TransactionState = "AUTHORIZED"
	StateOrderCreating          TransactionState = "ORDER_CREATING"
	StateAwaitingBuyerApproval  TransactionState = "AWAITING_BUYER_APPROVAL"
	StateCancelled              TransactionState = "CANCELLED"
	StateCapturePending         TransactionState = "CAPTURE_PENDING"
	StateCompleted              TransactionState = "COMPLETED"
	StateReconciliationRequired TransactionState = "RECONCILIATION_REQUIRED"
	StateFailed                 TransactionState = "FAILED"
)

// states lists every valid state in schema order (the order of the database
// constraint), the single source for iteration.
var states = []TransactionState{
	StateProposed,
	StateVerifying,
	StateDenied,
	StateReviewRequired,
	StateAuthorized,
	StateOrderCreating,
	StateAwaitingBuyerApproval,
	StateCancelled,
	StateCapturePending,
	StateCompleted,
	StateReconciliationRequired,
	StateFailed,
}

// States returns a copy of every valid transaction state.
func States() []TransactionState {
	out := make([]TransactionState, len(states))
	copy(out, states)
	return out
}

// Valid reports whether the state is part of the locked vocabulary.
func (s TransactionState) Valid() bool {
	for _, known := range states {
		if s == known {
			return true
		}
	}
	return false
}

// Terminal reports states with no outgoing transitions:
//
//   - DENIED, CANCELLED, COMPLETED and FAILED are final outcomes.
//   - REVIEW_REQUIRED is a non-executable hold: V1 resolves review outside
//     this transaction (a contract edit creates a new version and a fresh
//     proposal), so this machine gives it no automatic edge.
//
// Note an unresolved outcome is NOT terminal: CAPTURE_PENDING resolves to
// RECONCILIATION_REQUIRED, which itself only becomes FAILED against
// authoritative provider-failure evidence.
func (s TransactionState) Terminal() bool {
	switch s {
	case StateDenied, StateReviewRequired, StateCancelled, StateCompleted, StateFailed:
		return true
	}
	return false
}

// PaymentEligible reports whether a provider operation may be initiated from
// this state. Only AUTHORIZED may create a PayPal order — which in turn is
// only reachable through the gated VERIFYING -> AUTHORIZED edge.
func (s TransactionState) PaymentEligible() bool { return s == StateAuthorized }
