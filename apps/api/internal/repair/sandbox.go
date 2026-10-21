package repair

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Runner executes candidate commands under an OS-enforced boundary. The only
// production implementation is the Docker sandbox: if it cannot be opened, the
// pipeline fails closed — there is deliberately no host fallback.
type Runner interface {
	Run(ctx context.Context, worktree, artifacts string, timeout time.Duration, env map[string]string, name string, args ...string) StepResult
	DSN() string
	Close()
	Describe() string
}

const (
	sandboxImage      = "golang:1.27-alpine"
	sandboxDBImage    = "postgres:17-alpine"
	sandboxMemory     = "2g"
	sandboxCPUs       = "2"
	sandboxPidsLimit  = "256"
	sandboxDBUser     = "waypoint"
	sandboxDBName     = "waypoint"
	sandboxTestDBName = "waypoint_repair_test"
)

// Sandbox is a disposable Docker network + PostgreSQL container plus a private
// Go module cache. Candidate code runs in a one-shot container with:
//
//   - no host home directory and no host credentials (only a READ-ONLY
//     candidate source tree, the artifact directory and a disposable module
//     cache are mounted);
//   - no outbound network (an internal Docker network; the only reachable peer
//     is the disposable database container);
//   - non-root execution (the invoking user's uid/gid, no capabilities,
//     no-new-privileges);
//   - memory, CPU, PID and wall-clock limits;
//   - a read-only root filesystem with tmpfs for the Go build cache.
type Sandbox struct {
	root        string
	modcache    string
	gocache     string
	network     string
	dbContainer string
	dbPassword  string
	image       string
	closed      bool
}

// OpenSandbox verifies Docker is usable, prepares a disposable module cache
// from the pinned module graph, and starts the internal network + database.
// Every failure is returned: callers must not fall back to host execution.
func OpenSandbox(ctx context.Context, repoRoot string) (*Sandbox, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, fmt.Errorf("sandbox unavailable: docker CLI not found (fail closed)")
	}
	info := RunBounded(ctx, repoRoot, 15*time.Second, os.Environ(), "docker", "info", "--format", "{{.ServerVersion}}")
	if !info.Passed() {
		return nil, fmt.Errorf("sandbox unavailable: docker daemon unreachable (fail closed): %s", strings.TrimSpace(info.Output))
	}
	if err := ensureImage(ctx, sandboxImage); err != nil {
		return nil, err
	}
	if err := ensureImage(ctx, sandboxDBImage); err != nil {
		return nil, err
	}

	root, err := os.MkdirTemp("", "waypoint-sandbox-*")
	if err != nil {
		return nil, fmt.Errorf("sandbox: temp root: %w", err)
	}
	s := &Sandbox{
		root:     root,
		modcache: filepath.Join(root, "gomodcache"),
		gocache:  filepath.Join(root, "gocache"),
		image:    sandboxImage,
	}
	cleanup := func() {
		_ = os.RemoveAll(root)
	}

	// Populate the module cache on the HOST with trusted go, from the pinned
	// module graph, so the sandbox needs no network and no host home mount.
	for _, dir := range []string{s.modcache, s.gocache, filepath.Join(root, "home"), filepath.Join(root, "gopath")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			cleanup()
			return nil, fmt.Errorf("sandbox: prepare cache dir: %w", err)
		}
	}
	dl := RunBounded(ctx, filepath.Join(repoRoot, "apps", "api"), 300*time.Second, []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + filepath.Join(root, "home"),
		"GOMODCACHE=" + s.modcache,
		"GOCACHE=" + s.gocache,
		"GOPATH=" + filepath.Join(root, "gopath"),
		"GOFLAGS=-mod=readonly",
		"GOTOOLCHAIN=local",
		"GOPROXY=" + defaultGOPROXY(),
	}, "go", "mod", "download")
	if !dl.Passed() {
		cleanup()
		return nil, fmt.Errorf("sandbox unavailable: module cache prefetch failed (fail closed): %s", redact(dl.Output, root))
	}

	s.network = "wp-net-" + randSuffix()
	if out := RunBounded(ctx, root, 30*time.Second, os.Environ(), "docker", "network", "create", "--internal", s.network); !out.Passed() {
		cleanup()
		return nil, fmt.Errorf("sandbox unavailable: create internal network (fail closed): %s", strings.TrimSpace(out.Output))
	}

	s.dbPassword = randSuffix() + randSuffix()
	s.dbContainer = "wp-db-" + randSuffix()
	dbRun := RunBounded(ctx, root, 120*time.Second, os.Environ(),
		"docker", "run", "-d", "--rm",
		"--name", s.dbContainer,
		"--network", s.network,
		"--tmpfs", "/var/lib/postgresql/data:rw,size=1g",
		"-e", "POSTGRES_USER="+sandboxDBUser,
		"-e", "POSTGRES_PASSWORD="+s.dbPassword,
		"-e", "POSTGRES_DB="+sandboxDBName,
		sandboxDBImage,
	)
	if !dbRun.Passed() {
		s.Close()
		cleanup()
		return nil, fmt.Errorf("sandbox unavailable: start database (fail closed): %s", strings.TrimSpace(dbRun.Output))
	}

	deadline := time.Now().Add(60 * time.Second)
	for {
		check := RunBounded(ctx, root, 10*time.Second, os.Environ(),
			"docker", "exec", s.dbContainer, "pg_isready", "-U", sandboxDBUser, "-d", sandboxDBName)
		if check.Passed() {
			break
		}
		if time.Now().After(deadline) {
			s.Close()
			cleanup()
			return nil, fmt.Errorf("sandbox unavailable: database did not become ready (fail closed)")
		}
		time.Sleep(500 * time.Millisecond)
	}
	return s, nil
}

