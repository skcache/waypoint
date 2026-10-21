package repair_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/skcache/waypoint/apps/api/internal/repair"
)

func TestAllowlistRejectsEverythingOutsideTheSurface(t *testing.T) {
	allow := repair.DefaultAllowlist()
	bad := []string{
		"",
		"/etc/passwd",
		"apps/api/internal/merchant/../../../etc/passwd",
		"apps/api/internal/merchant/../../merchant/service.go",
		`apps/api/internal/merchant/service.go\..\..\x`,
		"apps/api/internal/merchant/store.go",
		"apps/api/internal/merchant/types.go",
		"apps/api/internal/merchant/incident.go",
		"apps/api/internal/merchant/service_test.go",
		"apps/api/internal/merchant/sub/helper.go",
		"apps/api/internal/replay/provider.go",
		"apps/api/internal/oracle/invariants.go",
		"apps/api/cmd/repaircheck/main.go",
		"apps/api/internal/merchant/not_a_go.txt",
		"apps/api/internal/merchant/Bad-Name.go",
	}
	for _, p := range bad {
		if err := allow.Validate(p); err == nil {
			t.Errorf("Validate(%q) = nil, want rejection", p)
		}
	}

	good := []string{
		"apps/api/internal/merchant/service.go",
		"apps/api/internal/merchant/capture_result.go",
	}
	for _, p := range good {
		if err := allow.Validate(p); err != nil {
			t.Errorf("Validate(%q) = %v, want allowed", p, err)
		}
	}
}

func TestRunBoundedKillsTheProcessGroupOnTimeout(t *testing.T) {
	start := time.Now()
	res := repair.RunBounded(context.Background(), t.TempDir(), 300*time.Millisecond, os.Environ(), "sh", "-c", "sleep 30")
	if !res.TimedOut {
		t.Fatalf("TimedOut = false, exit=%d output=%q", res.ExitCode, res.Output)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("timeout took %v, want a prompt group kill", d)
	}
}

func TestRunBoundedPropagatesExitAndCombinesStreams(t *testing.T) {
	res := repair.RunBounded(context.Background(), t.TempDir(), 10*time.Second, os.Environ(), "sh", "-c", "echo out; echo err >&2; exit 7")
	if res.ExitCode != 7 || res.TimedOut {
		t.Fatalf("exit = %d timedOut=%v, want 7/false", res.ExitCode, res.TimedOut)
	}
	if !strings.Contains(res.Output, "out") || !strings.Contains(res.Output, "err") {
		t.Fatalf("output lost a stream: %q", res.Output)
	}
}

func TestRunBoundedCapsRunawayOutput(t *testing.T) {
	res := repair.RunBounded(context.Background(), t.TempDir(), 20*time.Second, os.Environ(),
		"sh", "-c", "i=0; while [ $i -lt 20000 ]; do echo 0123456789012345678901234567890123456789012345678901234567890123456789; i=$((i+1)); done")
	if !res.OutputTruncated {
		t.Fatalf("expected truncation, got %d bytes", len(res.Output))
	}
	if len(res.Output) > 70<<10 {
		t.Fatalf("captured output = %d bytes, want the bounded window", len(res.Output))
	}
}

func TestSandboxEnvNeverForwardsSecrets(t *testing.T) {
	t.Setenv("LLM_API_KEY", "sk-live-SECRET-1234567890")
	t.Setenv("PAYPAL_CLIENT_SECRET", "paypal-secret-value")
	env := repair.SandboxEnv(t.TempDir(), "", nil)
	joined := strings.Join(env, "\n")
	for _, needle := range []string{"sk-live-SECRET", "paypal-secret-value", "LLM_API_KEY", "PAYPAL_CLIENT_SECRET"} {
		if strings.Contains(joined, needle) {
			t.Fatalf("sandbox environment leaks %q", needle)
		}
	}
	if !strings.Contains(joined, "GOPROXY=off") || !strings.Contains(joined, "GOFLAGS=-mod=readonly") {
		t.Fatalf("sandbox environment missing supply-chain limits:\n%s", joined)
	}
}
