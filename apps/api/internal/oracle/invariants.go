package oracle

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// InvariantResult is one invariant's outcome with human-readable reasoning and
// machine-checkable evidence pointers.
type InvariantResult struct {
	ID             string         `json:"id"`
	Title          string         `json:"title"`
	Status         Status         `json:"status"`
	Summary        string         `json:"summary"`
	Evidence       []string       `json:"evidence,omitempty"`
	Counterexample map[string]any `json:"counterexample,omitempty"`
}

// invariant is one deterministic check. Implementations must only read the
// Evidence value — no clock, no randomness, no network.
type invariant struct {
	ID    string
	Title string
	Check func(ev *Evidence) InvariantResult
}

var amountPattern = regexp.MustCompile(`^\d+\.\d{2}$`)

// amountString renders minor units the one canonical way.
func amountString(minor int64) string {
	sign := ""
	if minor < 0 {
		sign = "-"
		minor = -minor
	}
	return fmt.Sprintf("%s%d.%02d", sign, minor/100, minor%100)
}

// parseAmountExact parses a provider amount string to minor units; ok is false
// for anything that is not an exact two-decimal amount (float-formatted
// values like "12.340000" or exponentials are rejected).
func parseAmountExact(s string) (int64, bool) {
	if !amountPattern.MatchString(s) {
		return 0, false
	}
	parts := strings.SplitN(s, ".", 2)
	major, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, false
	}
	minor, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, false
	}
	return major*100 + minor, true
}

// Invariants is the frozen I1–I12 registry, in order.
var Invariants = []invariant{
	{ID: "I1", Title: "Integer minor units with explicit currency; no floating-point amounts", Check: checkI1},
	{ID: "I2", Title: "Captured amount/currency bound to the server-side order", Check: checkI2},
	{ID: "I3", Title: "Browser approval is not capture; COMPLETED requires capture evidence", Check: checkI3},
	{ID: "I4", Title: "Operation identity and request id durable before the outbound call", Check: checkI4},
	{ID: "I5", Title: "Ambiguity is never interpreted as definite failure", Check: checkI5},
	{ID: "I6", Title: "Completed-capture claims require matching provider evidence", Check: checkI6},
	{ID: "I7", Title: "One logical order, at most one fulfillment effect and one capture pathway", Check: checkI7},
	{ID: "I8", Title: "Verified capture cannot silently roll back to pending/failed", Check: checkI8},
	{ID: "I9", Title: "Local/provider divergence is explicit, never silently papered over", Check: checkI9},
	{ID: "I10", Title: "Refund success claims require a verified refund resource", Check: checkI10},
	{ID: "I11", Title: "Unsafe or inconclusive verification never auto-applies", Check: checkI11},
	{ID: "I12", Title: "Evidence provenance is explicit (R/F/S)", Check: checkI12},
}

// RunInvariants runs every invariant against the evidence. Combining the
// results into a verdict is Evaluate's job (verdict.go).
func RunInvariants(ev *Evidence) []InvariantResult {
	results := make([]InvariantResult, 0, len(Invariants))
	for _, inv := range Invariants {
		r := inv.Check(ev)
		r.ID = inv.ID
		r.Title = inv.Title
		results = append(results, r)
	}
	return results
}

// CombineVerdict applies the normative aggregation:
// any FAIL → UNSAFE; else any INCONCLUSIVE → INCONCLUSIVE; else any PASS →
// VERIFIED_FOR_TESTED_SCENARIOS; else (nothing applicable) → INCONCLUSIVE.
func CombineVerdict(results []InvariantResult) Verdict {
	hasPass, hasInconclusive := false, false
	for _, r := range results {
		switch r.Status {
		case StatusFail:
			return VerdictUnsafe
		case StatusInconclusive:
			hasInconclusive = true
		case StatusPass:
			hasPass = true
		}
	}
	if hasInconclusive {
		return VerdictInconclusive
	}
	if hasPass {
		return VerdictVerified
	}
	return VerdictInconclusive
}

func result(status Status, format string, args ...any) InvariantResult {
	return InvariantResult{Status: status, Summary: fmt.Sprintf(format, args...)}
}

// ---------------------------------------------------------------------------
// Checks
// ---------------------------------------------------------------------------

