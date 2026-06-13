package minizinc

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"sync"
)

// Instance ties a Model to a Solver and a Driver. Methods are safe for
// concurrent callers but serialize internally: solve operations on the same
// Instance run one at a time.
type Instance struct {
	mu        sync.Mutex
	model     *Model
	solver    *Solver
	driver    *Driver
	tempFile  string
	tempFiles []string
}

// cmdlineJSONLimit controls when getDataJSON output is written to a temporary
// .json data file and passed via -d, instead of inlined with
// --cmdline-json-data. Keep below the cross-platform ARG_MAX comfort zone.
const cmdlineJSONLimit = 64 * 1024

// NewInstance returns an Instance bound to the given solver. If solver is nil,
// FindSolverForModel is used to pick one automatically.
func NewInstance(model *Model, solver *Solver) (*Instance, error) {
	if model == nil {
		return nil, ErrNilModel
	}

	if solver == nil {
		var err error
		solver, err = FindSolverForModel(model)
		if err != nil {
			return nil, err
		}
	}

	if solver == nil {
		return nil, ErrNoSolver
	}

	driver := solver.driver
	if driver == nil {
		var err error
		driver, err = DefaultDriver()
		if err != nil {
			return nil, err
		}
	}

	return &Instance{
		model:  model.Copy(),
		solver: solver,
		driver: driver,
	}, nil
}

// NewInstanceAuto picks the best solver via FindSolverForModel and returns an
// Instance bound to it.
func NewInstanceAuto(model *Model) (*Instance, error) {
	solver, err := FindSolverForModel(model)
	if err != nil {
		return nil, err
	}

	return NewInstance(model, solver)
}

// SetParam sets a parameter on the underlying model copy held by this Instance.
func (inst *Instance) SetParam(name string, value any) error {
	return inst.model.SetParam(name, value)
}

// GetParam returns a parameter from the underlying model copy.
func (inst *Instance) GetParam(name string) (any, bool) {
	return inst.model.GetParam(name)
}

// Solve runs the solver and returns the last solution along with the final
// status and statistics.
func (inst *Instance) Solve(ctx context.Context, opts ...SolveOption) (*Result, error) {
	options := &SolveOptions{}
	for _, opt := range opts {
		opt(options)
	}

	inst.mu.Lock()
	defer inst.mu.Unlock()

	args, err := inst.buildArgsLocked(options)
	if err != nil {
		return nil, err
	}
	defer inst.cleanupLocked()

	messages, err := inst.driver.runJSON(ctx, args, runConfigFor(options))
	if err != nil {
		return nil, err
	}

	var lastResult *Result
	var finalStatus = StatusUnknown
	var finalStats Statistics
	var hasStats bool

	for _, msg := range messages {
		switch msg.Type {
		case "solution":
			result, err := parseStreamMessage(msg)
			if err != nil {
				return nil, err
			}
			lastResult = result
		case "statistics":
			if stats, ok := parseStatisticsFromMessage(msg); ok {
				finalStats = stats
				hasStats = true
			}
		case "status":
			finalStatus = msg.Status
		}
	}

	if lastResult == nil {
		result := &Result{
			Status:   finalStatus,
			Solution: make(map[string]any),
		}
		if hasStats {
			result.Statistics = finalStats
		}
		return result, nil
	}

	lastResult.Status = finalStatus
	if hasStats {
		lastResult.Statistics = finalStats
	}
	return lastResult, nil
}

// SolveAll returns every solution the solver reports.
func (inst *Instance) SolveAll(ctx context.Context, opts ...SolveOption) ([]*Result, error) {
	options := &SolveOptions{}
	for _, opt := range opts {
		opt(options)
	}

	if options.NumSolutions == 0 && !options.AllSolutions {
		options.AllSolutions = true
	}

	inst.mu.Lock()
	defer inst.mu.Unlock()

	args, err := inst.buildArgsLocked(options)
	if err != nil {
		return nil, err
	}
	defer inst.cleanupLocked()

	messages, err := inst.driver.runJSON(ctx, args, runConfigFor(options))
	if err != nil {
		return nil, err
	}

	var results []*Result
	var finalStatus = StatusUnknown
	var finalStats Statistics
	var hasStats bool

	for _, msg := range messages {
		switch msg.Type {
		case "solution":
			result, err := parseStreamMessage(msg)
			if err != nil {
				return nil, err
			}
			results = append(results, result)
		case "statistics":
			if stats, ok := parseStatisticsFromMessage(msg); ok {
				finalStats = stats
				hasStats = true
			}
		case "status":
			finalStatus = msg.Status
		}
	}

	if len(results) > 0 {
		results[len(results)-1].Status = finalStatus
	}
	if hasStats {
		for _, r := range results {
			r.Statistics = finalStats
		}
	}

	return results, nil
}

