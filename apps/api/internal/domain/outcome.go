package domain

import "fmt"

// SemanticClass is the semantic verifier's classification (PRD §7.3). The
// verifier is a separate, model-backed gate implemented in a later phase;
// this type is the boundary contract it must satisfy. It is NOT a substitute
// for the deterministic checks in this package.
type SemanticClass string

const (
	SemanticSupported    SemanticClass = "SUPPORTED"
	SemanticContradicted SemanticClass = "CONTRADICTED"
	SemanticUnknown      SemanticClass = "UNKNOWN"
)

// Valid reports whether the classification is part of the vocabulary.
func (c SemanticClass) Valid() bool {
	return c == SemanticSupported || c == SemanticContradicted || c == SemanticUnknown
}

// PolicyResult is the deterministic hard-policy outcome (PRD §7.3).
type PolicyResult string

const (
	PolicyAllow         PolicyResult = "ALLOW"
	PolicyDeny          PolicyResult = "DENY"
	PolicyIndeterminate PolicyResult = "INDETERMINATE"
)

// Valid reports whether the policy result is part of the vocabulary.
func (p PolicyResult) Valid() bool {
	return p == PolicyAllow || p == PolicyDeny || p == PolicyIndeterminate
}

// Decision is the aggregated authorization outcome.
type Decision string

const (
	DecisionAllow  Decision = "ALLOW"
	DecisionDeny   Decision = "DENY"
	DecisionReview Decision = "REVIEW"
)

// Valid reports whether the decision is part of the vocabulary.
func (d Decision) Valid() bool {
	return d == DecisionAllow || d == DecisionDeny || d == DecisionReview
}

// AggregateDecision combines the deterministic policy result with the
// semantic classification exactly per PRD §7.3:
//
//   - a hard DENY always denies — even when the model says SUPPORTED, and
//     even when the semantic input is invalid (deny dominates);
//   - INDETERMINATE policy reviews; it never auto-pays;
//   - ALLOW requires both policy ALLOW and semantic SUPPORTED;
//   - a contradicting model verdict denies by default.
//
// Anything invalid or unhandled fails closed to REVIEW.
func AggregateDecision(policy PolicyResult, semantic SemanticClass) Decision {
	if !policy.Valid() {
		return DecisionReview
	}
	if policy == PolicyDeny {
		// Deny dominates every model outcome, valid or not.
		return DecisionDeny
	}
	if policy == PolicyIndeterminate {
		return DecisionReview
	}
	// policy == PolicyAllow
	if !semantic.Valid() {
		return DecisionReview
	}
	switch semantic {
	case SemanticSupported:
		return DecisionAllow
	case SemanticContradicted:
		return DecisionDeny
	default:
		return DecisionReview
	}
}

// State maps the aggregated decision to the transaction state it permits
// (PRD §7.3/§7.4): ALLOW -> AUTHORIZED, DENY -> DENIED, REVIEW ->
// REVIEW_REQUIRED. An unknown decision is an error, not a default.
func (d Decision) State() (TransactionState, error) {
	switch d {
	case DecisionAllow:
		return StateAuthorized, nil
	case DecisionDeny:
		return StateDenied, nil
	case DecisionReview:
		return StateReviewRequired, nil
	}
	return "", fmt.Errorf("unknown decision %q", string(d))
}