// I1: money is minor-unit integers with an explicit currency, and every
// provider-bound amount string is the exact canonical two-decimal rendering —
// no floating-point artifacts can reach (or come back from) the provider.
func checkI1(ev *Evidence) InvariantResult {
	if len(ev.Orders) == 0 {
		return result(StatusNotApplicable, "no orders in evidence")
	}
	for _, o := range ev.Orders {
		if o.TotalMinor <= 0 || len(o.Currency) != 3 {
			return InvariantResult{
				Status:  StatusFail,
				Summary: fmt.Sprintf("order %s carries non-canonical monetary fields", o.ID),
				Counterexample: map[string]any{
					"order_id": o.ID, "total_minor": o.TotalMinor, "currency": o.Currency,
				},
			}
		}
	}
	for _, obs := range ev.Provider {
		if obs.Method == "read" {
			continue
		}
		o := orderByProviderRef(ev, obs.OrderRef)
		if o == nil {
			continue
		}
		want := amountString(o.TotalMinor)
		if _, ok := parseAmountExact(obs.SentAmount); !ok {
			if obs.Applied {
				return InvariantResult{
					Status:  StatusFail,
					Summary: fmt.Sprintf("an APPLIED provider mutation carried a non-canonical amount string %q", obs.SentAmount),
					Counterexample: map[string]any{
						"obs_seq": obs.Seq, "sent_amount": obs.SentAmount, "want": want, "order_id": o.ID,
					},
				}
			}
			continue // malformed attempt that had no effect; honesty is checked by the other invariants
		}
		if got, _ := parseAmountExact(obs.SentAmount); got != o.TotalMinor {
			return InvariantResult{
				Status:         StatusFail,
				Summary:        fmt.Sprintf("provider-bound amount %q does not equal the order total %s", obs.SentAmount, want),
				Counterexample: map[string]any{"obs_seq": obs.Seq, "sent_amount": obs.SentAmount, "want": want, "order_id": o.ID},
			}
		}
		if obs.SentCurrency != "" && obs.SentCurrency != o.Currency {
			return InvariantResult{
				Status:         StatusFail,
				Summary:        fmt.Sprintf("provider-bound currency %q does not match order currency %q", obs.SentCurrency, o.Currency),
				Counterexample: map[string]any{"obs_seq": obs.Seq, "sent_currency": obs.SentCurrency, "want": o.Currency, "order_id": o.ID},
			}
		}
	}
	return result(StatusPass, "all local amounts are minor-unit integers with explicit currency; all provider-bound amounts are exact two-decimal renderings")
}

// I2: the amount/currency sent for each operation equals the server-side order
// total — no observation may carry any other amount for the same order.
func checkI2(ev *Evidence) InvariantResult {
	applicable := false
	for _, obs := range ev.Provider {
		if obs.Method == "read" {
			continue
		}
		applicable = true
		o := orderByProviderRef(ev, obs.OrderRef)
		if o == nil {
			continue
		}
		if got, ok := parseAmountExact(obs.SentAmount); ok && got != o.TotalMinor {
			return InvariantResult{
				Status:  StatusFail,
				Summary: fmt.Sprintf("operation used amount %s that the server never authorized (order total %s)", obs.SentAmount, amountString(o.TotalMinor)),
				Counterexample: map[string]any{
					"obs_seq": obs.Seq, "method": obs.Method, "sent_amount": obs.SentAmount,
					"server_total": amountString(o.TotalMinor), "order_id": o.ID,
				},
			}
		}
	}
	if !applicable {
		return result(StatusNotApplicable, "no outbound provider operations in evidence")
	}
	return result(StatusPass, "every outbound operation carried exactly the server-side order amount and currency")
}

