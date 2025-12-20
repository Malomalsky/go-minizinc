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
)

type Driver struct {
	executable string
	version    *Version
}

type Version struct {
	Major int
	Minor int
	Patch int
}

func (v *Version) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

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
	out, err := exec.Command(d.executable, "--version").Output()
	if err != nil {
		return wrapError("failed to get minizinc version", err)
	}

	re := regexp.MustCompile(`version (\d+)\.(\d+)\.(\d+)`)
	matches := re.FindStringSubmatch(string(out))
	if len(matches) != 4 {
		return newError("failed to parse version string")
	}

	major, _ := strconv.Atoi(matches[1])
	minor, _ := strconv.Atoi(matches[2])
	patch, _ := strconv.Atoi(matches[3])

	d.version = &Version{
		Major: major,
		Minor: minor,
		Patch: patch,
	}

	return nil
}

func (d *Driver) Version() *Version {
	return d.version
}

func (d *Driver) run(ctx context.Context, args []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, d.executable, args...)
	return cmd.CombinedOutput()
}

func (d *Driver) runJSON(ctx context.Context, args []string) ([]streamMessage, error) {
	args = append(args, "--json-stream")

	cmd := exec.CommandContext(ctx, d.executable, args...)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if stderr.Len() > 0 {
			return nil, wrapError(stderr.String(), err)
		}
		return nil, wrapError("minizinc execution failed", err)
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

func (d *Driver) runJSONStream(ctx context.Context, args []string, handle func(streamMessage) error) error {
	args = append(args, "--json-stream")

	cmd := exec.CommandContext(ctx, d.executable, args...)

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
		if stderrBuf.Len() > 0 {
			return wrapError(stderrBuf.String(), err)
		}
		return wrapError("minizinc execution failed", err)
	}

	return nil
}

func (d *Driver) listSolvers(ctx context.Context) ([]Solver, error) {
	out, err := d.run(ctx, []string{"--solvers-json"})
	if err != nil {
		return nil, wrapError("failed to list solvers", err)
	}

	var solvers []Solver
	if err := json.Unmarshal(out, &solvers); err != nil {
		return nil, wrapError("failed to parse solvers JSON", err)
	}

	for i := range solvers {
		solvers[i].driver = d
	}

	return solvers, nil
}
