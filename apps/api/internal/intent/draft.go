// Package intent compiles a natural-language purchase request into a
// reviewable, NON-AUTHORITATIVE intent draft (PRD Phase 06, §9.1).
//
// The draft is not authority. Nothing in this package can make a purchase
// spendable: a draft carries no confirmation, no contract version and no
// canonical hash, and the runtime's payment eligibility will bind only to
// human-confirmed contracts (Phase 07) and deterministic policy (Phase 09).
//
// The compiler treats the model as a fallible extractor: its reply is parsed
// under a strict schema, every value is range-checked, and any deviation is
// an error — never a guessed draft and never a fabricated success. Terms the
// model could not extract stay null and are listed in unresolved_fields for
// the human to fill in (PRD §9.1: "Missing critical field -> ask user for
// explicit input or mark unresolved").
package intent

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/skcache/waypoint/apps/api/internal/domain"
)

const (
	// DraftStatusDraft is the only status a draft ever has. Confirmed state
	// exists only on an intent contract, never on a draft.
	DraftStatusDraft = "DRAFT"

	// MaxRequestTextLen bounds the natural-language request (bytes). Short
	// caps keep model cost predictable and the prompt small.
	MaxRequestTextLen = 500

	// Field caps mirror the system prompt exactly, so a compliant model can
	// never trip the validator and a non-compliant one is always rejected.
	maxPreferences   = 8
	maxPreferenceLen = 120
	maxAssumptions   = 5
	maxAssumptionLen = 200
	maxCategoryLen   = 64
)

// Terms is the extracted, non-authoritative mandate content. Every field is
// optional at draft time: null/absent means the model could not extract it
// and the human must supply it before any confirmation.
type Terms struct {
	Currency               *string  `json:"currency"`
	MaxTotalMinor          *int64   `json:"max_total_minor"`
	MaxQuantity            *int     `json:"max_quantity"`
	Category               *string  `json:"category"`
	QualitativePreferences []string `json:"qualitative_preferences"`
}

// Draft is the reviewable interpretation of a purchase request. It is
// deliberately incapable of granting spend authority: spendable is always
// false, status is always DRAFT, and no confirmation fields exist. Only a
// human-confirmed intent contract can authorize payment.
type Draft struct {
	Status           string   `json:"status"`
	Spendable        bool     `json:"spendable"`
	OriginalRequest  string   `json:"original_request"`
	Terms            Terms    `json:"terms"`
	UnresolvedFields []string `json:"unresolved_fields"`
	Assumptions      []string `json:"assumptions"`
}

// ErrInvalidOutput marks a model reply that is not a valid draft under the
// strict schema. It is surfaced to callers — never silently coerced into a
// draft — and reported as COMPILATION_FAILED.
var ErrInvalidOutput = errors.New("intent: model output is not a valid draft")

func invalidOutput(detail string) error {
	return fmt.Errorf("%w: %s", ErrInvalidOutput, detail)
}

// ValidateRequest checks a natural-language request before any model call.
// It is shared by the compiler and the HTTP layer so both enforce the same
// rule (defense in depth — the HTTP layer never lets an invalid request
// through, and the compiler still refuses one it is handed directly).
func ValidateRequest(request string) error {
	if !utf8.ValidString(request) {
		return errors.New("intent: request must be valid UTF-8")
	}
	if request == "" || strings.TrimSpace(request) == "" {
		return errors.New("intent: request is empty")
	}
	if len(request) > MaxRequestTextLen {
		return fmt.Errorf("intent: request exceeds %d bytes", MaxRequestTextLen)
	}
	for _, r := range request {
		if (r < 0x20 && r != '\n' && r != '\t') || r == 0x7f {
			return errors.New("intent: request contains unsupported control characters")
		}
	}
	return nil
}

// modelDraft is the exact JSON shape the model is instructed to produce.
// Decoding is strict: unknown fields, wrong types and malformed JSON are all
// rejected (DisallowUnknownFields plus explicit per-field checks below).
// Numeric fields are raw JSON so a quoted number ("15000") is a schema
// violation, not a coercion opportunity — json.Number alone would accept it.
type modelDraft struct {
	Currency               *string          `json:"currency"`
	MaxTotalMinor          *json.RawMessage `json:"max_total_minor"`
	MaxQuantity            *json.RawMessage `json:"max_quantity"`
	Category               *string          `json:"category"`
	QualitativePreferences []string         `json:"qualitative_preferences"`
	Assumptions            []string         `json:"assumptions"`
}