// I3: approval evidence must belong to the SAME order being captured, and a
// capture claim ("verified") requires actual capture evidence — approval
// alone is not money. Each order's approval chronology is checked
// independently: order B's APPROVED read can never authorize order A's
// capture.
func checkI3(ev *Evidence) InvariantResult {
	checked := 0
	for _, o := range ev.Orders {
		firstCaptureSeq := 0
		for _, obs := range ev.Provider {
			if obs.Method != "capture" || obs.OrderRef == "" || obs.OrderRef != o.ProviderOrderID {
				continue
			}
			if firstCaptureSeq == 0 || obs.Seq < firstCaptureSeq {
				firstCaptureSeq = obs.Seq
			}
		}
		hasCaptureOp := len(ev.captureOps(o.ID)) > 0
		if firstCaptureSeq == 0 && !hasCaptureOp {
			continue // no capture activity for this order
		}
		checked++

		approvedSeen := false
		for _, obs := range ev.Provider {
			if obs.Method == "read" && obs.OrderRef == o.ProviderOrderID && obs.OrderStatus == "APPROVED" {
				if firstCaptureSeq == 0 || obs.Seq < firstCaptureSeq {
					approvedSeen = true
				}
			}
		}
		if !approvedSeen {
			// Journal fallback: an APPROVED observation event FOR THIS ORDER
			// must precede this order's first capture attempt.
			approvedEventSeen := false
			for _, e := range ev.Events {
				if e.OrderID != o.ID {
					continue
				}
				switch e.EventType {
				case "PROVIDER_ORDER_OBSERVED":
					var d struct {
						ProviderStatus string `json:"provider_status"`
					}
					if json.Unmarshal([]byte(e.Detail), &d) == nil && d.ProviderStatus == "APPROVED" {
						approvedEventSeen = true
					}
				case "CAPTURE_ATTEMPT_STARTED", "CAPTURE_RETRY_STARTED":
					if !approvedEventSeen {
						return InvariantResult{
							Status:         StatusFail,
							Summary:        fmt.Sprintf("order %s began capturing with no observed buyer approval for THIS order", o.ID),
							Counterexample: map[string]any{"order_id": o.ID, "event_id": e.ID, "event_type": e.EventType},
						}
					}
				}
			}
			approvedSeen = approvedEventSeen
		}
		if !approvedSeen {
			return InvariantResult{
				Status:  StatusFail,
				Summary: fmt.Sprintf("order %s has capture activity but no buyer-approval observation belonging to it", o.ID),
				Counterexample: map[string]any{
					"order_id": o.ID, "provider_order_id": o.ProviderOrderID, "first_capture_obs_seq": firstCaptureSeq,
				},
			}
		}
	}
	if checked == 0 {
		return result(StatusNotApplicable, "no capture activity in evidence")
	}
	return result(StatusPass, "every capture attempt is preceded by an observed buyer approval belonging to the same order")
}

// I4: for every outbound mutation, the operation identity and request id were
// durable in the database BEFORE the call left the process; every capture
// request id maps to exactly the operation row that survives.
func checkI4(ev *Evidence) InvariantResult {
	applicable := false
	for _, obs := range ev.Provider {
		if obs.Method == "read" {
			continue
		}
		applicable = true
		if !obs.DurableBeforeCall {
			return InvariantResult{
				Status:  StatusFail,
				Summary: "an outbound provider call happened before the operation identity was durably committed",
				Counterexample: map[string]any{
					"obs_seq": obs.Seq, "method": obs.Method, "request_id": obs.RequestID,
				},
			}
		}
		if obs.Method == "capture" {
			if ev.opByRequest(obs.RequestID) == nil {
				return InvariantResult{
					Status:         StatusFail,
					Summary:        "a capture call used a request id with no durable operation row",
					Counterexample: map[string]any{"obs_seq": obs.Seq, "request_id": obs.RequestID},
				}
			}
		}
	}
	if !applicable {
		return result(StatusNotApplicable, "no outbound provider mutations in evidence")
	}

	// Identity continuity: for one logical order, a capture mutation may only
	// use a NEW operation identity when every earlier attempt has
	// evidence-backed definite closure (a refusal that proves no effect).
	// Replacing the identity while an earlier attempt has applied-or-
	// ambiguous evidence is exactly the crash-window failure I4 forbids.
	for _, o := range ev.Orders {
		obsList := ev.capturesForOrderRef(o.ProviderOrderID)
		if len(obsList) < 2 {
			continue
		}
		first := obsList[0].RequestID
		for _, later := range obsList[1:] {
			if later.RequestID == first {
				continue
			}
			for _, earlier := range ev.captureObsForRequest(first) {
				if earlier.Seq >= later.Seq {
					continue
				}
				if earlier.Applied || earlier.ResponseDropped || !earlier.DefiniteRefusal {
					return InvariantResult{
						Status:  StatusFail,
						Summary: "a capture was reissued under a NEW operation identity while an earlier attempt's outcome was not definitely closed",
						Counterexample: map[string]any{
							"order_id": o.ID, "first_request_id": first,
							"first_applied": earlier.Applied, "first_response_dropped": earlier.ResponseDropped,
							"new_request_id": later.RequestID, "new_obs_seq": later.Seq,
						},
					}
				}
			}
			first = later.RequestID
		}
	}
	return result(StatusPass, "every outbound mutation was preceded by its durable operation row (probed before the call); no capture identity was replaced while an earlier attempt remained unresolved")
}

