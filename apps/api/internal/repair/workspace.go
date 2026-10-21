package repair

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Workspace is a disposable git worktree of the repository at a pinned base
// revision. The developer's active tree is never modified; the candidate's
// compiled code is what the verifier executes.
type Workspace struct {
	Repo    string
	Dir     string
	BaseRef string
	closed  bool
}

// NewWorkspace creates the worktree. baseRef must already exist locally.
func NewWorkspace(ctx context.Context, repoRoot, baseRef string) (*Workspace, error) {
	if repoRoot == "" || baseRef == "" {
		return nil, fmt.Errorf("repair: repo root and base ref are required")
	}
	dir, err := os.MkdirTemp("", "waypoint-candidate-*")
	if err != nil {
		return nil, err
	}
	// git worktree add requires a path that does not exist yet.
	if err := os.Remove(dir); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, "git", "-C", repoRoot, "worktree", "add", "--detach", dir, baseRef)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("repair: create workspace at %s: %w: %s", baseRef, err, strings.TrimSpace(string(out)))
	}
	return &Workspace{Repo: repoRoot, Dir: dir, BaseRef: baseRef}, nil
}

// Close removes the worktree (never the repository).
func (w *Workspace) Close() {
	if w == nil || w.closed {
		return
	}
	w.closed = true
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", w.Repo, "worktree", "remove", "--force", w.Dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		// The directory may already be gone; prune the metadata either way.
		_ = exec.Command("git", "-C", w.Repo, "worktree", "prune").Run()
		if !strings.Contains(string(out), "not a working tree") {
			_ = os.RemoveAll(w.Dir)
		}
	}
	_ = os.RemoveAll(w.Dir)
}

// Path resolves a repo-relative path inside the workspace and refuses anything
// that escapes it.
func (w *Workspace) Path(rel string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(rel))
	if filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
		return "", fmt.Errorf("repair: refusing path outside the workspace: %q", rel)
	}
	full := filepath.Join(w.Dir, clean)
	relCheck, err := filepath.Rel(w.Dir, full)
	if err != nil || strings.HasPrefix(relCheck, "..") {
		return "", fmt.Errorf("repair: refusing path outside the workspace: %q", rel)
	}
	return full, nil
}

// ApplyEdits writes the candidate patch into the workspace. Every edit is
// validated against the allowlist, symlinks, size caps and exact-match
// semantics BEFORE any file is touched; a rejected patch leaves no trace.
func (w *Workspace) ApplyEdits(edits []Edit, allow Allowlist) ([]string, error) {
	if len(edits) == 0 {
		return nil, fmt.Errorf("repair: the patch contains no edits")
	}
	if len(edits) > allow.MaxEdits {
		return nil, fmt.Errorf("repair: patch has %d edits, limit is %d", len(edits), allow.MaxEdits)
	}
	total := 0
	for i, e := range edits {
		if err := allow.Validate(e.Path); err != nil {
			return nil, err
		}
		if len(e.NewText) > allow.MaxEditBytes {
			return nil, fmt.Errorf("repair: edit %d exceeds %d bytes", i, allow.MaxEditBytes)
		}
		total += len(e.OldText) + len(e.NewText)
	}
	if total > allow.MaxTotalBytes {
		return nil, fmt.Errorf("repair: patch payload exceeds %d bytes", allow.MaxTotalBytes)
	}

	changed := make([]string, 0, len(edits))
	for _, e := range edits {
		full, err := w.Path(e.Path)
		if err != nil {
			return nil, err
		}
		if err := ensureNoSymlink(w.Dir, e.Path); err != nil {
			return nil, err
		}
		info, err := os.Lstat(full)
		exists := err == nil
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		if exists && info.IsDir() {
			return nil, fmt.Errorf("repair: %q is a directory", e.Path)
		}

		if !exists {
			if e.OldText != "" {
				return nil, fmt.Errorf("repair: %q does not exist, so old_text must be empty", e.Path)
			}
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				return nil, err
			}
			if err := os.WriteFile(full, []byte(e.NewText), 0o644); err != nil {
				return nil, err
			}
			changed = append(changed, e.Path)
			continue
		}

		current, err := os.ReadFile(full)
		if err != nil {
			return nil, err
		}
		occurrences := strings.Count(string(current), e.OldText)
		if e.OldText == "" || occurrences != 1 {
			return nil, fmt.Errorf("repair: old_text for %q must match exactly once (matched %d times)", e.Path, occurrences)
		}
		updated := strings.Replace(string(current), e.OldText, e.NewText, 1)
		if err := os.WriteFile(full, []byte(updated), info.Mode().Perm()); err != nil {
			return nil, err
		}
		changed = append(changed, e.Path)
	}

	// Belt and braces: the workspace must show exactly the files we wrote and
	// nothing else (no hidden side effects from earlier steps).
	status, err := w.StatusPorcelain()
	if err != nil {
		return nil, err
	}
	allowed := map[string]bool{}
	for _, c := range changed {
		allowed[c] = true
	}
	for _, line := range strings.Split(status, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		if len(line) < 4 {
			return nil, fmt.Errorf("repair: unreadable workspace status line %q", line)
		}
		// Porcelain v1: two status columns, one space, then the path. The
		// leading column may itself be a space, so never trim the whole
		// output before splitting (it would shift the first line's path).
		path := strings.TrimSpace(line[3:])
		if strings.Contains(path, " -> ") {
			return nil, fmt.Errorf("repair: renames are outside the writable surface: %q", line)
		}
		if strings.HasPrefix(line, " D") || strings.HasPrefix(line, "D ") {
			return nil, fmt.Errorf("repair: deletions are outside the writable surface: %q", line)
		}
		if !allowed[path] {
			return nil, fmt.Errorf("repair: workspace changed unexpected file %q", path)
		}
	}
	// Two edits may target the same file; report it once.
	seen := map[string]bool{}
	unique := changed[:0]
	for _, c := range changed {
		if !seen[c] {
			seen[c] = true
			unique = append(unique, c)
		}
	}
	return unique, nil
}

// StatusPorcelain returns `git status --porcelain`.
func (w *Workspace) StatusPorcelain() (string, error) {
	cmd := exec.Command("git", "-C", w.Dir, "status", "--porcelain")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("repair: workspace status: %w", err)
	}
	return string(out), nil
}

// Diff stages the candidate change and returns a real unified diff, bounded in
// size. The diff is the reviewable artifact.
func (w *Workspace) Diff() (string, error) {
	if err := exec.Command("git", "-C", w.Dir, "add", "-A").Run(); err != nil {
		return "", fmt.Errorf("repair: stage candidate diff: %w", err)
	}
	out, err := exec.Command("git", "-C", w.Dir, "diff", "--cached", "--no-color", "--no-ext-diff").Output()
	if err != nil {
		return "", fmt.Errorf("repair: render candidate diff: %w", err)
	}
	const max = 256 << 10
	if len(out) > max {
		out = append(out[:max], []byte("\n... [diff truncated] ...\n")...)
	}
	return string(out), nil
}

// VerifyIsolation proves the developer's tree is untouched: the workspace has
// its own HEAD and the repository's status shows no candidate paths.
func (w *Workspace) VerifyIsolation(repoStatusBefore string) error {
	after, err := RepoStatus(w.Repo)
	if err != nil {
		return err
	}
	if after != repoStatusBefore {
		return fmt.Errorf("repair: the developer's working tree changed during the run")
	}
	return nil
}

// RepoStatus returns the repository's porcelain status (for before/after
// comparisons).
func RepoStatus(repo string) (string, error) {
	out, err := exec.Command("git", "-C", repo, "status", "--porcelain").Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}
