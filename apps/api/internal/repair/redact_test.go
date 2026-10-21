package repair_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/skcache/waypoint/apps/api/internal/repair"
)

func TestRedactCommandHandlesDSNFlagsAndURLs(t *testing.T) {
	cases := []struct {
		in   []string
		want []string
	}{
		{[]string{"go", "run", "./cmd/repaircheck", "-dsn", "postgres://u:p@h:5432/db"}, []string{"go", "run", "./cmd/repaircheck", "-dsn", "[REDACTED]"}},
		{[]string{"cmd", "--dsn=postgres://u:p@h/db"}, []string{"cmd", "--dsn=[REDACTED]"}},
		{[]string{"cmd", "-dsn", "host=db user=u password=topsecret"}, []string{"cmd", "-dsn", "[REDACTED]"}},
		{[]string{"curl", "-H", "Authorization: Bearer abc123"}, []string{"curl", "-H", "Authorization: Bearer [REDACTED]"}},
	}
	for _, c := range cases {
		got := repair.RedactCommandForTest(c.in)
		if strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Errorf("redact(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestReportRedactsCredentialsEverywhere(t *testing.T) {
	canaryDSN := "postgres://waypoint:canary-password-123@db:5432/x?sslmode=disable"
	canaryToken := "canarytoken-abcdef"
	t.Setenv("TEST_DATABASE_URL", canaryDSN)

	rep := &repair.Report{
		SchemaVersion: 1,
		IncidentID:    "M3-INC-TEST",
		Fault:         "F4",
		Mode:          "oracle",
		EvidenceTier:  "F",
		BaseRevision:  "deadbeef",
		Status:        repair.StatusNotVerified,
		Limitations:   []string{"test"},
		Attempts: []repair.Attempt{{
			Number: 1,
			Steps: []repair.StepResult{{
				Step:    "eval",
				Command: []string{"go", "run", "./cmd/repaircheck", "-out", "/out", "-dsn", canaryDSN},
				Output:  "connecting to " + canaryDSN + "\nAuthorization: Bearer " + canaryToken + "\npassword=canary-password-123",
			}},
			Feedback: "retry with " + canaryDSN,
		}},
	}
	out := t.TempDir()
	if err := repair.WriteReport(rep, out); err != nil {
		t.Fatalf("write report: %v", err)
	}
	var all strings.Builder
	err := filepath.Walk(out, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return err
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		if strings.Contains(string(data), "canary-password-123") {
			t.Logf("LEAK IN %s", path)
		}
		all.WriteString(string(data))
		return nil
	})
	if err != nil {
		t.Fatalf("walk artifacts: %v", err)
	}
	rendered := all.String()
	for _, canary := range []string{"canary-password-123", canaryToken, canaryDSN} {
		if strings.Contains(rendered, canary) {
			t.Fatalf("artifact leaked credential %q", canary)
		}
	}
	if !strings.Contains(rendered, "[REDACTED]") {
		t.Fatalf("artifacts do not show redaction markers:\n%s", rendered[:min(800, len(rendered))])
	}
	if !strings.Contains(rendered, "repaircheck") {
		t.Fatalf("redaction destroyed the reviewable command shape")
	}
}

// TestSandboxReceivesTheRealDSNButRedactsItsOutput proves the verifier's
// process still gets the credential while the persisted output does not.
func TestSandboxReceivesTheRealDSNButRedactsItsOutput(t *testing.T) {
	sb := sharedSandbox(t)
	canary := "postgres://waypoint:runtime-canary-pw@db:5432/x?sslmode=disable"
	worktree, artifacts := probeWorktree(t)

	res := sb.Run(context.Background(), worktree, artifacts, 30*time.Second,
		map[string]string{"TEST_DATABASE_URL": canary}, "sh", "-c",
		"test -n \"$TEST_DATABASE_URL\" && echo RECEIVED; printf '%s\\n' \"$TEST_DATABASE_URL\"")
	if !res.Passed() {
		t.Fatalf("probe: exit=%d out=%s", res.ExitCode, res.Output)
	}
	if !strings.Contains(res.Output, "RECEIVED") {
		t.Fatalf("the process did not receive the DSN:\n%s", res.Output)
	}
	if strings.Contains(res.Output, "runtime-canary-pw") || strings.Contains(res.Output, canary) {
		t.Fatalf("sandbox output leaked the DSN:\n%s", res.Output)
	}
	if !strings.Contains(res.Output, "REDACTED") {
		t.Fatalf("sandbox output carries no redaction marker:\n%s", res.Output)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
