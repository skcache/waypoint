package repair

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

// boundedBuffer keeps a head and tail window of subprocess output and marks
// truncation, so a runaway candidate cannot flood the report or memory.
type boundedBuffer struct {
	mu      sync.Mutex
	head    []byte
	tail    []byte
	limit   int
	dropped int
}

func newBoundedBuffer(limit int) *boundedBuffer {
	return &boundedBuffer{limit: limit}
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	half := b.limit / 2
	if len(b.head) < half {
		take := half - len(b.head)
		if take > len(p) {
			take = len(p)
		}
		b.head = append(b.head, p[:take]...)
		p = p[take:]
	}
	if len(p) > 0 {
		b.tail = append(b.tail, p...)
		if len(b.tail) > half {
			drop := len(b.tail) - half
			b.tail = b.tail[drop:]
			b.dropped += drop
		}
	}
	return n, nil
}

func (b *boundedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.dropped == 0 {
		return string(b.head) + string(b.tail)
	}
	return string(b.head) +
		fmt.Sprintf("\n... [%d bytes of output omitted] ...\n", b.dropped) +
		string(b.tail)
}

func (b *boundedBuffer) Truncated() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.dropped > 0
}

// RunBounded executes one command in dir with a hard wall-clock bound, a
// process-group kill on timeout (so child compilers/tests die too), capped
// output, and a caller-supplied environment. It never inherits the parent's
// environment: secrets cannot leak into candidate-executed code by accident.
func RunBounded(ctx context.Context, dir string, timeout time.Duration, env []string, name string, args ...string) StepResult {
	started := time.Now()
	res := StepResult{Step: name, Command: append([]string{name}, args...), ExitCode: -1}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	buf := newBoundedBuffer(64 << 10)
	cmd.Stdout = buf
	cmd.Stderr = buf

	if err := cmd.Start(); err != nil {
		res.Output = "failed to start: " + err.Error()
		res.DurationMS = time.Since(started).Milliseconds()
		return res
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				res.ExitCode = ee.ExitCode()
			} else {
				res.Output = err.Error()
			}
		} else {
			res.ExitCode = 0
		}
	case <-runCtx.Done():
		// Kill the whole process group: go test spawns children.
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done
		res.TimedOut = true
		res.ExitCode = -1
	}

	res.DurationMS = time.Since(started).Milliseconds()
	res.Output = buf.String()
	res.OutputTruncated = buf.Truncated()
	return res
}

// redact removes credentials and host paths from captured output before it is
// stored in a report or fed back to the model.
func redact(s, workspace string) string {
	s = redactText(s)
	if workspace != "" {
		s = strings.ReplaceAll(s, workspace, "<workspace>")
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		s = strings.ReplaceAll(s, home, "~")
	}
	s = scrubSecrets(s)
	return s
}

var secretPatterns = []string{
	"sk-", "ghp_", "gho_", "github_pat_", "xoxb-", "xoxp-",
}

// scrubSecrets is intentionally blunt: anything resembling a credential is
// replaced rather than risk it reaching a model prompt or a committed artifact.
func scrubSecrets(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lower := strings.ToLower(line)
		for _, p := range secretPatterns {
			if idx := strings.Index(lower, p); idx >= 0 {
				lines[i] = line[:idx] + "[REDACTED]"
				break
			}
		}
		if idx := strings.Index(lines[i], "Bearer "); idx >= 0 {
			lines[i] = lines[i][:idx] + "Bearer [REDACTED]"
		}
	}
	return strings.Join(lines, "\n")
}