// SolveStream emits solutions on the returned channel as they are produced. The
// channel is closed when the solver finishes or ctx is canceled.
func (inst *Instance) SolveStream(ctx context.Context, opts ...SolveOption) <-chan *Result {
	ch := make(chan *Result)

	go func() {
		defer close(ch)

		options := &SolveOptions{}
		for _, opt := range opts {
			opt(options)
		}

		if options.NumSolutions == 0 && !options.AllSolutions {
			options.AllSolutions = true
		}

		inst.mu.Lock()
		defer inst.mu.Unlock()

		args, err := inst.buildArgsLocked(options)
		if err != nil {
			select {
			case ch <- &Result{Status: StatusError, Error: err}:
			case <-ctx.Done():
			}
			return
		}
		defer inst.cleanupLocked()

		var finalStatus = StatusUnknown
		var latestStats Statistics
		var hasStats bool
		var pending *Result

		flush := func() error {
			if pending == nil {
				return nil
			}
			r := pending
			pending = nil
			if hasStats {
				r.Statistics = latestStats
			}
			select {
			case ch <- r:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		err = inst.driver.runJSONStream(ctx, args, runConfigFor(options), func(msg streamMessage) error {
			switch msg.Type {
			case "statistics":
				if stats, ok := parseStatisticsFromMessage(msg); ok {
					latestStats = stats
					hasStats = true
				}
			case "solution":
				result, err := parseStreamMessage(msg)
				if err != nil {
					return err
				}
				if err := flush(); err != nil {
					return err
				}
				pending = result
			case "status":
				finalStatus = msg.Status
			}

			return nil
		})
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			select {
			case ch <- &Result{Status: StatusError, Error: err}:
			case <-ctx.Done():
			}
			return
		}

		if pending != nil && finalStatus != StatusUnknown {
			pending.Status = finalStatus
		}
		_ = flush()
	}()

	return ch
}

// Cleanup removes the temporary model file written by the last solve, if any.
// Solve, SolveAll and SolveStream call Cleanup automatically; this is exposed
// for callers that abort before a solve completes.
func (inst *Instance) Cleanup() error {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.cleanupLocked()
}

func (inst *Instance) cleanupLocked() error {
	paths := append(inst.tempFiles, "")
	if inst.tempFile != "" {
		paths[len(paths)-1] = inst.tempFile
	} else {
		paths = paths[:len(paths)-1]
	}
	inst.tempFile = ""
	inst.tempFiles = nil

	var firstErr error
	for _, p := range paths {
		if p == "" {
			continue
		}
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

func (inst *Instance) buildArgsLocked(options *SolveOptions) ([]string, error) {
	tmpModel, err := os.CreateTemp("", "minizinc-*.mzn")
	if err != nil {
		return nil, wrapError("failed to create temp file", err)
	}
	tmpName := tmpModel.Name()
	cleanupTemp := func() {
		_ = tmpModel.Close()
		_ = os.Remove(tmpName)
	}

	code := inst.model.getCode()
	if _, err := tmpModel.WriteString(code); err != nil {
		cleanupTemp()
		return nil, wrapError("failed to write model", err)
	}
	if err := tmpModel.Close(); err != nil {
		_ = os.Remove(tmpName)
		return nil, wrapError("failed to close temp file", err)
	}

	args := []string{"--solver", inst.solver.ID}

	dataJSON, err := inst.model.getDataJSON()
	if err != nil {
		_ = os.Remove(tmpName)
		return nil, err
	}

	if dataJSON != "" {
		if len(dataJSON) > cmdlineJSONLimit {
			dataPath, err := writeTempJSON(dataJSON)
			if err != nil {
				_ = os.Remove(tmpName)
				return nil, err
			}
			inst.tempFiles = append(inst.tempFiles, dataPath)
			args = append(args, "-d", dataPath)
		} else {
			args = append(args, "--cmdline-json-data", dataJSON)
		}
	}

	for _, dataFile := range inst.model.dataFiles {
		args = append(args, "-d", dataFile)
	}

	if options.AllSolutions {
		args = append(args, "-a")
	}

	if options.NumSolutions > 0 {
		args = append(args, "--num-solutions", strconv.Itoa(options.NumSolutions))
	}

	if options.TimeLimit > 0 {
		ms := options.TimeLimit.Milliseconds()
		args = append(args, "--time-limit", strconv.FormatInt(ms, 10))
	}

	if options.Processes > 0 {
		args = append(args, "-p", strconv.Itoa(options.Processes))
	}

	if options.HasRandomSeed {
		args = append(args, "-r", strconv.Itoa(options.RandomSeed))
	}

	if options.FreeSearch {
		args = append(args, "-f")
	}

	if options.OptimizationLevel > 0 {
		args = append(args, fmt.Sprintf("-O%d", options.OptimizationLevel))
	}

	if options.Verbose {
		args = append(args, "-v")
	}

	if options.Statistics {
		args = append(args, "-s")
	}

	args = append(args, options.ExtraArgs...)
	args = append(args, tmpName)

	inst.tempFile = tmpName

	if options.CommandHook != nil {
		options.CommandHook(append([]string(nil), args...))
	}
	return args, nil
}

func runConfigFor(options *SolveOptions) runConfig {
	cfg := runConfig{grace: options.CancelGrace}
	if cfg.grace == 0 {
		cfg.grace = defaultCancelGrace
	}
	return cfg
}

func writeTempJSON(data string) (string, error) {
	f, err := os.CreateTemp("", "minizinc-data-*.json")
	if err != nil {
		return "", wrapError("failed to create temp data file", err)
	}
	name := f.Name()
	if _, err := f.WriteString(data); err != nil {
		_ = f.Close()
		_ = os.Remove(name)
		return "", wrapError("failed to write temp data file", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(name)
		return "", wrapError("failed to close temp data file", err)
	}
	return name, nil
}