// I5: an ambiguous outcome (dropped/unreadable response, mutation possibly
// applied) must never be converted into a definite failure, must never mint a
// new capture identity, and no definite-failure claim may contradict applied
// provider evidence.
func checkI5(ev *Evidence) InvariantResult {
	applicable := false
	for _, obs := range ev.Provider {
		if obs.Method == "capture" {
			applicable = true
			break
		}
	}
	if !applicable {
		return result(StatusNotApplicable, "no capture observations in evidence")
	}

	for _, obs := range ev.Provider {
		if obs.Method != "capture" || !obs.ResponseDropped {
			continue
		}
		// (a) no later capture mutation for the SAME ORDER may use a
		// different request id (other orders' activity is irrelevant here).
		for _, later := range ev.Provider {
			if later.Method == "capture" && later.OrderRef == obs.OrderRef &&
				later.Seq > obs.Seq && later.RequestID != obs.RequestID {
				return InvariantResult{
					Status:  StatusFail,
					Summary: "after an ambiguous capture outcome, a NEW capture identity was issued (the ambiguity was treated as failure)",
					Counterexample: map[string]any{
						"ambiguous_request_id": obs.RequestID, "ambiguous_obs_seq": obs.Seq,
						"new_request_id": later.RequestID, "new_obs_seq": later.Seq,
						"order_ref": obs.OrderRef,
					},
				}
			}
		}
		// (b) a new capture operation minted after the ambiguous send is the
		// same violation observed from the local side.
		for _, op := range ev.Operations {
			if op.Type != "CAPTURE" || op.ProviderRequestID == obs.RequestID {
				continue
			}
			if orderRefOfOp(ev, op) == obs.OrderRef && op.CreatedAt.After(obs.At) {
				return InvariantResult{
					Status:  StatusFail,
					Summary: "a new capture operation was minted after an ambiguous capture outcome",
					Counterexample: map[string]any{
						"ambiguous_request_id": obs.RequestID, "new_operation_id": op.ID,
						"new_request_id": op.ProviderRequestID,
					},
				}
			}
		}
		// (c) the ambiguous attempt must not be locally resolved as a
		// definite failure without provider evidence of a refusal.
		if op := ev.opByRequest(obs.RequestID); op != nil && op.Status == "REJECTED" {
			refused := false
			for _, extra := range ev.captureObsForRequest(obs.RequestID) {
				if extra.DroppedOrRefused() {
					refused = true
				}
			}
			if !refused {
				return InvariantResult{
					Status:         StatusFail,
					Summary:        "an ambiguous capture was locally resolved as REJECTED with no definite provider refusal",
					Counterexample: map[string]any{"request_id": obs.RequestID, "operation_id": op.ID},
				}
			}
		}
	}
	// (d) no definite-failure claim may contradict applied provider evidence.
	for _, obs := range ev.Provider {
		if obs.Method != "capture" || !obs.Applied {
			continue
		}
		if op := ev.opByRequest(obs.RequestID); op != nil && op.Status == "REJECTED" {
			return InvariantResult{
				Status:         StatusFail,
				Summary:        "a capture the provider applied was locally recorded as a definite failure",
				Counterexample: map[string]any{"request_id": obs.RequestID, "operation_id": op.ID, "capture_id": obs.CaptureID},
			}
		}
		if o := orderByProviderRef(ev, obs.OrderRef); o != nil && o.Status == "FAILED_DEFINITE" {
			return InvariantResult{
				Status:         StatusFail,
				Summary:        "an order marked FAILED_DEFINITE has provider evidence of an applied capture",
				Counterexample: map[string]any{"order_id": o.ID, "capture_id": obs.CaptureID},
			}
		}
	}
	return result(StatusPass, "no ambiguity was converted to failure; no new identity followed an ambiguous outcome; no false failure contradicts provider evidence")
}

