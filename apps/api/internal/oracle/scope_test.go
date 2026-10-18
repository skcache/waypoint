// Scope guard: the oracle must stay outside every candidate patch's writable
// scope. This test inspects the package's real dependency graph (via the Go
// toolchain) and fails if the judge ever gains a dependency on the code it is
// supposed to judge, on the replay harness, on the LLM adapter, or on any
// command package.
package oracle_test

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/skcache/waypoint/apps/api/internal/oracle"
)

func TestOracleScopeGuard(t *testing.T) {
	cmd := exec.Command("go", "list", "-deps", "-f", "{{.ImportPath}}", ".")
	cmd.Dir = "."
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list -deps failed (is the Go toolchain available?): %v", err)
	}
	deps := strings.Split(strings.TrimSpace(string(out)), "\n")

	banned := []string{
		"/internal/merchant", // the thing being judged
		"/internal/replay",   // the evidence harness (a patch could tamper with it)
		"/internal/llm",      // the AI must never be in the verdict path
		"/internal/intent",   // V1 surface
		"/cmd/",              // command layers are not part of the judge
	}
	for _, dep := range deps {
		for _, b := range banned {
			if strings.Contains(dep, b) {
				t.Fatalf("oracle depends on %q (matched %q); the judge must remain outside candidate scope", dep, b)
			}
		}
	}

	// And it must genuinely be a real dependency set (sanity: pgx present,
	// because the oracle reads the journal itself).
	sawPgx := false
	for _, dep := range deps {
		if dep == "github.com/jackc/pgx/v5/pgxpool" {
			sawPgx = true
		}
	}
	if !sawPgx {
		t.Fatalf("expected the oracle to carry its own pgx dependency; deps: %v", deps)
	}
}

// The frozen registry must always contain exactly I1–I12 — a missing or
// reordered invariant is a policy regression, not a test tweak.
func TestInvariantRegistryIsComplete(t *testing.T) {
	want := []string{"I1", "I2", "I3", "I4", "I5", "I6", "I7", "I8", "I9", "I10", "I11", "I12"}
	if len(oracle.Invariants) != len(want) {
		t.Fatalf("registry has %d invariants, want %d", len(oracle.Invariants), len(want))
	}
	for i, inv := range oracle.Invariants {
		if inv.ID != want[i] {
			t.Fatalf("invariant %d = %s, want %s", i, inv.ID, want[i])
		}
	}
}
