// Package oracle — proof bundles.
//
// A bundle is the portable, redacted artifact of one evaluated run: the exact
// code revision, scenario, invariant outcomes with evidence pointers, the
// combined verdict, and explicit limitations. Bundles are derived from the
// actual run (no placeholders) and contain no credentials, buyer identities,
// or raw provider payloads.
package oracle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// BundleSchemaVersion is the proof-bundle schema version.
const BundleSchemaVersion = 1

// OracleVersion identifies the oracle implementation's semantics.
const OracleVersion = "waypoint-oracle-m2.0.0"

// Bundle is the redacted proof artifact for one scenario/candidate pair.
type Bundle struct {
	SchemaVersion   int               `json:"schema_version"`
	GeneratedAt     time.Time         `json:"generated_at"`
	ScenarioID      string            `json:"scenario_id"`
	ScenarioTitle   string            `json:"scenario_title"`
	ScenarioFamily  string            `json:"scenario_family"`
	Candidate       string            `json:"candidate"`
	Verdict         Verdict           `json:"verdict"`
	ExpectedVerdict Verdict           `json:"expected_verdict"`
	FixtureRevision string            `json:"fixture_revision"`
	OracleVersion   string            `json:"oracle_version"`
	EvidenceTiers   []string          `json:"evidence_tiers"`
	Invariants      []InvariantResult `json:"invariants"`
	Skips           []InvariantResult `json:"skips"`
	Counterexamples []InvariantResult `json:"counterexamples"`
	EvidenceDigest  string            `json:"evidence_digest_sha256"`
	EvidenceCounts  map[string]int    `json:"evidence_counts"`
	PayPalSandbox   SandboxNote       `json:"paypal_sandbox"`
	Applied         bool              `json:"applied"`
	Limitations     []string          `json:"limitations"`
}

// SandboxNote states honestly whether the bundle contains real provider
// evidence. M2's deterministic scenarios do not.
type SandboxNote struct {
	Verified bool   `json:"verified"`
	Note     string `json:"note"`
}

// redactEmail masks anything email-shaped as a defense against accidental
// PII; deterministic fixture ids never match it.
var redactEmail = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)

// BuildBundle derives the artifact from an evaluated run and its evidence.
func BuildBundle(result Result, ev *Evidence, fixtureRevision string) Bundle {
	tierSet := map[string]bool{}
	for _, obs := range ev.Provider {
		if obs.Tier != "" {
			tierSet[obs.Tier] = true
		}
	}
	if ev.Scenario.EvidenceTier != "" {
		tierSet[ev.Scenario.EvidenceTier] = true
	}
	tiers := make([]string, 0, len(tierSet))
	for t := range tierSet {
		tiers = append(tiers, t)
	}
	// stable order R, F, S
	ordered := []string{}
	for _, t := range []string{TierReal, TierInjected, TierSample} {
		if tierSet[t] {
			ordered = append(ordered, t)
		}
	}
	tiers = ordered

	realSandboxUsed := false
	for _, obs := range ev.Provider {
		if obs.Tier == TierReal {
			realSandboxUsed = true
		}
	}

	return Bundle{
		SchemaVersion:   BundleSchemaVersion,
		GeneratedAt:     time.Now().UTC(),
		ScenarioID:      ev.Scenario.ID,
		ScenarioTitle:   ev.Scenario.Title,
		ScenarioFamily:  ev.Scenario.Family,
		Candidate:       result.Candidate,
		Verdict:         result.Verdict,
		ExpectedVerdict: ev.Scenario.ExpectedVerdict,
		FixtureRevision: fixtureRevision,
		OracleVersion:   fmt.Sprintf("%s (%s)", OracleVersion, fixtureRevision),
		EvidenceTiers:   tiers,
		Invariants:      result.Invariants,
		Skips:           result.Skips(),
		Counterexamples: result.Counterexamples(),
		EvidenceDigest:  EvidenceDigest(ev),
		EvidenceCounts: map[string]int{
			"orders":                len(ev.Orders),
			"operations":            len(ev.Operations),
			"events":                len(ev.Events),
			"provider_observations": len(ev.Provider),
			"fault_events":          len(ev.Faults),
		},
		PayPalSandbox: SandboxNote{
			Verified: realSandboxUsed,
			Note:     sandboxNote(realSandboxUsed),
		},
		Applied:     false,
		Limitations: result.Limitations,
	}
}

func sandboxNote(real bool) string {
	if real {
		return "This bundle contains tier-R observations from the real PayPal Sandbox API."
	}
	return "No real PayPal Sandbox execution is included: all provider interactions in this bundle are deterministic scripted-fixture observations (tier F). The M1 tier-R capture evidence is recorded separately in the private progress log and is not re-executed."
}

// EvidenceDigest is a sha256 over the canonical JSON of the evidence.
func EvidenceDigest(ev *Evidence) string {
	b, err := json.Marshal(ev)
	if err != nil {
		return "unavailable: " + err.Error()
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Marshal renders the bundle as indented, redacted JSON.
func (b Bundle) Marshal() ([]byte, error) {
	raw, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return nil, err
	}
	return []byte(redactEmail.ReplaceAllString(string(raw), "[redacted-email]")), nil
}

// WriteBundle writes bundle.json plus per-artifact evidence files into dir.
func WriteBundle(dir string, b Bundle, ev *Evidence) (string, error) {
	if err := os.MkdirAll(filepath.Join(dir, "artifacts"), 0o755); err != nil {
		return "", err
	}
	body, err := b.Marshal()
	if err != nil {
		return "", err
	}
	bundlePath := filepath.Join(dir, "bundle.json")
	if err := os.WriteFile(bundlePath, body, 0o644); err != nil {
		return "", err
	}
	artifacts := map[string]any{
		"artifacts/local-journal.json":         map[string]any{"orders": ev.Orders, "operations": ev.Operations, "events": ev.Events},
		"artifacts/provider-observations.json": map[string]any{"observations": ev.Provider, "faults": ev.Faults, "facts": ev.Facts},
		"artifacts/invariant-results.json":     map[string]any{"verdict": b.Verdict, "invariants": b.Invariants},
	}
	for name, v := range artifacts {
		raw, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return "", err
		}
		redacted := redactEmail.ReplaceAllString(string(raw), "[redacted-email]")
		if err := os.WriteFile(filepath.Join(dir, name), []byte(redacted), 0o644); err != nil {
			return "", err
		}
	}
	return bundlePath, nil
}
