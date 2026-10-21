package repair_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/skcache/waypoint/apps/api/internal/repair"
)

// testSandbox is the package-wide sandbox, closed by TestMain.
var testSandbox repair.Runner

func TestMain(m *testing.M) {
	code := m.Run()
	if testSandbox != nil {
		testSandbox.Close()
	}
	os.Exit(code)
}

// sharedSandbox opens ONE Docker sandbox for the package's tests. It is
// intentionally fail-closed: with REQUIRE_DATABASE_TESTS set (the integration
// gate), an unavailable sandbox is a failure, never a skip and never a host
// fallback.
func sharedSandbox(t *testing.T) repair.Runner {
	t.Helper()
	if testSandbox != nil {
		return testSandbox
	}
	sb, err := repair.OpenSandbox(context.Background(), repoRoot(t))
	if err != nil {
		if os.Getenv("REQUIRE_DATABASE_TESTS") != "" {
			t.Fatalf("candidate sandbox is REQUIRED but unavailable (fail closed): %v", err)
		}
		t.Skipf("candidate sandbox unavailable (%v); start Docker to run these tests", err)
	}
	testSandbox = sb
	return sb
}

func probeWorktree(t *testing.T) (worktree, artifacts string) {
	t.Helper()
	worktree = t.TempDir()
	artifacts = t.TempDir()
	if err := os.MkdirAll(filepath.Join(worktree, "apps", "api"), 0o755); err != nil {
		t.Fatalf("prepare probe worktree: %v", err)
	}
	return worktree, artifacts
}

// TestSandboxIsolatesHomeAndCredentials proves candidate code cannot read a
// host-only secret: the host home is not mounted and the host environment is
// not forwarded.
func TestSandboxIsolatesHomeAndCredentials(t *testing.T) {
	sb := sharedSandbox(t)
	worktree, artifacts := probeWorktree(t)

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("host home: %v", err)
	}
	secret := filepath.Join(home, fmt.Sprintf(".waypoint-sandbox-secret-%d", os.Getpid()))
	if err := os.WriteFile(secret, []byte("HOST-ONLY-SECRET-MARKER"), 0o600); err != nil {
		t.Skipf("cannot create the host-only probe secret: %v", err)
	}
	defer os.Remove(secret)
	t.Setenv("WAYPOINT_HOST_ONLY_CANARY", "host-env-value")

	res := sb.Run(context.Background(), worktree, artifacts, 30*time.Second, nil, "sh", "-c",
		"id -u; echo canary=${WAYPOINT_HOST_ONLY_CANARY:-absent}; "+
			"test -e "+secret+" && cat "+secret+" || echo SECRET_BLOCKED")
	if !res.Passed() {
		t.Fatalf("probe failed: exit=%d out=%s", res.ExitCode, res.Output)
	}
	if strings.Contains(res.Output, "HOST-ONLY-SECRET-MARKER") {
		t.Fatalf("candidate read a host-only secret:\n%s", res.Output)
	}
	if !strings.Contains(res.Output, "SECRET_BLOCKED") {
		t.Fatalf("secret probe did not report blocked:\n%s", res.Output)
	}
	if !strings.Contains(res.Output, "canary=absent") {
		t.Fatalf("host environment leaked into the sandbox:\n%s", res.Output)
	}
	uid := strings.TrimSpace(strings.SplitN(res.Output, "\n", 2)[0])
	if uid == "0" {
		t.Fatalf("candidate runs as root in the sandbox")
	}
}

// TestSandboxHasNoOutboundNetwork proves the internal network cannot reach the
// internet, by name or by address.
func TestSandboxHasNoOutboundNetwork(t *testing.T) {
	sb := sharedSandbox(t)
	worktree, artifacts := probeWorktree(t)

	res := sb.Run(context.Background(), worktree, artifacts, 40*time.Second, nil, "sh", "-c",
		"wget -T 3 -qO- http://example.com >/dev/null 2>&1 && echo HTTP_OK || echo HTTP_BLOCKED; "+
			"nc -z -w 3 1.1.1.1 443 && echo IP_OK || echo IP_BLOCKED")
	if !res.Passed() {
		t.Fatalf("network probe failed: exit=%d out=%s", res.ExitCode, res.Output)
	}
	if strings.Contains(res.Output, "HTTP_OK") || strings.Contains(res.Output, "IP_OK") {
		t.Fatalf("candidate reached an unauthorized network destination:\n%s", res.Output)
	}
	if !strings.Contains(res.Output, "HTTP_BLOCKED") || !strings.Contains(res.Output, "IP_BLOCKED") {
		t.Fatalf("network probes did not both report blocked:\n%s", res.Output)
	}
}

// TestSandboxEnforcesResourceLimits proves memory/CPU/PID bounds are actually
// applied by the runtime (cgroup v2 inside the Docker VM).
func TestSandboxEnforcesResourceLimits(t *testing.T) {
	sb := sharedSandbox(t)
	worktree, artifacts := probeWorktree(t)

	res := sb.Run(context.Background(), worktree, artifacts, 20*time.Second, nil, "sh", "-c",
		"cat /sys/fs/cgroup/memory.max 2>/dev/null; cat /sys/fs/cgroup/pids.max 2>/dev/null")
	if !res.Passed() {
		t.Fatalf("limits probe failed: exit=%d out=%s", res.ExitCode, res.Output)
	}
	if !strings.Contains(res.Output, "2147483648") {
		t.Fatalf("memory limit not applied (want 2 GiB), got:\n%s", res.Output)
	}
	if !strings.Contains(res.Output, "256") {
		t.Fatalf("pid limit not applied (want 256), got:\n%s", res.Output)
	}
}

// TestSandboxFailsClosedWhenUnavailable pins the no-fallback rule: a broken
// Docker endpoint must refuse candidate execution rather than run on the host.
func TestSandboxFailsClosedWhenUnavailable(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker CLI absent")
	}
	t.Setenv("DOCKER_HOST", "unix:///nonexistent/waypoint-docker.sock")
	_, err := repair.OpenSandbox(context.Background(), repoRoot(t))
	if err == nil {
		t.Fatal("OpenSandbox succeeded with an unreachable daemon; it must fail closed")
	}
	if !strings.Contains(err.Error(), "fail closed") {
		t.Fatalf("error does not state fail-closed: %v", err)
	}
}

// TestVerifierRefusesWithoutSandbox pins that the verifier itself will not run
// candidate commands when no runner is provided.
func TestVerifierRefusesWithoutSandbox(t *testing.T) {
	v := &repair.Verifier{Fault: "F4", ArtifactDir: t.TempDir(), Changed: []string{"apps/api/internal/merchant/service.go"}}
	out := v.Run(context.Background())
	if len(out.Steps) != 1 || out.Steps[0].Step != "sandbox" || out.Steps[0].Passed() {
		t.Fatalf("verifier without a runner = %+v, want a single failed sandbox step", out.Steps)
	}
}
