package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/skcache/waypoint/apps/api/internal/intent"
	"github.com/skcache/waypoint/apps/api/internal/llm"
)

const (
	// maxDraftBodyBytes bounds the request body well above any honest draft
	// request and well below anything that could waste model spend.
	maxDraftBodyBytes = 8 << 10

	// draftTimeout bounds one compile call. The llm client carries its own
	// 30s transport timeout, so the client's classification usually wins.
	draftTimeout = 35 * time.Second
)

// draftRequest is the exact accepted request body. Unknown fields are
// rejected — no request may smuggle extra "authority" fields past the
// documented contract.
type draftRequest struct {
	Request string `json:"request"`
}

type modelInfo struct {
	Provider string `json:"provider"`
	Name     string `json:"name"`
}

type draftResponse struct {
	Draft     intent.Draft `json:"draft"`
	Model     modelInfo    `json:"model"`
	RequestID string       `json:"request_id"`
}

// errorBody mirrors the documented error envelope (PRD §8.1).
type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	// TransactionID stays null here: drafts create no transaction.
	TransactionID any  `json:"transaction_id"`
	Retryable     bool `json:"retryable"`
}

type errorEnvelope struct {
	Error     errorBody `json:"error"`
	RequestID string    `json:"request_id"`
}

// writeError renders the documented error envelope. Messages are constant
// per failure class: raw error text (which could embed provider or
// configuration detail) never reaches the client.
func writeError(w http.ResponseWriter, status int, code, message string, retryable bool) {
	writeJSON(w, status, errorEnvelope{
		Error:     errorBody{Code: code, Message: message, Retryable: retryable},
		RequestID: newRequestID(),
	}, "error")
}

// newRequestID returns a short random correlation id for responses.
func newRequestID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// getrandom does not fail on supported platforms; if it somehow
		// does, an unusable correlation id beats a fabricated one.
		return "unavailable"
	}
	return hex.EncodeToString(b[:])
}

// handleIntentDraft implements POST /api/v1/intents/draft (PRD §8): ask the
// configured model to extract a structured, non-authoritative draft mandate.
//
// Phase 06 boundary: this endpoint creates NO spendable state. It persists
// nothing, touches no transaction, and the draft it returns carries
// spendable=false by construction. Session/CSRF hardening of demo write
// endpoints is Phase 07 (P07.1/P07.4); the origin guard here is the Phase 06
// minimum, and the endpoint's only resource cost is one bounded model call.
func (h *handler) handleIntentDraft(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")

	if h.deps.AllowedOrigin != "" {
		if origin := r.Header.Get("Origin"); origin != "" && origin != h.deps.AllowedOrigin {
			writeError(w, http.StatusForbidden, "UNAUTHORIZED", "the request origin is not allowed", false)
			return
		}
	}

	mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mt != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "VALIDATION_ERROR", "Content-Type must be application/json", false)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxDraftBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var in draftRequest
	if err := dec.Decode(&in); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "VALIDATION_ERROR", "request body exceeds the size limit", false)
			return
		}
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", `body must be a single JSON object with a "request" string`, false)
		return
	}
	if _, err := dec.Token(); err != io.EOF {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "unexpected data after the JSON object", false)
		return
	}

	text := strings.TrimSpace(in.Request)
	if err := intent.ValidateRequest(text); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "request must be 1..500 bytes of plain text", false)
		return
	}

	if h.deps.IntentCompiler == nil {
		writeError(w, http.StatusServiceUnavailable, "NOT_CONFIGURED",
			"the intent draft compiler is not configured (set LLM_API_BASE_URL, LLM_API_KEY and LLM_MODEL)", false)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), draftTimeout)
	defer cancel()

	result, err := h.deps.IntentCompiler.Compile(ctx, text)
	if err != nil {
		h.writeCompilerError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, draftResponse{
		Draft:     result.Draft,
		Model:     modelInfo{Provider: result.Provider, Name: result.Model},
		RequestID: newRequestID(),
	}, "intent draft")
}

// writeCompilerError maps compiler failures to the documented envelope:
//
//   - a provider rejection (bad key/model) is PROVIDER_UNAVAILABLE with
//     retryable=false — retrying the same request cannot succeed;
//   - other provider failures (timeout, 5xx, unreadable reply) are
//     PROVIDER_UNAVAILABLE with retryable=true;
//   - a reply that fails the strict draft schema is COMPILATION_FAILED with
//     retryable=true — a fresh model call may produce a valid draft, and the
//     call has no side effects;
//   - anything else is a sanitized INTERNAL_ERROR.
func (h *handler) writeCompilerError(w http.ResponseWriter, err error) {
	var provider *llm.Error
	switch {
	case errors.As(err, &provider):
		slog.Warn("intent draft provider failure", "kind", string(provider.Kind), "status", provider.Status, "detail", provider.Detail)
		message := "the model provider did not return a usable response"
		if provider.Kind == llm.KindRejected {
			message = "the model provider rejected the request; check the configured credentials and model"
		}
		writeError(w, http.StatusBadGateway, "PROVIDER_UNAVAILABLE", message, provider.Retryable())
	case errors.Is(err, intent.ErrInvalidOutput):
		slog.Warn("intent draft rejected by schema", "error", err)
		writeError(w, http.StatusBadGateway, "COMPILATION_FAILED", "the model response could not be validated as a draft", true)
	default:
		slog.Error("intent draft failed", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "the draft could not be compiled", false)
	}
}
