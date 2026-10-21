package repair

import (
	"context"
	"time"

	"github.com/skcache/waypoint/apps/api/internal/llm"
)

// Reply is one model answer with its measured metadata.
type Reply struct {
	Text string
	Meta CallMeta
}

// Model is the narrow model boundary the pipeline needs. Tests inject a
// scripted boundary double to exercise the pipeline; production injects the
// hosted adapter. Nothing else in the package knows which is in use — but the
// report always records the model name and usage, so a boundary test can never
// masquerade as a hosted run.
type Model interface {
	Complete(ctx context.Context, purpose, system, user string) (Reply, error)
	Name() string
}

// LLMModel adapts the existing hosted chat-completions client.
type LLMModel struct {
	client *llm.Client
	name   string
}

// NewLLMModel builds the hosted adapter. Missing configuration returns
// llm.ErrNotConfigured, which callers surface as NOT_CONFIGURED.
func NewLLMModel(cfg llm.Config) (*LLMModel, error) {
	client, err := llm.New(cfg)
	if err != nil {
		return nil, err
	}
	return &LLMModel{client: client, name: cfg.Model}, nil
}

func (m *LLMModel) Name() string { return m.name }

// Complete performs one bounded model call and records latency and usage.
func (m *LLMModel) Complete(ctx context.Context, purpose, system, user string) (Reply, error) {
	started := time.Now()
	out, err := m.client.GenerateDetail(ctx, system, user)
	meta := CallMeta{
		Purpose:          purpose,
		Model:            m.name,
		LatencyMS:        time.Since(started).Milliseconds(),
		PromptBytes:      len(system) + len(user),
		ResponseBytes:    len(out.Text),
		PromptTokens:     out.PromptTokens,
		CompletionTokens: out.CompletionTokens,
		UsageReported:    out.UsageReported,
	}
	return Reply{Text: out.Text, Meta: meta}, err
}
