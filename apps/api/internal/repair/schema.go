package repair

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// extractJSON pulls one JSON object out of a model reply. Models sometimes wrap
// JSON in markdown fences; that is tolerated. Prose around the object is not:
// the object must be the only non-whitespace content after the fence is
// stripped, so an injected "also do X" tail cannot ride along unnoticed.
func extractJSON(text string) (string, error) {
	s := strings.TrimSpace(text)
	if strings.HasPrefix(s, "```") {
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			s = s[i+1:]
		}
		if i := strings.LastIndex(s, "```"); i >= 0 {
			s = s[:i]
		}
		s = strings.TrimSpace(s)
	}
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start < 0 || end < 0 || end < start {
		return "", fmt.Errorf("repair: model reply contains no JSON object")
	}
	obj := s[start : end+1]
	if strings.TrimSpace(s[:start]) != "" || strings.TrimSpace(s[end+1:]) != "" {
		return "", fmt.Errorf("repair: model reply carries prose outside the JSON object")
	}
	return obj, nil
}

// decodeStrict enforces the declared schema: unknown fields are rejected rather
// than silently ignored, and trailing content is refused.
func decodeStrict(text string, target any) error {
	obj, err := extractJSON(text)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader([]byte(obj)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return fmt.Errorf("repair: reply does not match the declared schema: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err == nil {
		return fmt.Errorf("repair: reply carries trailing content after the JSON object")
	}
	return nil
}

var knownInvariants = map[string]bool{
	"I1": true, "I2": true, "I3": true, "I4": true, "I5": true, "I6": true,
	"I7": true, "I8": true, "I9": true, "I10": true, "I11": true, "I12": true, "I13": true, "I14": true,
}

// ParseDiagnosis validates the investigator's structured output against the
// evidence it was given. Citations must exist; invented evidence is refused.
func ParseDiagnosis(text string, ev Evidence) (*Diagnosis, error) {
	var d Diagnosis
	if err := decodeStrict(text, &d); err != nil {
		return nil, err
	}
	if strings.TrimSpace(d.RootCause) == "" || strings.TrimSpace(d.Mechanism) == "" {
		return nil, fmt.Errorf("repair: diagnosis must state a root cause and a mechanism")
	}
	if strings.TrimSpace(d.ProposedFix) == "" {
		return nil, fmt.Errorf("repair: diagnosis must propose a fix direction")
	}
	if len(d.SupportingEvidence) == 0 {
		return nil, fmt.Errorf("repair: diagnosis must cite at least one evidence id")
	}
	ids := ev.BlockIDs()
	for _, id := range d.SupportingEvidence {
		if !ids[id] {
			return nil, fmt.Errorf("repair: diagnosis cites evidence id %q that does not exist", id)
		}
	}
	files := map[string]bool{}
	for _, f := range ev.Files {
		files[f.Path] = true
	}
	for _, f := range d.SuspectedFiles {
		if !files[f] {
			return nil, fmt.Errorf("repair: diagnosis names source %q that was not provided", f)
		}
	}
	for _, inv := range d.InvariantsAtRisk {
		if !knownInvariants[inv] {
			return nil, fmt.Errorf("repair: diagnosis names unknown invariant %q", inv)
		}
	}
	return &d, nil
}

// ParsePatch validates the repair proposal's structure. Path-level validation
// happens again at application time, against the actual workspace.
func ParsePatch(text string, allow Allowlist) (*Patch, error) {
	var p Patch
	if err := decodeStrict(text, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Summary) == "" {
		return nil, fmt.Errorf("repair: patch must state a summary")
	}
	if len(p.Edits) == 0 {
		return nil, fmt.Errorf("repair: patch contains no edits")
	}
	if len(p.Edits) > allow.MaxEdits {
		return nil, fmt.Errorf("repair: patch has %d edits, limit is %d", len(p.Edits), allow.MaxEdits)
	}
	for i, e := range p.Edits {
		if err := allow.Validate(e.Path); err != nil {
			return nil, err
		}
		if e.OldText == e.NewText {
			return nil, fmt.Errorf("repair: edit %d does not change anything", i)
		}
		if len(e.OldText) > allow.MaxEditBytes || len(e.NewText) > allow.MaxEditBytes {
			return nil, fmt.Errorf("repair: edit %d exceeds the size limit", i)
		}
	}
	return &p, nil
}
