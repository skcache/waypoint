package repair

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Allowlist is the ONLY writable surface a candidate patch may touch: the
// merchant fixture's implementation file and new helper files in the same
// package. Incident building, the store (durable identity), fault labels, the
// replay harness, the oracle, migrations and every test stay out of reach.
type Allowlist struct {
	// Root is the repo-relative package directory.
	Root string
	// Existing maps repo-relative paths (from Root) of pre-existing files that
	// may be modified.
	Existing map[string]bool
	// MaxEdits bounds a single candidate patch.
	MaxEdits int
	// MaxEditBytes bounds each replacement's size.
	MaxEditBytes int
	// MaxTotalBytes bounds the whole patch payload.
	MaxTotalBytes int
}

// DefaultAllowlist returns the M3 surface.
func DefaultAllowlist() Allowlist {
	return Allowlist{
		Root:          "apps/api/internal/merchant",
		Existing:      map[string]bool{"service.go": true},
		MaxEdits:      4,
		MaxEditBytes:  48 << 10,
		MaxTotalBytes: 96 << 10,
	}
}

// AllowlistForFault narrows the M4 surface to the implementation file that
// actually contains the seeded defect, plus new lowercase helper files in the
// same package. Trusted store, tests, harness, oracle, migrations and verifier
// stay out of reach for every fault.
func AllowlistForFault(fault string) Allowlist {
	a := DefaultAllowlist()
	switch fault {
	case "F2":
		a.Existing["ambiguity.go"] = true
	case "F3":
		a.Existing["webhook.go"] = true
		a.Existing["webhook_store.go"] = true
	}
	return a
}

var newFilePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*\.go$`)

// reserved lists names that must never be created: they would shadow the
// trusted pieces the verifier depends on or the fixture's durable identity
// logic.
var reserved = map[string]bool{
	"store.go":        true,
	"types.go":        true,
	"incident.go":     true,
	"store_test.go":   true,
	"service_test.go": true,
}

// Validate checks a candidate path BEFORE anything is written.
func (a Allowlist) Validate(path string) error {
	if path == "" {
		return fmt.Errorf("repair: empty path")
	}
	if strings.Contains(path, `\`) {
		return fmt.Errorf("repair: path %q contains a backslash", path)
	}
	if filepath.IsAbs(path) {
		return fmt.Errorf("repair: absolute paths are not allowed: %q", path)
	}
	if strings.Contains(path, "..") {
		return fmt.Errorf("repair: path traversal is not allowed: %q", path)
	}
	clean := filepath.ToSlash(filepath.Clean(path))
	if clean != path {
		return fmt.Errorf("repair: path %q is not clean (want %q)", path, clean)
	}
	if !strings.HasPrefix(path, a.Root+"/") {
		return fmt.Errorf("repair: %q is outside the writable surface %q", path, a.Root)
	}
	rel := strings.TrimPrefix(path, a.Root+"/")
	if strings.Contains(rel, "/") {
		return fmt.Errorf("repair: %q is in a subdirectory of the writable package", path)
	}
	if strings.HasSuffix(rel, "_test.go") {
		return fmt.Errorf("repair: tests are outside the writable surface: %q", path)
	}
	if !strings.HasSuffix(rel, ".go") {
		return fmt.Errorf("repair: only .go files are writable: %q", path)
	}
	if a.Existing[rel] {
		return nil
	}
	if reserved[rel] {
		return fmt.Errorf("repair: %q is reserved for the trusted fixture", path)
	}
	if !newFilePattern.MatchString(rel) {
		return fmt.Errorf("repair: new files must be lowercase snake_case .go: %q", rel)
	}
	return nil
}

// ensureNoSymlink rejects symlinked components anywhere under root, so a path
// cannot escape the workspace even if one exists in the base tree.
func ensureNoSymlink(root, rel string) error {
	parts := strings.Split(rel, "/")
	cur := root
	for _, p := range parts {
		cur = filepath.Join(cur, p)
		info, err := os.Lstat(cur)
		if err != nil {
			if os.IsNotExist(err) {
				return nil // remaining components do not exist yet
			}
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("repair: refusing symlinked path component %q", cur)
		}
	}
	return nil
}