// I6: every completed-capture claim requires matching provider evidence
// (COMPLETED capture, same order, exact amount and currency). Missing
// evidence is INCONCLUSIVE; contradictory evidence is FAIL.
func checkI6(ev *Evidence) InvariantResult {
	claimed := []LocalOrder{}
	for _, o := range ev.Orders {
		if o.Status == "CAPTURED_VERIFIED" {
			claimed = append(claimed, o)
			continue
		}
		for _, op := range ev.captureOps(o.ID) {
			if op.Status == "SUCCEEDED" && op.ProviderCaptureID != "" {
				claimed = append(claimed, o)
				break
			}
		}
	}
	if len(claimed) == 0 {
		return result(StatusNotApplicable, "no completed-capture claim in evidence")
	}
	for _, o := range claimed {
		if r, ok := classifyClaimAgainstEvidence(ev, o); !ok {
			return r
		}
	}
	return result(StatusPass, "every completed-capture claim is corroborated by matching provider evidence (order, amount, currency, COMPLETED status)")
}

// classifyClaimAgainstEvidence judges ONE claimed order against its provider
// observations, binding the claim to the exact identity the merchant recorded:
// the SUCCEEDED capture operation's provider capture id.
//
//   - local claim without a capture identity → INCONCLUSIVE (cannot bind);
//   - provider evidence missing entirely → INCONCLUSIVE;
//   - provider evidence whose capture id / status / amount / currency
//     contradicts the claim → FAIL (a matching observation never hides a
//     materially contradictory one);
//   - matching identity AND financial evidence → corroborated (ok=true).
func classifyClaimAgainstEvidence(ev *Evidence, o LocalOrder) (InvariantResult, bool) {
	claimID := ""
	for _, op := range ev.captureOps(o.ID) {
		if op.Status == "SUCCEEDED" && op.ProviderCaptureID != "" {
			claimID = op.ProviderCaptureID
			break
		}
	}
	if claimID == "" {
		return InvariantResult{
			Status:  StatusInconclusive,
			Summary: fmt.Sprintf("order %s claims a completed capture but carries no capture identity to bind against provider evidence", o.ID),
			Counterexample: map[string]any{
				"order_id": o.ID, "claim": "CAPTURED_VERIFIED", "local_capture_id": "missing",
			},
		}, false
	}

	obsList := ev.capturesForOrderRef(o.ProviderOrderID)
	if len(obsList) == 0 {
		return InvariantResult{
			Status:  StatusInconclusive,
			Summary: fmt.Sprintf("order %s claims a completed capture, but no provider capture evidence is available", o.ID),
			Counterexample: map[string]any{
				"order_id": o.ID, "claim": claimID, "provider_evidence": "missing",
			},
		}, false
	}

	matched := false
	for _, obs := range obsList {
		if !obs.Applied {
			continue // idempotent replays and refusals are I7's domain
		}
		if obs.CaptureID != claimID {
			return InvariantResult{
				Status:  StatusFail,
				Summary: fmt.Sprintf("order %s claims capture %s, but the provider produced capture %s — contradictory identity evidence", o.ID, claimID, obs.CaptureID),
				Evidence: []string{
					fmt.Sprintf("obs#%d", obs.Seq),
				},
				Counterexample: map[string]any{
					"order_id": o.ID, "claimed_capture_id": claimID,
					"provider_capture_id": obs.CaptureID, "obs_seq": obs.Seq,
				},
			}, false
		}
		if obs.CaptureStatus != "COMPLETED" {
			if obs.CaptureStatus == "" {
				continue // no terminal status observed on this entry; non-material here
			}
			return InvariantResult{
				Status:  StatusFail,
				Summary: fmt.Sprintf("order %s claims capture %s as completed, but the provider shows status %q for it", o.ID, claimID, obs.CaptureStatus),
				Evidence: []string{
					fmt.Sprintf("obs#%d", obs.Seq),
				},
				Counterexample: map[string]any{
					"order_id": o.ID, "claimed_capture_id": claimID, "observed_capture_status": obs.CaptureStatus,
				},
			}, false
		}
		amountOK := false
		if obs.CaptureAmount != "" {
			if got, ok := parseAmountExact(obs.CaptureAmount); ok && got == o.TotalMinor && obs.CaptureCurrency == o.Currency {
				amountOK = true
			}
		}
		if !amountOK {
			return InvariantResult{
				Status:  StatusFail,
				Summary: fmt.Sprintf("order %s claims capture %s for %s %s, but the provider evidence shows %q %q", o.ID, claimID, amountString(o.TotalMinor), o.Currency, obs.CaptureAmount, obs.CaptureCurrency),
				Evidence: []string{
					fmt.Sprintf("obs#%d", obs.Seq),
				},
				Counterexample: map[string]any{
					"order_id": o.ID, "claimed_capture_id": claimID,
					"claim":    fmt.Sprintf("%s %s", amountString(o.TotalMinor), o.Currency),
					"provider": fmt.Sprintf("capture_id=%q status=%q amount=%q %q", obs.CaptureID, obs.CaptureStatus, obs.CaptureAmount, obs.CaptureCurrency),
				},
			}, false
		}
		matched = true
	}
	if !matched {
		return InvariantResult{
			Status:  StatusInconclusive,
			Summary: fmt.Sprintf("order %s claims capture %s, but no applied provider observation carries that identity", o.ID, claimID),
			Counterexample: map[string]any{
				"order_id": o.ID, "claimed_capture_id": claimID,
			},
		}, false
	}
	return InvariantResult{}, true
}

