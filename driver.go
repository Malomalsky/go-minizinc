package minizinc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
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
	defaultDriver   *Driver
	defaultDriverMu sync.Mutex
)

func DefaultDriver() (*Driver, error) {
	defaultDriverMu.Lock()
	defer defaultDriverMu.Unlock()
	if defaultDriver != nil {
		return defaultDriver, nil
	}
	driver, err := NewDriver("")
	if err != nil {
		return nil, err
	}
	defaultDriver = driver
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
		return nil, fmt.Errorf("%w: %w", ErrDriverNotFound, err)
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

var versionRegexp = regexp.MustCompile(`version (\d+)\.(\d+)\.(\d+)`)

func (d *Driver) detectVersion() error {
	cmd := exec.Command(d.executable, "--version")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return newMinizincError("version", stderr.String(), err)
	}
	out := stdout.Bytes()

	matches := versionRegexp.FindStringSubmatch(string(out))
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
	if d.version == nil {
		return nil
	}
	version := *d.version
	return &version
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
		installCooperativeCancel(cmd, cfg.grace)
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

	runErr := cmd.Run()

	messages, parseErr := parseJSONStream(&stdout)

	if runErr != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, newMinizincError("solve", combineErrorText(messages, stderr.String()), runErr)
	}
	if parseErr != nil {
		return nil, parseErr
	}

	// Even on success, a type=="error" message means something is wrong
	// (e.g. minizinc 0-exit with a model-level error block).
	if text := collectStreamErrors(messages); text != "" {
		return nil, newMinizincError("solve", text, nil)
	}

	return messages, nil
}

func parseJSONStream(r io.Reader) ([]streamMessage, error) {
	var messages []streamMessage
	err := decodeJSONStream(r, func(msg streamMessage) error {
		messages = append(messages, msg)
		return nil
	})
	return messages, err
}

func collectStreamErrors(messages []streamMessage) string {
	var parts []string
	for _, m := range messages {
		if m.Type == "error" {
			label := m.What
			if label == "" {
				label = "error"
			}
			parts = append(parts, fmt.Sprintf("%s: %s", label, m.Message))
		}
	}
	return strings.Join(parts, "\n")
}

func combineErrorText(messages []streamMessage, stderrText string) string {
	streamText := collectStreamErrors(messages)
	switch {
	case streamText == "" && stderrText == "":
		return ""
	case streamText == "":
		return stderrText
	case stderrText == "":
		return streamText
	default:
		return streamText + "\n" + stderrText
	}
}

func decodeJSONStream(r io.Reader, handle func(streamMessage) error) error {
	dec := json.NewDecoder(r)
	dec.UseNumber()
	for {
		var msg streamMessage
		if err := dec.Decode(&msg); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return wrapError("failed to parse JSON stream", err)
		}
		if err := handle(msg); err != nil {
			return err
		}
	}
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

	var streamErrors []streamMessage
	streamErr := decodeJSONStream(stdout, func(msg streamMessage) error {
		if msg.Type == "error" {
			streamErrors = append(streamErrors, msg)
			return nil
		}
		return handle(msg)
	})
	if streamErr != nil {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
		<-stderrDone
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return streamErr
	}

	err = cmd.Wait()
	stderrErr := <-stderrDone
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if stderrErr != nil {
		return wrapError("failed to read stderr", stderrErr)
	}
	if err != nil {
		return newMinizincError("solve", combineErrorText(streamErrors, stderrBuf.String()), err)
	}
	if text := collectStreamErrors(streamErrors); text != "" {
		return newMinizincError("solve", text, nil)
	}

	return nil
}

func (d *Driver) listSolvers(ctx context.Context) ([]Solver, error) {
	d.solversMu.Lock()
	defer d.solversMu.Unlock()

	if d.solversLoaded {
		return cloneSolvers(d.solvers), nil
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

	return cloneSolvers(solvers), nil
}

func cloneSolvers(solvers []Solver) []Solver {
	cloned := slices.Clone(solvers)
	for i := range cloned {
		cloned[i].Tags = slices.Clone(cloned[i].Tags)
		cloned[i].StdFlags = slices.Clone(cloned[i].StdFlags)
		if cloned[i].ExtraFlags != nil {
			cloned[i].ExtraFlags = deepCopyValue(cloned[i].ExtraFlags).([]any)
		}
	}
	return cloned
}

// RefreshSolvers discards the cached solver list so the next call hits
// `minizinc --solvers-json` again.
func (d *Driver) RefreshSolvers() {
	d.solversMu.Lock()
	d.solvers = nil
	d.solversLoaded = false
	d.solversMu.Unlock()
}
