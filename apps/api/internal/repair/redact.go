package repair

import (
	"net/url"
	"os"
	"regexp"
	"strings"
)

// Credential handling: the verifier needs the real DSN internally, but no
// persisted artifact — StepResult.Command, JSON/Markdown reports, step logs, or
// model feedback — may contain a connection string, password, authorization
// header, or a secret-bearing CLI argument. Redaction is structural (URL
// parsing, argument names) with exact-value scrubbing as defense in depth, not
// a single substring heuristic.

var (
	postgresURLPattern = regexp.MustCompile(`(?i)\bpostgres(?:ql)?://[^\s"'<>\\]+`)
	bearerPattern      = regexp.MustCompile(`(?i)(authorization\s*:\s*bearer\s+)[^\s"']+`)
	passwordKVPattern  = regexp.MustCompile(`(?i)(\bpassword\s*=\s*)[^\s&"']+`)
	dsnFlagNames       = map[string]bool{
		"-dsn": true, "--dsn": true, "-database-url": true, "--database-url": true,
		"-databaseurl": true, "--databaseurl": true,
	}
)

const redactedValue = "[REDACTED]"

// redactPostgresURL rebuilds a PostgreSQL URL with the password removed while
// keeping the shape (scheme, user, host, database, options) reviewable.
func redactPostgresURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "[REDACTED-DSN]"
	}
	if u.User != nil {
		user := u.User.Username()
		if _, has := u.User.Password(); has {
			// No brackets: url.String would percent-encode them.
			u.User = url.UserPassword(user, "REDACTED")
		}
	}
	q := u.Query()
	for _, key := range []string{"password", "passfile", "sslpassword"} {
		if q.Has(key) {
			q.Set(key, redactedValue)
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// redactText removes credentials from free text (outputs, feedback, JSON).
func redactText(s string) string {
	s = postgresURLPattern.ReplaceAllStringFunc(s, redactPostgresURL)
	s = bearerPattern.ReplaceAllString(s, "${1}"+redactedValue)
	s = passwordKVPattern.ReplaceAllString(s, "${1}"+redactedValue)
	// Exact configured values, in case a secret appears without its usual
	// syntax (defense in depth; never the only mechanism).
	for _, key := range []string{
		"LLM_API_KEY", "OPENROUTER_API_KEY", "PAYPAL_CLIENT_SECRET", "DATABASE_URL", "TEST_DATABASE_URL",
	} {
		if v := os.Getenv(key); len(v) >= 8 {
			s = strings.ReplaceAll(s, v, redactedValue)
		}
	}
	return s
}

// sanitizeReport redacts every string a report can persist, before it is
// serialized. Serialized redaction is kept as defense in depth, but the
// structural pass is what handles fields correctly.
func sanitizeReport(r *Report) {
	for i := range r.Attempts {
		a := &r.Attempts[i]
		a.Error = redactText(a.Error)
		a.Feedback = redactText(a.Feedback)
		a.Diff = redactText(a.Diff)
		for j := range a.Steps {
			a.Steps[j].Command = redactCommand(a.Steps[j].Command)
			a.Steps[j].Output = redactText(a.Steps[j].Output)
		}
		if a.Eval != nil {
			a.Eval.Reason = redactText(a.Eval.Reason)
			for k := range a.Eval.FailingInvariants {
				a.Eval.FailingInvariants[k] = redactText(a.Eval.FailingInvariants[k])
			}
		}
		if a.Patch != nil {
			for k := range a.Patch.Edits {
				a.Patch.Edits[k].NewText = redactText(a.Patch.Edits[k].NewText)
				a.Patch.Edits[k].OldText = redactText(a.Patch.Edits[k].OldText)
				a.Patch.Edits[k].Rationale = redactText(a.Patch.Edits[k].Rationale)
			}
		}
	}
	if r.Diagnosis != nil {
		d := r.Diagnosis
		d.RootCause = redactText(d.RootCause)
		d.Mechanism = redactText(d.Mechanism)
		d.ProposedFix = redactText(d.ProposedFix)
		for k := range d.ConfidenceReasons {
			d.ConfidenceReasons[k] = redactText(d.ConfidenceReasons[k])
		}
		for k := range d.Uncertainties {
			d.Uncertainties[k] = redactText(d.Uncertainties[k])
		}
	}
	for i := range r.Limitations {
		r.Limitations[i] = redactText(r.Limitations[i])
	}
}

// RedactTextForTest exposes the text redactor to external tests.
func RedactTextForTest(s string) string { return redactText(s) }

// RedactCommandForTest exposes the command redactor to external tests.
func RedactCommandForTest(argv []string) []string { return redactCommand(argv) }

// redactCommand removes secrets from an argv before it is persisted. The
// argument AFTER a DSN flag is replaced regardless of its shape, so a DSN that
// does not parse as a URL still cannot leak.
func redactCommand(argv []string) []string {
	out := make([]string, len(argv))
	redactNext := false
	for i, arg := range argv {
		if redactNext {
			out[i] = redactedValue
			redactNext = false
			continue
		}
		lower := strings.ToLower(arg)
		if dsnFlagNames[lower] {
			out[i] = arg
			redactNext = true
			continue
		}
		if idx := strings.IndexByte(lower, '='); idx > 0 && dsnFlagNames[lower[:idx]] {
			out[i] = arg[:idx+1] + redactedValue
			continue
		}
		out[i] = redactText(arg)
	}
	return out
}