// I7: one logical order produces at most ONE applied capture mutation effect,
// and no duplicate capture pathway may be accumulated once an attempt has
// real provider evidence.
func checkI7(ev *Evidence) InvariantResult {
	applicable := false
	for _, obs := range ev.Provider {
		if obs.Method == "capture" {
			applicable = true
		}
	}
	if !applicable {
		return result(StatusNotApplicable, "no capture observations in evidence")
	}

	// (a) at most one applied capture effect per provider order.
	effects := map[string][]ProviderObs{}
	for _, obs := range ev.Provider {
		if obs.Method == "capture" && obs.Applied && !obs.Replayed {
			effects[obs.OrderRef] = append(effects[obs.OrderRef], obs)
		}
	}
	for ref, list := range effects {
		if len(list) > 1 {
			return InvariantResult{
				Status:  StatusFail,
				Summary: fmt.Sprintf("the provider executed %d capture effects for one logical order", len(list)),
				Counterexample: map[string]any{
					"order_ref": ref, "effect_capture_ids": captureIDs(list),
				},
			}
		}
	}

	// (b) duplicate local pathway: a second capture operation may exist only
	// when every earlier one has evidence-backed definite closure. An earlier
	// attempt with applied-or-ambiguous provider evidence, force-closed, is a
	// violation.
	for _, o := range ev.Orders {
		ops := ev.captureOps(o.ID)
		for i := 0; i < len(ops)-1; i++ {
			op := ops[i]
			closedDefinitely := false
			obsList := ev.captureObsForRequest(op.ProviderRequestID)
			for _, obs := range obsList {
				if !obs.Applied && !obs.ResponseDropped && obs.DefiniteRefusal {
					closedDefinitely = true
				}
			}
			if !closedDefinitely {
				return InvariantResult{
					Status:  StatusFail,
					Summary: "a duplicate capture pathway was created: an earlier capture operation without evidence-backed definite closure was abandoned in favor of a new one",
					Counterexample: map[string]any{
						"order_id": o.ID, "earlier_operation_id": op.ID, "earlier_request_id": op.ProviderRequestID,
						"earlier_status": op.Status, "capture_operations": len(ops),
					},
				}
			}
		}
	}

	// (c) idempotent replays must return the original capture.
	for ref, list := range effects {
		for _, obs := range ev.Provider {
			if obs.Method != "capture" || obs.OrderRef != ref || !obs.Replayed {
				continue
			}
			if obs.CaptureID != list[0].CaptureID {
				return InvariantResult{
					Status:  StatusFail,
					Summary: "an idempotent replay returned a different capture resource",
					Counterexample: map[string]any{
						"order_ref": ref, "original_capture_id": list[0].CaptureID, "replay_capture_id": obs.CaptureID,
					},
				}
			}
		}
	}
	return result(StatusPass, "at most one applied capture effect per logical order; no duplicate capture pathway; idempotent replays returned the original capture")
}

