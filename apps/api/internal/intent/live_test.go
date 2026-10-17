package intent

import (
	"context"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/skcache/waypoint/apps/api/internal/llm"
)

// TestLiveCompile exercises the real hosted compiler end to end. It runs
// only when LLM_LIVE=1 is set AND the LLM_* configuration is complete; a
// skipped test is not evidence of a live model call. Nothing here runs as
// part of the normal suite, and no real credentials are ever required in CI.
func TestLiveCompile(t *testing.T) {
	if os.Getenv("LLM_LIVE") != "1" {
		t.Skip("set LLM_LIVE=1 (with LLM_API_BASE_URL, LLM_API_KEY, LLM_MODEL) to exercise the real provider")
	}
	cfg, err := llm.ConfigFromEnv(os.Getenv)
	if err != nil {
		t.Fatalf("LLM_LIVE=1 but configuration is incomplete: %v", err)
	}
	client, err := llm.New(cfg)
	if err != nil {
		t.Fatalf("llm.New: %v", err)
	}
	provider := ""
	if u, err := url.Parse(cfg.BaseURL); err == nil {
		provider = u.Host
	}
	compiler := NewHostedCompiler(client, provider, cfg.Model)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	res, err := compiler.Compile(ctx, "Buy one quiet office keyboard for office work, under $150.")
	if err != nil {
		t.Fatalf("live compile failed: %v", err)
	}
	d := res.Draft
	if d.Status != DraftStatusDraft || d.Spendable {
		t.Fatalf("live draft must remain non-authoritative: status=%q spendable=%v", d.Status, d.Spendable)
	}
	t.Logf("live draft: currency=%v ceiling=%v qty=%v category=%v prefs=%v unresolved=%v assumptions=%v",
		d.Terms.Currency, d.Terms.MaxTotalMinor, d.Terms.MaxQuantity, d.Terms.Category,
		d.Terms.QualitativePreferences, d.UnresolvedFields, d.Assumptions)
	t.Logf("evidence: provider=%s model=%s", res.Provider, res.Model)

	if d.Terms.MaxTotalMinor == nil || *d.Terms.MaxTotalMinor != 15000 {
		t.Errorf("expected max_total_minor=15000 from \"under $150\", got %v", d.Terms.MaxTotalMinor)
	}
	if d.Terms.Category == nil || *d.Terms.Category != "keyboard" {
		t.Errorf("expected category=keyboard, got %v", d.Terms.Category)
	}
	if d.Terms.Currency == nil || *d.Terms.Currency != "USD" {
		t.Errorf("expected currency=USD, got %v", d.Terms.Currency)
	}
}
