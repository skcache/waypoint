// Package bench is the M5 held-out repair benchmark. It owns the case corpus,
// the controlled mutations, the deterministic B0 baseline, and the B1/B2
// evaluation runs. The corpus is trusted evaluator data: reference fixes,
// expected categories and mutation specs never enter a model prompt.
package bench

import "fmt"

// Mutation is one exact-text edit applied to a fresh worktree at BaseRevision.
// The applier refuses unless old occurs exactly once.
type Mutation struct {
	Path string
	Old  string
	New  string
}

// Case is one held-out defect. Only the fields in ModelFacing() may reach the
// model; everything else is evaluator/human-only.
type Case struct {
	ID           string     `json:"id"`
	Family       string     `json:"family"`
	Title        string     `json:"title"`
	BaseRevision string     `json:"base_revision,omitempty"`
	Journey      string     `json:"-"`
	Mutations    []Mutation `json:"-"`
	// GateFault selects the writable surface ("" = service.go, "F3" adds
	// webhook.go). It is not shown to the model.
	GateFault string `json:"-"`
	// Mode is the acceptance mode: verified (a safe verified capture) or
	// refused (no unverified capture claimed).
	Mode string `json:"mode"`
	// ExpectedCategory is the human-only diagnosis category used to score
	// diagnosis accuracy.
	ExpectedCategory string `json:"-"`
	// ReferenceFix is the inverse mutation, kept for human review only.
	ReferenceFix []Mutation `json:"-"`
	// Exposed marks a case whose mechanism already appeared in model-facing M3
	// or M4 evidence. Exposed cases are reported separately.
	Exposed bool `json:"exposed"`
	// Facts are the evidence facts allowed into the prompt (whitelist).
	Facts []string `json:"-"`
}