// I8: a verified capture is terminal — no later event may downgrade it and
// the final local status must remain CAPTURED_VERIFIED.
func checkI8(ev *Evidence) InvariantResult {
	verifyEvent := map[string]int64{} // order -> event id of the confirm
	for _, e := range ev.Events {
		if e.EventType == "PROVIDER_CAPTURE_CONFIRMED" || e.EventType == "PROVIDER_CAPTURE_RECOVERED" {
			if prev, ok := verifyEvent[e.OrderID]; !ok || e.ID > prev {
				verifyEvent[e.OrderID] = e.ID
			}
		}
	}
	if len(verifyEvent) == 0 {
		return result(StatusNotApplicable, "no capture confirmation in evidence")
	}
	downgrades := map[string]bool{
		"CAPTURE_OUTCOME_UNKNOWN":   true,
		"CAPTURE_REJECTED_DEFINITE": true,
		"RECONCILIATION_REQUIRED":   true,
	}
	for oid, confirmID := range verifyEvent {
		for _, e := range ev.Events {
			if e.OrderID == oid && e.ID > confirmID && downgrades[e.EventType] {
				return InvariantResult{
					Status:  StatusFail,
					Summary: "a verified capture was downgraded by a later journal event",
					Counterexample: map[string]any{
						"order_id": oid, "confirm_event_id": confirmID, "downgrade_event": e.EventType, "downgrade_event_id": e.ID,
					},
				}
			}
		}
		o := ev.order(oid)
		if o != nil && o.Status != "CAPTURED_VERIFIED" {
			return InvariantResult{
				Status:         StatusFail,
				Summary:        "final order status rolled back from a verified capture",
				Counterexample: map[string]any{"order_id": oid, "final_status": o.Status, "confirm_event_id": confirmID},
			}
		}
	}
	return result(StatusPass, "every capture confirmation is terminal in the journal and in the final order status")
}

// I9: every significant local state (verified, unknown, reconciliation,
// definite failure) must be backed by an explicit journal event — divergence
// is documented, never silently papered over.
func checkI9(ev *Evidence) InvariantResult {
	if len(ev.Orders) == 0 {
		return result(StatusNotApplicable, "no orders in evidence")
	}
	required := map[string]string{
		"CAPTURED_VERIFIED":       "PROVIDER_CAPTURE_CONFIRMED|PROVIDER_CAPTURE_RECOVERED",
		"CAPTURE_OUTCOME_UNKNOWN": "CAPTURE_OUTCOME_UNKNOWN",
		"RECONCILIATION_REQUIRED": "RECONCILIATION_REQUIRED",
		"FAILED_DEFINITE":         "PROVIDER_CREATE_REJECTED|CAPTURE_REJECTED_DEFINITE",
	}
	for _, o := range ev.Orders {
		need, ok := required[o.Status]
		if !ok {
			continue
		}
		found := false
		for _, alt := range strings.Split(need, "|") {
			if ev.eventSeen(o.ID, alt) {
				found = true
				break
			}
		}
		if !found {
			return InvariantResult{
				Status:         StatusFail,
				Summary:        fmt.Sprintf("order status %s is not backed by an explicit journal event (divergence would be silent)", o.Status),
				Counterexample: map[string]any{"order_id": o.ID, "status": o.Status, "expected_event": need},
			}
		}
	}
	return result(StatusPass, "every significant local state carries its explicit journal event")
}

// I10: refunds are out of scope for M2; if refund evidence ever appears
// without verification support, the oracle must say INCONCLUSIVE rather than
// pretend to judge it.
func checkI10(ev *Evidence) InvariantResult {
	for _, op := range ev.Operations {
		if strings.Contains(strings.ToUpper(op.Type), "REFUND") {
			return InvariantResult{
				Status:         StatusInconclusive,
				Summary:        "refund operations are present but refund-resource verification is not implemented in the M2 oracle",
				Counterexample: map[string]any{"operation_id": op.ID, "operation_type": op.Type},
			}
		}
	}
	for _, e := range ev.Events {
		if strings.Contains(strings.ToUpper(e.EventType), "REFUND") {
			return InvariantResult{
				Status:         StatusInconclusive,
				Summary:        "refund events are present but refund-resource verification is not implemented in the M2 oracle",
				Counterexample: map[string]any{"event_id": e.ID, "event_type": e.EventType},
			}
		}
	}
	return result(StatusNotApplicable, "no refund activity in the executed scenarios (refunds are not required for M2)")
}

