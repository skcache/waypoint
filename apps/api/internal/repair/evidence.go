package repair

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/skcache/waypoint/apps/api/internal/oracle"
	"github.com/skcache/waypoint/apps/api/internal/replay"
)

// SourceFile is one bounded, redacted source excerpt shown to the model.
type SourceFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Bytes   int    `json:"bytes"`
}

// Block is one citable evidence item. The model must reference these ids; it
// cannot invent new ones.
type Block struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"` // incident | journal | observation | reference
	Title string `json:"title"`
	Body  string `json:"body"`
}

// Evidence is the complete, bounded model input for one investigation.
type Evidence struct {
	IncidentID   string               `json:"incident_id"`
	Fault        string               `json:"fault_family"`
	Tier         string               `json:"evidence_tier"`
	Revision     string               `json:"fixture_revision"`
	Incident     map[string]any       `json:"incident"`
	Journal      JournalView          `json:"journal"`
	Observations []replay.Observation `json:"observations"`
	Files        []SourceFile         `json:"files"`
	Limitations  []string             `json:"limitations"`
}

// JournalView is a bounded projection of the durable journal.
type JournalView struct {
	Orders     []oracle.LocalOrder `json:"orders"`
	Operations []oracle.LocalOp    `json:"operations"`
	Events     []oracle.LocalEvent `json:"events"`
}

const (
	maxJournalEvents = 40
	maxEventDetail   = 1200
	maxIncidentJSON  = 16 << 10
	maxObservation   = 24
	maxSourceBytes   = 48 << 10
	maxTotalSource   = 72 << 10
)

// BuildEvidence assembles the model input from a fixture run and the durable
// journal. Only allowlisted source files are included: the fault catalogue,
// store, tests, harness and oracle are never shown, so the model must reason
// from behavior and implementation rather than read the answer off a comment.
func BuildEvidence(ctx context.Context, h *replay.Harness, run replay.FixtureRun, repoRoot, revision string, allow Allowlist) (Evidence, error) {
	local, err := oracle.LoadLocal(ctx, h.Pool, []string{run.OrderID})
	if err != nil {
		return Evidence{}, fmt.Errorf("repair: load journal evidence: %w", err)
	}

	ev := Evidence{
		IncidentID: "M3-INC-" + run.Fault + "-" + run.OrderID,
		Fault:      run.Fault,
		Tier:       "F",
		Revision:   revision,
		Journal: JournalView{
			Orders:     local.Orders,
			Operations: local.Operations,
		},
		Limitations: []string{
			"All provider interactions in this run are deterministic scripted-fixture observations (tier F). No real PayPal Sandbox call was made during repair evaluation.",
			"The fixture and its defect are controlled test instrumentation, not production traffic.",
		},
	}

	incident := map[string]any{
		"incident_id":         ev.IncidentID,
		"fault_family":        run.Fault,
		"evidence_tier":       "F",
		"fixture_revision":    revision,
		"order_id":            run.OrderID,
		"order_status":        run.OrderStatus,
		"total_minor":         run.TotalMinor,
		"currency":            run.Currency,
		"claimed_capture_id":  run.ClaimedCapture,
		"provider_capture_id": providerCaptureID(run.Observations),
		"checkout_error":      run.CheckoutError,
		"capture_error":       run.CaptureError,
	}
	ev.Incident = incident
	if data, err := json.MarshalIndent(incident, "", "  "); err == nil {
		if len(data) > maxIncidentJSON {
			data = data[:maxIncidentJSON]
		}
		incident["_rendered"] = string(data)
	}

	for _, e := range local.Events {
		if len(ev.Journal.Events) >= maxJournalEvents {
			break
		}
		if len(e.Detail) > maxEventDetail {
			e.Detail = e.Detail[:maxEventDetail] + "…[truncated]"
		}
		e.Detail = scrubSecrets(e.Detail)
		ev.Journal.Events = append(ev.Journal.Events, e)
	}
	for _, obs := range run.Observations {
		if len(ev.Observations) >= maxObservation {
			break
		}
		ev.Observations = append(ev.Observations, obs)
	}

	ev.Files, err = selectSourceFiles(repoRoot, allow)
	if err != nil {
		return Evidence{}, err
	}
	return ev, nil
}