// Cases is the frozen M5 corpus (v1). The first five are held-out; H7 is an
// exposed reference case (the M4 duplicate-webhook mechanism) and is excluded
// from the held-out totals.
func Cases() []Case {
	return []Case{
		{
			ID: "H1-identity-rewrite", Family: "idempotency-identity",
			Title:   "A retry rewrites the durable capture request id",
			Journey: "H1-identity-rewrite", Mode: "verified",
			ExpectedCategory: "identity",
			Facts:            []string{"final_status", "capture_status", "capture_error", "provider_capture_effects"},
			Mutations: []Mutation{{
				Path: "apps/api/internal/merchant/service.go",
				Old: "		claimed, err := s.store.BeginCaptureRetry(ctx, order.ID, active.ID, s.instanceID, s.leaseTTL)\n" +
					"		if err != nil {\n" +
					"			return Operation{}, err\n" +
					"		}\n" +
					"		return claimed, nil",
				New: "		claimed, err := s.store.BeginCaptureRetry(ctx, order.ID, active.ID, s.instanceID, s.leaseTTL)\n" +
					"		if err != nil {\n" +
					"			return Operation{}, err\n" +
					"		}\n" +
					"		claimed.ProviderRequestID += \"-retry\"\n" +
					"		return claimed, nil",
			}},
			ReferenceFix: []Mutation{{
				Path: "apps/api/internal/merchant/service.go",
				Old:  "		claimed.ProviderRequestID += \"-retry\"\n		return claimed, nil",
				New:  "		return claimed, nil",
			}},
		},
		{
			ID: "H2-webhook-conflict", Family: "webhook-fulfillment",
			Title:   "A conflicting duplicate event id is treated as a new capture event",
			Journey: "H2-webhook-conflict", Mode: "verified", GateFault: "F3",
			ExpectedCategory: "fulfillment",
			Facts:            []string{"final_status", "fulfillment_effects", "conflict_rejected", "conflict_error"},
			Mutations: []Mutation{
				{
					Path: "apps/api/internal/merchant/webhook.go",
					Old:  "VALUES ($1::uuid, 'FULFILL', $2, $3)",
					New:  "VALUES ($1::uuid, $2, $2, $3)",
				},
				{
					Path: "apps/api/internal/merchant/webhook.go",
					Old:  "if ev.CaptureID == \"\" || ev.CaptureID != verifiedCapture {",
					New:  "if ev.CaptureID == \"\" {\n\t\t\t_ = verifiedCapture",
				},
			},
			ReferenceFix: []Mutation{
				{Path: "apps/api/internal/merchant/webhook.go", Old: "VALUES ($1::uuid, $2, $2, $3)", New: "VALUES ($1::uuid, 'FULFILL', $2, $3)"},
				{Path: "apps/api/internal/merchant/webhook.go", Old: "if ev.CaptureID == \"\" {", New: "if ev.CaptureID == \"\" || ev.CaptureID != verifiedCapture {"},
			},
		},
		{
			ID: "H3-currency-mismatch", Family: "monetary-evidence",
			Title:   "Capture verification ignores the provider currency",
			Journey: "H3-currency-mismatch", Mode: "refused",
			ExpectedCategory: "monetary",
			Facts:            []string{"final_status", "capture_status", "capture_error"},
			Mutations: []Mutation{{
				Path: "apps/api/internal/merchant/service.go",
				Old:  "c.Amount.CurrencyCode == order.Currency && c.Amount.Value == expected",
				New:  "c.Amount.Value == expected",
			}},
			ReferenceFix: []Mutation{{
				Path: "apps/api/internal/merchant/service.go",
				Old:  "if c.Status == paypal.CaptureStatusCompleted && c.ID != \"\" &&\n\t\t\tc.Amount.Value == expected {",
				New:  "if c.Status == paypal.CaptureStatusCompleted && c.ID != \"\" &&\n\t\t\tc.Amount.CurrencyCode == order.Currency && c.Amount.Value == expected {",
			}},
		},
		{
			ID: "H4-pending-capture", Family: "response-interpretation",
			Title:   "Any capture status is treated as completed",
			Journey: "H4-pending-capture", Mode: "refused",
			ExpectedCategory: "status",
			Facts:            []string{"final_status", "capture_status", "capture_error"},
			Mutations: []Mutation{{
				Path: "apps/api/internal/merchant/service.go",
				Old:  "if c.Status == paypal.CaptureStatusCompleted && c.ID != \"\" &&",
				New:  "if c.Status != \"\" && c.ID != \"\" &&",
			}},
			ReferenceFix: []Mutation{{
				Path: "apps/api/internal/merchant/service.go",
				Old:  "if c.Status != \"\" && c.ID != \"\" &&",
				New:  "if c.Status == paypal.CaptureStatusCompleted && c.ID != \"\" &&",
			}},
		},
		{
			ID: "H5-early-fulfillment", Family: "webhook-fulfillment",
			Title:   "An early capture event fulfills before local verification",
			Journey: "H5-early-fulfillment", Mode: "verified", GateFault: "F3",
			ExpectedCategory: "fulfillment",
			Facts:            []string{"final_status", "fulfillment_effects_early", "fulfillment_effects"},
			Mutations: []Mutation{
				{
					Path: "apps/api/internal/merchant/webhook.go",
					Old:  "\t\tif status != StatusCapturedVerified {\n\t\t\t// Early event: durable receipt only. A later redelivery, after the\n\t\t\t// merchant has verified the capture, can still fulfill.\n\t\t\tbreak\n\t\t}",
					New:  "\t\t_ = status",
				},
				{
					Path: "apps/api/internal/merchant/webhook.go",
					Old:  "if ev.CaptureID == \"\" || ev.CaptureID != verifiedCapture {",
					New:  "if ev.CaptureID == \"\" {\n\t\t\t_ = verifiedCapture",
				},
			},
			ReferenceFix: []Mutation{
				{Path: "apps/api/internal/merchant/webhook.go", Old: "\t\t_ = status", New: "\t\tif status != StatusCapturedVerified {\n\t\t\tbreak\n\t\t}"},
				{Path: "apps/api/internal/merchant/webhook.go", Old: "if ev.CaptureID == \"\" {\n\t\t\t_ = verifiedCapture", New: "if ev.CaptureID == \"\" || ev.CaptureID != verifiedCapture {"},
			},
		},
		{
			ID: "H6-ambiguous-reconcile", Family: "ambiguity-reconciliation",
			Title:   "Reconciliation turns an ambiguous capture into a definite rejection",
			Journey: "H6-ambiguous-reconcile", Mode: "refused",
			ExpectedCategory: "ambiguity",
			Facts:            []string{"final_status", "capture_status", "reconcile_error", "capture_error"},
			Mutations: []Mutation{{
				Path: "apps/api/internal/merchant/service.go",
				Old: "	} else {\n" +
					"		if err := s.store.AppendEvent(ctx, orderID, \"PAYPAL\", \"RECONCILIATION_INCONCLUSIVE\", detail, s.evidenceTier()); err != nil {\n" +
					"			return Order{}, err\n" +
					"		}\n" +
					"		if err := s.store.TouchLastChecked(ctx, active.ID); err != nil {\n" +
					"			return Order{}, err\n" +
					"		}\n" +
					"	}",
				New: "	} else {\n" +
					"		if _, err := s.store.MarkCaptureRejected(ctx, orderID, active.ID, active.Fence, detail, s.evidenceTier()); err != nil {\n" +
					"			return Order{}, err\n" +
					"		}\n" +
					"	}",
			}},
			ReferenceFix: []Mutation{{
				Path: "apps/api/internal/merchant/service.go",
				Old:  "	} else {\n		if _, err := s.store.MarkCaptureRejected(ctx, orderID, active.ID, active.Fence, detail, s.evidenceTier()); err != nil {\n			return Order{}, err\n		}\n	}",
				New:  "	} else {\n		if err := s.store.AppendEvent(ctx, orderID, \"PAYPAL\", \"RECONCILIATION_INCONCLUSIVE\", detail, s.evidenceTier()); err != nil {\n			return Order{}, err\n		}\n		if err := s.store.TouchLastChecked(ctx, active.ID); err != nil {\n			return Order{}, err\n		}\n	}",
			}},
		},
		{
			ID: "H7-webhook-duplicate-exposed", Family: "webhook-fulfillment",
			Title:   "Duplicate delivery creates a second fulfillment (M4 mechanism, exposed)",
			Journey: "H7-webhook-duplicate-exposed", Mode: "verified", GateFault: "F3",
			ExpectedCategory: "fulfillment", Exposed: true,
			Facts: []string{"final_status", "fulfillment_effects", "evt_cap_1_deliveries"},
			Mutations: []Mutation{{
				Path: "apps/api/internal/merchant/webhook.go",
				Old:  "VALUES ($1::uuid, 'FULFILL', $2, $3)",
				New:  "VALUES ($1::uuid, $2, $2, $3)",
			}},
			ReferenceFix: []Mutation{{
				Path: "apps/api/internal/merchant/webhook.go",
				Old:  "VALUES ($1::uuid, $2, $2, $3)",
				New:  "VALUES ($1::uuid, 'FULFILL', $2, $3)",
			}},
		},
	}
}

// CaseByID returns one case.
func CaseByID(id string) (Case, error) {
	for _, c := range Cases() {
		if c.ID == id {
			return c, nil
		}
	}
	return Case{}, fmt.Errorf("bench: unknown case %q", id)
}

// HeldOut returns the cases included in the held-out totals.
func HeldOut() []Case {
	var out []Case
	for _, c := range Cases() {
		if !c.Exposed {
			out = append(out, c)
		}
	}
	return out
}
