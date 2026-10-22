package merchant

// Fixture-only constructors. This file is part of the trusted harness surface:
// it is deliberately NOT writable by a candidate patch, so the defect under
// test cannot be repaired by deleting the wiring that selects it. A candidate
// has to change the behavior in the writable implementation file
// (ambiguity.go for F2, webhook.go for F3).

// NewLegacyAmbiguityService wires the historical capture-ambiguity policy.
// The F2 fixture uses it to reproduce the lost-response defect.
func NewLegacyAmbiguityService(store *Store, api OrdersAPI) *Service {
	s := NewService(store, api, FaultNone, "")
	s.ambiguity = legacyAmbiguity{}
	return s
}

// NewLegacyWebhookService wires the historical webhook handler. The F3 fixture
// uses it to reproduce duplicate/reordered delivery defects.
func NewLegacyWebhookService(store *Store, api OrdersAPI) *Service {
	s := NewService(store, api, FaultNone, "")
	s.applyWebhook = s.legacyApplyWebhookEvent
	return s
}