// parseModelDraft validates the model's JSON object and derives the draft.
// Absent, null and empty-list values fold into the same unresolved/empty
// states; every other deviation is an error. Error details describe the
// schema rule only — model content is never echoed into errors.
func parseModelDraft(request, raw string) (Draft, error) {
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	var md modelDraft
	if err := dec.Decode(&md); err != nil {
		return Draft{}, invalidOutput(classifyDecodeError(err))
	}
	if _, err := dec.Token(); err != io.EOF {
		return Draft{}, invalidOutput("unexpected data after the JSON object")
	}

	terms := Terms{}
	var unresolved []string

	if md.Currency == nil {
		unresolved = append(unresolved, "currency")
	} else {
		if *md.Currency != string(domain.CurrencyUSD) {
			return Draft{}, invalidOutput("currency must be USD; this release supports USD only")
		}
		currency := *md.Currency
		terms.Currency = &currency
	}

	ceiling, err := integerField(md.MaxTotalMinor, "max_total_minor", 1, int64(domain.MaxAmountMinor))
	if err != nil {
		return Draft{}, err
	}
	if ceiling == nil {
		unresolved = append(unresolved, "max_total_minor")
	} else {
		terms.MaxTotalMinor = ceiling
	}

	quantity, err := integerField(md.MaxQuantity, "max_quantity", 1, int64(domain.MaxQuantity))
	if err != nil {
		return Draft{}, err
	}
	if quantity == nil {
		unresolved = append(unresolved, "max_quantity")
	} else {
		q := int(*quantity)
		terms.MaxQuantity = &q
	}

	if md.Category == nil {
		unresolved = append(unresolved, "category")
	} else {
		category, err := printableField(*md.Category, "category", maxCategoryLen)
		if err != nil {
			return Draft{}, err
		}
		terms.Category = &category
	}

	preferences, err := stringList(md.QualitativePreferences, "qualitative_preferences", maxPreferences, maxPreferenceLen)
	if err != nil {
		return Draft{}, err
	}
	terms.QualitativePreferences = preferences

	assumptions, err := stringList(md.Assumptions, "assumptions", maxAssumptions, maxAssumptionLen)
	if err != nil {
		return Draft{}, err
	}

	if unresolved == nil {
		unresolved = []string{}
	}
	return Draft{
		Status:           DraftStatusDraft,
		Spendable:        false,
		OriginalRequest:  request,
		Terms:            terms,
		UnresolvedFields: unresolved,
		Assumptions:      assumptions,
	}, nil
}

// classifyDecodeError maps JSON decoder failures to structural descriptions
// that never carry raw model content.
func classifyDecodeError(err error) string {
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		return fmt.Sprintf("field %q has the wrong JSON type", typeErr.Field)
	}
	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) {
		return "the draft JSON is not well-formed"
	}
	if strings.Contains(err.Error(), "unknown field") {
		return "the draft contains fields outside the schema"
	}
	return "the JSON does not match the draft schema"
}

// integerField decodes an optional integer expressed as a JSON number in
// minor units. Only plain digits are accepted: fractions ("15000.5"),
// exponents, signs, JSON strings ("15000"), booleans, arrays and
// out-of-range values are all rejected. An absent or null field decodes to a
// nil pointer before this function is reached.
func integerField(raw *json.RawMessage, name string, min, max int64) (*int64, error) {
	if raw == nil {
		return nil, nil
	}
	s := string(*raw)
	if s == "" {
		return nil, invalidOutput(name + " must be an integer")
	}
	if s[0] == '"' {
		return nil, invalidOutput(name + " must be a JSON integer, not a string")
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return nil, invalidOutput(name + " must be a whole number of minor units (digits only)")
		}
	}
	var v int64
	for i := 0; i < len(s); i++ {
		d := int64(s[i] - '0')
		if v > (1<<62)/10 {
			return nil, invalidOutput(name + " is out of range")
		}
		v = v*10 + d
	}
	if v < min || v > max {
		return nil, invalidOutput(fmt.Sprintf("%s is outside the supported range %d..%d", name, min, max))
	}
	return &v, nil
}

// printableField validates a short printable-ASCII string with no
// surrounding whitespace.
func printableField(value, name string, maxLen int) (string, error) {
	if value == "" || len(value) > maxLen {
		return "", invalidOutput(fmt.Sprintf("%s must be 1..%d characters", name, maxLen))
	}
	if strings.TrimSpace(value) != value {
		return "", invalidOutput(name + " must not have surrounding whitespace")
	}
	for i := 0; i < len(value); i++ {
		if value[i] < 0x20 || value[i] > 0x7e {
			return "", invalidOutput(name + " contains characters outside printable ASCII")
		}
	}
	return value, nil
}

// stringList validates a bounded list of short printable strings. A missing
// or null list folds to an empty list.
func stringList(values []string, name string, maxItems, maxLen int) ([]string, error) {
	if len(values) > maxItems {
		return nil, invalidOutput(fmt.Sprintf("%s exceeds %d items", name, maxItems))
	}
	out := make([]string, 0, len(values))
	for _, v := range values {
		checked, err := printableField(v, name+" item", maxLen)
		if err != nil {
			return nil, err
		}
		out = append(out, checked)
	}
	return out, nil
}
