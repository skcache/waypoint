package bench_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/skcache/waypoint/apps/api/internal/bench"
	"github.com/skcache/waypoint/apps/api/internal/repair"
)

func TestCorpusHasDistinctHeldOutCasesAndStableDigest(t *testing.T) {
	cases := bench.Cases()
	if len(cases) < 6 {
		t.Fatalf("corpus has %d cases, want at least 6 total", len(cases))
	}
	ids := map[string]bool{}
	heldOut := 0
	for _, c := range cases {
		if ids[c.ID] {
			t.Fatalf("duplicate case id %q", c.ID)
		}
		ids[c.ID] = true
		if c.Exposed {
			continue
		}
		heldOut++
		if c.ExpectedCategory == "" {
			t.Fatalf("%s has no expected diagnosis category", c.ID)
		}
		if len(c.ReferenceFix) == 0 {
			t.Fatalf("%s has no human-only reference fix", c.ID)
		}
		if len(c.Mutations) == 0 {
			t.Fatalf("%s has no mutation", c.ID)
		}
	}
	if heldOut < 5 {
		t.Fatalf("held-out cases = %d, want at least 5", heldOut)
	}
	if d := bench.Digest(); !strings.HasPrefix(d, "sha256:") || len(d) != len("sha256:")+64 {
		t.Fatalf("digest = %q, want sha256:<64 hex>", d)
	}
}

// TestMutationsApplyExactlyOnce keeps the corpus honest as the fixture evolves.
// A mutation whose old text no longer occurs exactly once would silently stop
// producing the intended defect.
func TestMutationsApplyExactlyOnce(t *testing.T) {
	root := repoRoot(t)
	for _, c := range bench.Cases() {
		for _, m := range c.Mutations {
			data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(m.Path)))
			if err != nil {
				t.Fatalf("%s: %v", c.ID, err)
			}
			if n := strings.Count(string(data), m.Old); n != 1 {
				t.Fatalf("%s: mutation old text occurs %d times in %s (want 1)", c.ID, n, m.Path)
			}
		}
	}
}

// TestFrozenCorpusMatchesRegistry ensures the digest recorded before the
// hosted evaluation still describes the corpus in code.
func TestFrozenCorpusMatchesRegistry(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "evidence", "m5", "benchmark.json"))
	if err != nil {
		t.Skipf("benchmark freeze file not present: %v", err)
	}
	var frozen struct {
		Digest string `json:"digest"`
		Base   string `json:"base_sha"`
		Cases  []struct {
			ID      string `json:"id"`
			Exposed bool   `json:"exposed"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &frozen); err != nil {
		t.Fatalf("freeze file: %v", err)
	}
	if frozen.Digest != bench.Digest() {
		t.Fatalf("frozen digest %s != registry digest %s (corpus drifted after freeze)", frozen.Digest, bench.Digest())
	}
	if frozen.Base == "" {
		t.Fatal("freeze file records no base revision")
	}
	if len(frozen.Cases) != len(bench.Cases()) {
		t.Fatalf("frozen cases = %d, registry = %d", len(frozen.Cases), len(bench.Cases()))
	}
}

// TestModelVisiblePromptsCarryNoCaseOrReference: the static prompt templates
// are the only model-visible text that is not derived from evidence. Neither
// may contain a case id, a mutation replacement, or a reference fix.
func TestModelVisiblePromptsCarryNoCaseOrReference(t *testing.T) {
	prompts := repair.DiagnosisSystemPrompt() + "\n" + repair.PatchSystemPromptForTests()
	for _, c := range bench.Cases() {
		if strings.Contains(prompts, c.ID) {
			t.Fatalf("prompt contains case id %q", c.ID)
		}
		for _, m := range c.Mutations {
			if strings.Contains(prompts, m.New) {
				t.Fatalf("prompt contains mutation text for %s", c.ID)
			}
		}
		for _, m := range c.ReferenceFix {
			if strings.Contains(prompts, m.New) {
				t.Fatalf("prompt contains reference fix for %s", c.ID)
			}
		}
	}
	if !strings.Contains(strings.ToLower(prompts), "untrusted data") {
		t.Fatal("prompt does not state that evidence is untrusted data")
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "apps", "api", "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repository root not found")
		}
		dir = parent
	}
}
