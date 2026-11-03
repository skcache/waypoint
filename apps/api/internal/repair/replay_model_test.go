package repair_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/skcache/waypoint/apps/api/internal/repair"
)

func TestRecordedModelReplaysRepliesAndLabelsProvenance(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("diagnosis-reply.txt", `{"root_cause":"recorded diagnosis"}`)
	write("patch-reply.txt", `{"summary":"recorded patch","edits":[]}`)
	write("report.json", `{
  "base_revision": "abc123",
  "model_calls": [
    {"purpose":"diagnosis","model":"deepseek/deepseek-v4.1-flash","latency_ms":1234,"prompt_tokens":100,"completion_tokens":20,"usage_reported":true},
    {"purpose":"patch","model":"deepseek/deepseek-v4.1-flash","latency_ms":2345,"prompt_tokens":80,"completion_tokens":30,"usage_reported":true}
  ]
}`)

	model, err := repair.NewRecordedModel(dir)
	if err != nil {
		t.Fatal(err)
	}
	if model.Name() != "recorded:deepseek/deepseek-v4.1-flash" {
		t.Fatalf("name = %q", model.Name())
	}
	if model.RecordedBase() != "abc123" {
		t.Fatalf("recorded base = %q", model.RecordedBase())
	}
	reply, err := model.Complete(context.Background(), "diagnosis", "system", "user")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply.Text, "recorded diagnosis") {
		t.Fatalf("text = %q", reply.Text)
	}
	if reply.Meta.Model != model.Name() || reply.Meta.PromptTokens != 100 || reply.Meta.CompletionTokens != 20 {
		t.Fatalf("meta = %+v", reply.Meta)
	}
	if _, err := model.Complete(context.Background(), "unknown", "", ""); err == nil {
		t.Fatal("expected an error for an unknown purpose")
	}
}

func TestRecordedModelRejectsMissingReply(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "diagnosis-reply.txt"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := repair.NewRecordedModel(dir); err == nil {
		t.Fatal("expected an error when patch-reply.txt is missing")
	}
}
