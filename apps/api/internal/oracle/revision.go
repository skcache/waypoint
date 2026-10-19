package oracle

import (
	"os/exec"
	"runtime/debug"
	"strings"
)

// DetectRevision returns the revision of the code actually being evaluated,
// plus how it was determined. The value is NOT caller-supplied: it prefers the
// VCS stamp embedded by the Go toolchain into the running binary (the truth
// about the built source tree) and falls back to asking git in the current
// directory. A modified working tree is reported with a "-dirty" suffix so a
// bundle can never silently claim a clean revision it did not evaluate.
func DetectRevision() (revision, source string) {
	if info, ok := debug.ReadBuildInfo(); ok {
		var rev string
		modified := false
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				rev = s.Value
			case "vcs.modified":
				modified = s.Value == "true"
			}
		}
		if rev != "" {
			if modified {
				rev += "-dirty"
			}
			return rev, "go-buildinfo"
		}
	}
	if out, err := exec.Command("git", "rev-parse", "HEAD").Output(); err == nil {
		rev := strings.TrimSpace(string(out))
		if rev != "" {
			if st, err := exec.Command("git", "status", "--porcelain").Output(); err == nil && len(strings.TrimSpace(string(st))) > 0 {
				rev += "-dirty"
			}
			return rev, "git"
		}
	}
	return "unknown", "unknown"
}