func defaultGOPROXY() string {
	if p := os.Getenv("GOPROXY"); p != "" {
		return p
	}
	return "https://proxy.golang.org,direct"
}

// DSN is the candidate-visible database URL (container name, internal network).
func (s *Sandbox) DSN() string {
	return fmt.Sprintf("postgres://%s:%s@%s:5432/%s?sslmode=disable",
		sandboxDBUser, s.dbPassword, s.dbContainer, sandboxTestDBName)
}

// Describe summarises the boundary for reports.
func (s *Sandbox) Describe() string {
	return fmt.Sprintf("docker image %s; internal network %s; disposable postgres %s; non-root uid %d; memory %s; cpus %s; pids %s; read-only root",
		s.image, s.network, s.dbContainer, os.Getuid(), sandboxMemory, sandboxCPUs, sandboxPidsLimit)
}

// Run executes one command inside the sandbox. The candidate source tree is
// mounted READ-ONLY at /src: generated code can be compiled and tested, but it
// cannot modify trusted tests, the oracle, the replay harness, the verifier
// commands or any other source file. Writable state is limited to /out,
// /gocache, /tmp and /home.
func (s *Sandbox) Run(ctx context.Context, worktree, artifacts string, timeout time.Duration, env map[string]string, name string, args ...string) StepResult {
	if s.closed {
		return StepResult{Step: name, ExitCode: -1, Output: "sandbox is closed"}
	}
	if err := os.MkdirAll(artifacts, 0o700); err != nil {
		return StepResult{Step: name, ExitCode: -1, Output: err.Error()}
	}
	container := "wp-run-" + randSuffix()
	uid, gid := os.Getuid(), os.Getgid()

	dockerArgs := []string{
		"run", "--rm",
		"--name", container,
		"--network", s.network,
		"--user", fmt.Sprintf("%d:%d", uid, gid),
		"--memory", sandboxMemory, "--memory-swap", sandboxMemory,
		"--cpus", sandboxCPUs,
		"--pids-limit", sandboxPidsLimit,
		"--read-only",
		"--tmpfs", "/tmp:rw,exec,nosuid,size=1g",
		"--tmpfs", "/home:rw,nosuid,size=64m",
		"--cap-drop", "ALL",
		"--security-opt", "no-new-privileges",
		"-v", worktree + ":/src:ro",
		"-v", artifacts + ":/out:rw",
		"-v", s.modcache + ":/gomodcache:rw",
		"-v", s.gocache + ":/gocache:rw",
		"-e", "HOME=/home/sandbox",
		"-e", "TMPDIR=/tmp",
		"-e", "PATH=/usr/local/go/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"-e", "GOCACHE=/gocache",
		"-e", "GOMODCACHE=/gomodcache",
		"-e", "GOPATH=/tmp/gopath",
		"-e", "GOFLAGS=-mod=readonly",
		"-e", "GOPROXY=off",
		"-e", "GOTOOLCHAIN=local",
		"-e", "GOMEMLIMIT=1GiB",
		"-e", "GOMAXPROCS=2",
		"-e", "CGO_ENABLED=0",
		"-w", "/src/apps/api",
	}
	for k, v := range env {
		dockerArgs = append(dockerArgs, "-e", k+"="+v)
	}
	dockerArgs = append(dockerArgs, s.image)
	dockerArgs = append(dockerArgs, name)
	dockerArgs = append(dockerArgs, args...)

	res := RunBounded(ctx, s.root, timeout, os.Environ(), "docker", dockerArgs...)
	if res.TimedOut {
		// The docker CLI is not the container: kill it explicitly.
		_ = exec.Command("docker", "kill", container).Run()
	}
	res.Command = redactCommand(append([]string{name}, args...))
	res.Output = redact(res.Output, s.root)
	return res
}

// Close removes the database, network and disposable caches.
func (s *Sandbox) Close() {
	if s == nil || s.closed {
		return
	}
	s.closed = true
	if s.dbContainer != "" {
		_ = exec.Command("docker", "rm", "-f", s.dbContainer).Run()
	}
	if s.network != "" {
		_ = exec.Command("docker", "network", "rm", s.network).Run()
	}
	if s.root != "" {
		_ = os.RemoveAll(s.root)
	}
}

func ensureImage(ctx context.Context, image string) error {
	inspect := RunBounded(ctx, os.TempDir(), 15*time.Second, os.Environ(), "docker", "image", "inspect", image)
	if inspect.Passed() {
		return nil
	}
	pull := RunBounded(ctx, os.TempDir(), 600*time.Second, os.Environ(), "docker", "pull", image)
	if !pull.Passed() {
		return fmt.Errorf("sandbox unavailable: cannot obtain image %s (fail closed): %s", image, strings.TrimSpace(pull.Output))
	}
	return nil
}

func randSuffix() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}
