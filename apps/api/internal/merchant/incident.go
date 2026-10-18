package merchant

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Incident is the sanitized failure artifact produced by the merchant
// fixture. It is the input the Waypoint V2 control plane will ingest in M2+:
// correlation, the failing operation, the provider's own diagnostic fields,
// and the append-only timeline. It never contains credentials, authorization
// headers, or raw provider bodies, and it is safe to keep under a local,
// git-ignored directory.
type Incident struct {
	SchemaVersion       int            `json:"schema_version"`
	IncidentID          string         `json:"incident_id"`
	CreatedAt           time.Time      `json:"created_at"`
	FaultMode           string         `json:"fault_mode"`
	TraceID             string         `json:"trace_id"`
	OrderID             string         `json:"order_id"`
	ProductID           string         `json:"product_id"`
	Quantity            int            `json:"quantity"`
	TotalMinor          int64          `json:"total_minor"`
	Currency            string         `json:"currency"`
	FailingOperation    Operation      `json:"failing_operation"`
	ProviderObservation map[string]any `json:"provider_observation,omitempty"`
	Timeline            []Event        `json:"timeline"`
	EvidenceTier        string         `json:"evidence_tier"`
	Limitations         []string       `json:"limitations"`
}

// BuildIncident derives the artifact from persisted state. The failing
// operation is the most recent REJECTED or UNKNOWN operation (create first
// when both exist, since a capture is only attempted after a create
// succeeds).
func BuildIncident(detail OrderDetail, fault string) (Incident, error) {
	inc := Incident{
		SchemaVersion: 1,
		IncidentID:    "M1-INC-" + detail.Order.ID,
		CreatedAt:     time.Now().UTC(),
		FaultMode:     fault,
		TraceID:       detail.Order.TraceID,
		OrderID:       detail.Order.ID,
		ProductID:     detail.Order.ProductID,
		Quantity:      detail.Order.Quantity,
		TotalMinor:    detail.Order.TotalMinor,
		Currency:      detail.Order.Currency,
		Timeline:      detail.Events,
		EvidenceTier:  TierReal,
	}

	if fault != "" && fault != FaultNone {
		inc.EvidenceTier = TierFault
	}

	found := false
	for i := len(detail.Operations) - 1; i >= 0; i-- {
		op := detail.Operations[i]
		if op.Status == OpStatusRejected || op.Status == OpStatusUnknown {
			inc.FailingOperation = op
			found = true
			break
		}
	}
	if !found {
		return Incident{}, fmt.Errorf("merchant: order %s has no failed operation to build an incident from", detail.Order.ID)
	}

	inc.ProviderObservation = observationFromTimeline(detail.Events, inc.FailingOperation)

	if inc.EvidenceTier == TierFault {
		inc.Limitations = append(inc.Limitations,
			"the malformed provider request was produced by the fixture's injectable formatting defect (fault "+fault+"); the provider rejection itself is a real PayPal Sandbox response")
	} else {
		inc.Limitations = append(inc.Limitations,
			"no fault was injected for this incident; evidence reflects the fixture's unmodified behavior")
	}
	inc.Limitations = append(inc.Limitations,
		"this artifact describes one fixture checkout, not a production incident")
	return inc, nil
}

// observationFromTimeline extracts the sanitized provider diagnostic fields
// stored with the operation's failure event.
func observationFromTimeline(events []Event, op Operation) map[string]any {
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		if e.Source != "PAYPAL" {
			continue
		}
		var d map[string]any
		if err := json.Unmarshal(e.Detail, &d); err != nil {
			continue
		}
		if got, _ := d["provider_request_id"].(string); got != "" && got != op.ProviderRequestID {
			continue
		}
		obs := map[string]any{"event_type": e.EventType, "tier": e.Tier}
		for _, key := range []string{
			"provider_http_status", "provider_error_name", "provider_error_message",
			"provider_debug_id", "provider_issues", "response_unreadable", "local_validation_refusal",
		} {
			if v, ok := d[key]; ok {
				obs[key] = v
			}
		}
		return obs
	}
	return nil
}

// WriteIncident writes the artifact atomically (temp file + rename) into dir
// and returns the final path.
func WriteIncident(dir string, inc Incident) (string, error) {
	if strings.TrimSpace(dir) == "" {
		return "", fmt.Errorf("merchant: incident directory is not configured")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("merchant: create incident directory: %w", err)
	}
	data, err := json.MarshalIndent(inc, "", "  ")
	if err != nil {
		return "", fmt.Errorf("merchant: encode incident: %w", err)
	}
	final := filepath.Join(dir, inc.OrderID+".json")
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return "", fmt.Errorf("merchant: write incident: %w", err)
	}
	if err := os.Rename(tmp, final); err != nil {
		return "", fmt.Errorf("merchant: finalize incident: %w", err)
	}
	return final, nil
}