// selectSourceFiles reads the writable implementation file plus the provider
// model declarations it depends on. Everything else (tests, store, fault
// catalogue, oracle, harness) stays out of the prompt.
func selectSourceFiles(repoRoot string, allow Allowlist) ([]SourceFile, error) {
	candidates := []string{filepath.Join(allow.Root, "service.go")}
	extras := make([]string, 0, len(allow.Existing))
	for rel := range allow.Existing {
		if rel != "service.go" {
			extras = append(extras, rel)
		}
	}
	sort.Strings(extras)
	for _, rel := range extras {
		candidates = append(candidates, filepath.Join(allow.Root, rel))
	}
	candidates = append(candidates, "apps/api/internal/payments/paypal/models.go")
	var out []SourceFile
	total := 0
	for _, rel := range candidates {
		full := filepath.Join(repoRoot, filepath.FromSlash(rel))
		data, err := os.ReadFile(full)
		if err != nil {
			return nil, fmt.Errorf("repair: read source %s: %w", rel, err)
		}
		content := string(data)
		if len(content) > maxSourceBytes {
			content = content[:maxSourceBytes] + "\n... [source truncated] ...\n"
		}
		content = scrubSecrets(content)
		total += len(content)
		if total > maxTotalSource {
			break
		}
		out = append(out, SourceFile{Path: rel, Content: content, Bytes: len(content)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func providerCaptureID(obs []replay.Observation) string {
	for _, o := range obs {
		if o.Method == "capture" && o.Applied && o.CaptureID != "" {
			return o.CaptureID
		}
	}
	return ""
}

// Blocks renders the evidence as citable, ordered items.
func (e Evidence) Blocks() []Block {
	blocks := []Block{{
		ID:    "EV-1",
		Kind:  "incident",
		Title: "Sanitized incident artifact",
		Body:  mustJSON(e.Incident),
	}}
	n := 2
	for _, o := range e.Journal.Orders {
		blocks = append(blocks, Block{
			ID:    fmt.Sprintf("EV-%d", n),
			Kind:  "journal",
			Title: fmt.Sprintf("merchant_orders row %s", o.ID),
			Body:  mustJSON(o),
		})
		n++
	}
	for _, op := range e.Journal.Operations {
		blocks = append(blocks, Block{
			ID:    fmt.Sprintf("EV-%d", n),
			Kind:  "journal",
			Title: fmt.Sprintf("merchant_operations row %s (%s)", op.ID, op.Type),
			Body:  mustJSON(op),
		})
		n++
	}
	for _, evt := range e.Journal.Events {
		blocks = append(blocks, Block{
			ID:    fmt.Sprintf("EV-%d", n),
			Kind:  "journal",
			Title: fmt.Sprintf("merchant_events #%d %s/%s", evt.ID, evt.Source, evt.EventType),
			Body:  mustJSON(evt),
		})
		n++
	}
	for _, o := range e.Observations {
		blocks = append(blocks, Block{
			ID:    fmt.Sprintf("EV-%d", n),
			Kind:  "observation",
			Title: fmt.Sprintf("provider observation #%d (%s)", o.Seq, o.Method),
			Body:  mustJSON(o),
		})
		n++
	}
	return blocks
}

// BlockIDs is the citable set.
func (e Evidence) BlockIDs() map[string]bool {
	out := map[string]bool{}
	for _, b := range e.Blocks() {
		out[b.ID] = true
	}
	return out
}

func mustJSON(v any) string {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("{\"_unrenderable\":%q}", err.Error())
	}
	if len(data) > maxEventDetail<<2 {
		data = data[:maxEventDetail<<2]
	}
	return string(data)
}

// ValidateForModel rejects evidence that could leak a forbidden credential.
func (e Evidence) ValidateForModel() error {
	rendered := strings.ToLower(mustJSON(e))
	for _, p := range secretPatterns {
		if strings.Contains(rendered, p) {
			return fmt.Errorf("repair: evidence contains a value resembling a credential; refusing to send it")
		}
	}
	return nil
}

// Age reports how long the evidence has existed (one fixture run).
func (e Evidence) Age(started time.Time) time.Duration { return time.Since(started) }
