package minizinc

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Driver wraps a discovered MiniZinc executable. A single Driver is safe for
// concurrent use and caches the solver list to avoid re-invoking
// `minizinc --solvers-json` on every call.
type Driver struct {
	executable string
	version    *Version

	solversMu     sync.Mutex
	solvers       []Solver
	solversLoaded bool
}

// Version is the parsed MiniZinc semantic version.
type Version struct {
	Major int
	Minor int
	Patch int
}

func (v *Version) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// AtLeast reports whether v is at least the given semantic version.
func (v *Version) AtLeast(major, minor, patch int) bool {
	if v.Major > major {
		return true
	}
	if v.Major < major {
		return false
	}
	if v.Minor > minor {
		return true
	}
	if v.Minor < minor {
		return false
	}
	return v.Patch >= patch
}

var (
	defaultDriver     *Driver
	defaultDriverErr  error
	defaultDriverOnce sync.Once
)

// DefaultDriver returns a process-wide Driver bound to the first "minizinc"
// found on PATH. Initialization is done at most once.
func DefaultDriver() (*Driver, error) {
	defaultDriverOnce.Do(func() {
		defaultDriver, defaultDriverErr = NewDriver("")
	})
	if defaultDriver == nil {
		if defaultDriverErr != nil {
			return nil, defaultDriverErr
		}
		return nil, ErrDriverNotFound
	}
	return defaultDriver, nil
}

// NewDriver creates a Driver for the MiniZinc executable at path. If path is
// empty, "minizinc" is looked up on PATH. The driver verifies the binary is
// at least version 2.6.0.
func NewDriver(path string) (*Driver, error) {
	if path == "" {
		path = "minizinc"
	}

	execPath, err := exec.LookPath(path)
	if err != nil {
		return nil, wrapError("failed to find minizinc", err)
	}

	d := &Driver{executable: execPath}

	if err := d.detectVersion(); err != nil {
		return nil, err
	}

	if !d.version.AtLeast(2, 6, 0) {
		return nil, ErrInvalidVersion
	}

	return d, nil
}

func (d *Driver) detectVersion() error {
	cmd := exec.Command(d.executable, "--version")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return newMinizincError("version", stderr.String(), err)
	}
	out := stdout.Bytes()

	re := regexp.MustCompile(`version (\d+)\.(\d+)\.(\d+)`)
	matches := re.FindStringSubmatch(string(out))
	if len(matches) != 4 {
		return newError("failed to parse version string")
	}

	major, err := strconv.Atoi(matches[1])
	if err != nil {
		return wrapError("failed to parse major version", err)
	}
	minor, err := strconv.Atoi(matches[2])
	if err != nil {
		return wrapError("failed to parse minor version", err)
	}
	patch, err := strconv.Atoi(matches[3])
	if err != nil {
		return wrapError("failed to parse patch version", err)
	}

	d.version = &Version{
		Major: major,
		Minor: minor,
		Patch: patch,
	}

	return nil
}

// Version returns the detected MiniZinc version.
func (d *Driver) Version() *Version {
	return d.version
}

func (d *Driver) run(ctx context.Context, args []string) (stdout, stderr []byte, err error) {
	cmd := exec.CommandContext(ctx, d.executable, args...)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err = cmd.Run()
	return outBuf.Bytes(), errBuf.Bytes(), err
}

// runConfig carries optional knobs across runJSON/runJSONStream so the call
// sites can stay readable while we feed in things like stdin payloads and the
// cooperative cancel grace period.
type runConfig struct {
	stdin []byte
	grace time.Duration
}

func (d *Driver) newCmd(ctx context.Context, args []string, cfg runConfig) *exec.Cmd {
	cmd := exec.CommandContext(ctx, d.executable, args...)
	if cfg.grace > 0 {
		cmd.Cancel = func() error {
			if cmd.Process != nil {
				return cmd.Process.Signal(syscall.SIGTERM)
			}
			return nil
		}
		cmd.WaitDelay = cfg.grace
	}
	if cfg.stdin != nil {
		cmd.Stdin = bytes.NewReader(cfg.stdin)
	}
	return cmd
}

func (d *Driver) runJSON(ctx context.Context, args []string, cfg runConfig) ([]streamMessage, error) {
	args = append(args, "--json-stream")

	cmd := d.newCmd(ctx, args, cfg)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, newMinizincError("solve", stderr.String(), err)
	}

	var messages []streamMessage
	scanner := bufio.NewScanner(&stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		msg, err := decodeStreamMessageLine(line)
		if err != nil {
			return nil, wrapError("failed to parse JSON stream", err)
		}
		messages = append(messages, msg)
	}

	if err := scanner.Err(); err != nil {
		return nil, wrapError("failed to read output", err)
	}

	return messages, nil
}

func decodeStreamMessageLine(line string) (streamMessage, error) {
	var msg streamMessage
	dec := json.NewDecoder(strings.NewReader(line))
	dec.UseNumber()
	if err := dec.Decode(&msg); err != nil {
		return streamMessage{}, err
	}
	return msg, nil
}

func (d *Driver) runJSONStream(ctx context.Context, args []string, cfg runConfig, handle func(streamMessage) error) error {
	args = append(args, "--json-stream")

	cmd := d.newCmd(ctx, args, cfg)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return wrapError("failed to capture stdout", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return wrapError("failed to capture stderr", err)
	}

	if err := cmd.Start(); err != nil {
		return wrapError("failed to start minizinc", err)
	}

	var stderrBuf bytes.Buffer
	stderrDone := make(chan error, 1)
	go func() {
		_, err := io.Copy(&stderrBuf, stderr)
		stderrDone <- err
	}()

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		msg, err := decodeStreamMessageLine(line)
		if err != nil {
			_ = cmd.Wait()
			<-stderrDone
			return wrapError("failed to parse JSON stream", err)
		}

		if err := handle(msg); err != nil {
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			_ = cmd.Wait()
			<-stderrDone
			return err
		}
	}

	if err := scanner.Err(); err != nil {
		_ = cmd.Wait()
		<-stderrDone
		return wrapError("failed to read output", err)
	}

	err = cmd.Wait()
	<-stderrDone
	if err != nil {
		return newMinizincError("solve", stderrBuf.String(), err)
	}

	return nil
}

func (d *Driver) listSolvers(ctx context.Context) ([]Solver, error) {
	d.solversMu.Lock()
	defer d.solversMu.Unlock()

	if d.solversLoaded {
		return d.solvers, nil
	}

	out, errOut, err := d.run(ctx, []string{"--solvers-json"})
	if err != nil {
		return nil, newMinizincError("list-solvers", string(errOut), err)
	}

	var solvers []Solver
	if err := json.Unmarshal(out, &solvers); err != nil {
		return nil, wrapError("failed to parse solvers JSON", err)
	}

	for i := range solvers {
		solvers[i].driver = d
	}

	d.solvers = solvers
	d.solversLoaded = true

	return solvers, nil
}

// RefreshSolvers discards the cached solver list so the next call hits
// `minizinc --solvers-json` again.
func (d *Driver) RefreshSolvers() {
	d.solversMu.Lock()
	d.solvers = nil
	d.solversLoaded = false
	d.solversMu.Unlock()
}