// I11: no auto-apply happened. Deliberately scoped to M2 reality: this
// milestone exposes NO patch-application mechanism at all, so the check can
// only confirm that no such action occurred — it does not (and must not)
// claim to enforce restrictions on a future apply pipeline.
func checkI11(ev *Evidence) InvariantResult {
	for k, v := range ev.Facts {
		if strings.HasPrefix(k, "auto_") && v != "" && v != "false" && v != "0" {
			return InvariantResult{
				Status:         StatusFail,
				Summary:        "the run claims an automatic apply/deploy action",
				Counterexample: map[string]any{"fact": k, "value": v},
			}
		}
	}
	return result(StatusPass, "M2 performs no apply/deploy step and exposes no patch-application mechanism; the verdict is an advisory output only (automatic-apply prevention for a future pipeline is out of M2 scope)")
}

// I12: evidence provenance is explicit for BOTH provider observations and the
// durable journal, and scripted fixture interactions may never be labelled as
// real PayPal evidence.
func checkI12(ev *Evidence) InvariantResult {
	applicable := false
	scripted := false
	for _, obs := range ev.Provider {
		applicable = true
		if obs.Tier == "" {
			return InvariantResult{
				Status:         StatusFail,
				Summary:        "a provider observation carries no provenance tier",
				Counterexample: map[string]any{"obs_seq": obs.Seq, "order_ref": obs.OrderRef},
			}
		}
		if strings.Contains(obs.Source, "scripted") {
			scripted = true
			if obs.Tier == TierReal {
				return InvariantResult{
					Status:         StatusFail,
					Summary:        "a scripted fixture interaction is labelled as real provider evidence",
					Counterexample: map[string]any{"obs_seq": obs.Seq, "source": obs.Source, "tier": obs.Tier},
				}
			}
		}
	}

	// The durable journal carries provenance too. Provider-derived events
	// (source PAYPAL) written while a scripted provider was wired must be
	// tagged F at their origin; genuine Sandbox observations keep R.
	for _, e := range ev.Events {
		if e.Tier == "" {
			return InvariantResult{
				Status:         StatusFail,
				Summary:        "a journal event carries no provenance tier",
				Counterexample: map[string]any{"event_id": e.ID, "event_type": e.EventType, "source": e.Source},
			}
		}
		if e.Source == "PAYPAL" && scripted && e.Tier == TierReal {
			return InvariantResult{
				Status:  StatusFail,
				Summary: "a provider-derived journal event is labelled as real provider evidence in a scripted run",
				Counterexample: map[string]any{
					"event_id": e.ID, "event_type": e.EventType, "source": e.Source, "tier": e.Tier,
				},
			}
		}
	}

	if !applicable {
		return result(StatusNotApplicable, "no provider observations in evidence")
	}
	if ev.Scenario.EvidenceTier == "" {
		return InvariantResult{Status: StatusFail, Summary: "scenario declares no evidence tier"}
	}
	return result(StatusPass, "provenance is explicit across provider observations and the journal; scripted interactions are labelled F, never R")
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// orderByProviderRef finds the order whose provider order id matches a ref.
func orderByProviderRef(ev *Evidence, ref string) *LocalOrder {
	if ref == "" {
		return nil
	}
	for i := range ev.Orders {
		if ev.Orders[i].ProviderOrderID == ref {
			return &ev.Orders[i]
		}
	}
	return nil
}

// orderRefOfOp resolves the provider order ref an operation belongs to.
func orderRefOfOp(ev *Evidence, op LocalOp) string {
	if o := ev.order(op.OrderID); o != nil {
		return o.ProviderOrderID
	}
	return op.ProviderOrderID
}

// captureIDs extracts capture ids for a counterexample.
func captureIDs(list []ProviderObs) []string {
	out := make([]string, 0, len(list))
	for _, obs := range list {
		out = append(out, obs.CaptureID)
	}
	return out
}

// DroppedOrRefused reports whether this observation established a definite
// provider refusal (or a definitive non-effect).
func (o ProviderObs) DroppedOrRefused() bool {
	return o.DefiniteRefusal && !o.ResponseDropped && !o.Applied
}
