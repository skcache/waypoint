package repair

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RecordedModel replays model replies that were captured from an earlier
// hosted run. It exists for the deterministic demo and for offline review: it
// never talks to a provider, and every call it returns is labelled
// "recorded:<model>" so a replay can never be mistaken for a live call.
type RecordedModel struct {
	name         string
	recordedBase string
	diagnosis    string
	patch        string
	calls        map[string]CallMeta
}

// NewRecordedModel loads diagnosis-reply.txt and patch-reply.txt from dir. A
// report.json in the same directory supplies the original model name, base
// revision and reported token/latency provenance when present.
func NewRecordedModel(dir string) (*RecordedModel, error) {
	diagnosis, err := readRecordedReply(filepath.Join(dir, "diagnosis-reply.txt"))
	if err != nil {
		return nil, err
	}
	patch, err := readRecordedReply(filepath.Join(dir, "patch-reply.txt"))
	if err != nil {
		return nil, err
	}
	m := &RecordedModel{
		name:      "recorded:unknown-model",
		diagnosis: diagnosis,
		patch:     patch,
		calls:     map[string]CallMeta{},
	}
	raw, err := os.ReadFile(filepath.Join(dir, "report.json"))
	if err == nil {
		var rep struct {
			BaseRevision string     `json:"base_revision"`
			ModelCalls   []CallMeta `json:"model_calls"`
		}
		if json.Unmarshal(raw, &rep) == nil {
			m.recordedBase = rep.BaseRevision
			for _, call := range rep.ModelCalls {
				if call.Purpose == "" {
					continue
				}
				if m.name == "recorded:unknown-model" && call.Model != "" {
					m.name = "recorded:" + call.Model
				}
				m.calls[call.Purpose] = call
			}
		}
	}
	return m, nil
}

func readRecordedReply(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("recorded reply %s: %w", filepath.Base(path), err)
	}
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return "", fmt.Errorf("recorded reply %s is empty", filepath.Base(path))
	}
	return text, nil
}

func (m *RecordedModel) Name() string { return m.name }

// RecordedBase is the revision recorded in the source report, if present.
func (m *RecordedModel) RecordedBase() string { return m.recordedBase }

// Complete returns a previously recorded reply. It never performs I/O outside
// this process and never contacts a model provider.
func (m *RecordedModel) Complete(_ context.Context, purpose, system, user string) (Reply, error) {
	var text string
	switch purpose {
	case "diagnosis":
		text = m.diagnosis
	case "patch":
		text = m.patch
	default:
		return Reply{}, fmt.Errorf("no recorded reply for purpose %q", purpose)
	}
	meta := m.calls[purpose]
	meta.Purpose = purpose
	// Override the source model so the report cannot represent the replay as
	// a fresh hosted call, while retaining the recorded token/latency values.
	meta.Model = m.name
	meta.PromptBytes = len(system) + len(user)
	meta.ResponseBytes = len(text)
	return Reply{Text: text, Meta: meta}, nil
}
