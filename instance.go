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
	options, err := applySolveOptions(opts...)
	if err != nil {
		return nil, err
	}

	inst.mu.Lock()
	defer inst.mu.Unlock()

	if err := inst.checkRequiredParamsLocked(options.params); err != nil {
		return nil, err
	}

	args, stdin, err := inst.buildArgsLocked(options)
	if err != nil {
		return nil, err
	}
	defer func() { _ = inst.cleanupLocked() }()

	cfg := runConfigFor(options)
	cfg.stdin = stdin
	messages, err := inst.driver.runJSON(ctx, args, cfg)
	if err != nil {
		return nil, err
	}

	var lastResult *Result
	var finalStatus = StatusUnknown
	var finalStats Statistics
	var hasStats bool
	var solutionCount int

	for _, msg := range messages {
		switch msg.Type {
		case "solution":
			result, err := parseStreamMessage(msg)
			if err != nil {
				return nil, err
			}
			lastResult = result
			solutionCount++
		case "statistics":
			if stats, ok := parseStatisticsFromMessage(msg); ok {
				finalStats = mergeStatistics(finalStats, stats)
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
			result.Statistics = cloneStatistics(finalStats)
		}
		if options.TimeLimit > 0 && result.Status == StatusUnknown {
			result.HitTimeLimit = true
		}
		return result, nil
	}

	if finalStatus != StatusUnknown {
		lastResult.Status = finalStatus
	}
	if hasStats {
		lastResult.Statistics = cloneStatistics(finalStats)
	}
	lastResult.HitTimeLimit = hitTimeLimit(options, finalStatus, solutionCount, analyzeModel(inst.model).SolveType)
	return lastResult, nil
}

// SolveAll returns every solution the solver reports.
func (inst *Instance) SolveAll(ctx context.Context, opts ...SolveOption) ([]*Result, error) {
	options, err := applySolveOptions(opts...)
	if err != nil {
		return nil, err
	}

	if options.NumSolutions == 0 && !options.AllSolutions {
		options.AllSolutions = true
	}

	inst.mu.Lock()
	defer inst.mu.Unlock()

	if err := inst.checkRequiredParamsLocked(options.params); err != nil {
		return nil, err
	}

	args, stdin, err := inst.buildArgsLocked(options)
	if err != nil {
		return nil, err
	}
	defer func() { _ = inst.cleanupLocked() }()

	cfg := runConfigFor(options)
	cfg.stdin = stdin
	messages, err := inst.driver.runJSON(ctx, args, cfg)
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
				finalStats = mergeStatistics(finalStats, stats)
				hasStats = true
			}
		case "status":
			finalStatus = msg.Status
		}
	}

	if len(results) > 0 && finalStatus != StatusUnknown {
		results[len(results)-1].Status = finalStatus
	}
	if hasStats {
		for _, r := range results {
			r.Statistics = cloneStatistics(finalStats)
		}
	}
	if len(results) > 0 {
		last := results[len(results)-1]
		last.HitTimeLimit = hitTimeLimit(options, finalStatus, len(results), analyzeModel(inst.model).SolveType)
	}

	// Empty result with a known terminal status (UNSAT / UNBOUNDED) or a
	// timeout deserves a synthetic Result so callers do not see an
	// indistinguishable "no solutions" slice.
	if len(results) == 0 && (finalStatus != StatusUnknown || options.TimeLimit > 0) {
		r := &Result{
			Status:   finalStatus,
			Solution: make(map[string]any),
		}
		if hasStats {
			r.Statistics = cloneStatistics(finalStats)
		}
		if options.TimeLimit > 0 && r.Status == StatusUnknown {
			r.HitTimeLimit = true
		}
		results = append(results, r)
	}

	return results, nil
}

// SolveStream emits solutions on the returned channel as they are produced. The
// channel is closed when the solver finishes or ctx is canceled.
func (inst *Instance) SolveStream(ctx context.Context, opts ...SolveOption) <-chan *Result {
	options, err := applySolveOptions(opts...)
	if err != nil {
		ch := make(chan *Result, 1)
		ch <- &Result{Status: StatusError, Error: err}
		close(ch)
		return ch
	}

	if options.NumSolutions == 0 && !options.AllSolutions {
		options.AllSolutions = true
	}

	ch := make(chan *Result)

	go func() {
		defer close(ch)

		inst.mu.Lock()
		defer inst.mu.Unlock()

		if err := inst.checkRequiredParamsLocked(options.params); err != nil {
			select {
			case ch <- &Result{Status: StatusError, Error: err}:
			case <-ctx.Done():
			}
			return
		}

		args, stdin, err := inst.buildArgsLocked(options)
		if err != nil {
			select {
			case ch <- &Result{Status: StatusError, Error: err}:
			case <-ctx.Done():
			}
			return
		}
		defer func() { _ = inst.cleanupLocked() }()

		cfg := runConfigFor(options)
		cfg.stdin = stdin

		var finalStatus = StatusUnknown
		var latestStats Statistics
		var hasStats bool
		var pending *Result
		var solutionCount int

		flush := func() error {
			if pending == nil {
				return nil
			}
			r := pending
			pending = nil
			r.IsIntermediate = true
			if hasStats {
				r.Statistics = cloneStatistics(latestStats)
			}
			select {
			case ch <- r:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		err = inst.driver.runJSONStream(ctx, args, cfg, func(msg streamMessage) error {
			switch msg.Type {
			case "statistics":
				if stats, ok := parseStatisticsFromMessage(msg); ok {
					latestStats = mergeStatistics(latestStats, stats)
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
				solutionCount++
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

		if pending != nil {
			if finalStatus != StatusUnknown {
				pending.Status = finalStatus
			}
			if hasStats {
				pending.Statistics = cloneStatistics(latestStats)
			}
			pending.IsIntermediate = false
			pending.HitTimeLimit = hitTimeLimit(options, finalStatus, solutionCount, analyzeModel(inst.model).SolveType)
			select {
			case ch <- pending:
			case <-ctx.Done():
			}
			return
		}

		// No solutions arrived but the solver reported a terminal status
		// (e.g. UNSATISFIABLE, UNBOUNDED, or UNKNOWN-after-time-limit).
		// Emit a synthetic empty Result so the consumer learns the outcome
		// instead of seeing the channel close without explanation.
		if finalStatus != StatusUnknown || options.TimeLimit > 0 {
			r := &Result{
				Status:   finalStatus,
				Solution: make(map[string]any),
			}
			if hasStats {
				r.Statistics = cloneStatistics(latestStats)
			}
			if options.TimeLimit > 0 && r.Status == StatusUnknown {
				r.HitTimeLimit = true
			}
			select {
			case ch <- r:
			case <-ctx.Done():
			}
		}
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

func (inst *Instance) buildArgsLocked(options *SolveOptions) ([]string, []byte, error) {
	if err := validateSolveOptions(options); err != nil {
		return nil, nil, err
	}

	code := inst.model.getCode()

	args := []string{"--solver", inst.solver.ID}
	mode := options.OutputMode
	if mode == "" {
		mode = OutputModeJSON
	}
	args = append(args, "--output-mode", string(mode))
	if mode != OutputModeItem {
		args = append(args, "--output-output-item")
	}

	for _, includeDir := range inst.model.includeDirs {
		args = append(args, "-I", includeDir)
	}

	dataJSON, err := inst.model.getDataJSON(options.params)
	if err != nil {
		return nil, nil, err
	}

	if dataJSON != "" {
		dataPath, err := writeTempJSON(dataJSON)
		if err != nil {
			return nil, nil, err
		}
		inst.tempFiles = append(inst.tempFiles, dataPath)
		args = append(args, "-d", dataPath)
	}

	for _, dataFile := range inst.model.dataFiles {
		args = append(args, "-d", dataFile)
	}

	if options.NumSolutions > 0 {
		args = append(args, "--num-solutions", strconv.Itoa(options.NumSolutions))
	} else if options.AllSolutions {
		args = append(args, "-a")
	}

	if options.TimeLimit > 0 {
		ms := options.TimeLimit.Milliseconds()
		if ms == 0 {
			ms = 1
		}
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

	if options.HasOptimizationLevel {
		args = append(args, fmt.Sprintf("-O%d", options.OptimizationLevel))
	}

	if options.Verbose {
		args = append(args, "-v")
	}

	if options.Statistics {
		args = append(args, "-s")
	}

	args = append(args, options.ExtraArgs...)

	var stdin []byte
	if options.ModelViaStdin {
		args = append(args, "--input-from-stdin")
		stdin = []byte(code)
	} else {
		tmpName, err := writeTempModel(code)
		if err != nil {
			_ = inst.cleanupLocked()
			return nil, nil, err
		}
		args = append(args, tmpName)
		inst.tempFile = tmpName
	}

	if options.CommandHook != nil {
		options.CommandHook(append([]string(nil), args...))
	}
	return args, stdin, nil
}

func validateSolveOptions(options *SolveOptions) error {
	switch options.OutputMode {
	case "", OutputModeJSON, OutputModeDZN, OutputModeItem:
	default:
		return newError(fmt.Sprintf("invalid output mode %q", options.OutputMode))
	}
	if options.NumSolutions < 0 {
		return newError("number of solutions must not be negative")
	}
	if options.TimeLimit < 0 {
		return newError("time limit must not be negative")
	}
	if options.Processes < 0 {
		return newError("process count must not be negative")
	}
	if options.HasOptimizationLevel && (options.OptimizationLevel < 0 || options.OptimizationLevel > 5) {
		return newError("optimization level must be between 0 and 5")
	}
	if options.HasRandomSeed && options.RandomSeed < 0 {
		return newError("random seed must not be negative")
	}
	if options.HasCancelGrace && options.CancelGrace < 0 {
		return newError("cancel grace must not be negative")
	}
	return nil
}

func hitTimeLimit(options *SolveOptions, finalStatus Status, solutions int, solveType SolveType) bool {
	if options.TimeLimit <= 0 || finalStatus != "" && finalStatus != StatusUnknown {
		return false
	}
	if solutions == 0 {
		return true
	}
	if options.NumSolutions > 0 {
		return solutions < options.NumSolutions
	}
	return options.AllSolutions || solveType != SolveTypeSatisfy
}

func (inst *Instance) checkRequiredParamsLocked(params Params) error {
	missing := inst.model.missingParams(params)
	if len(missing) > 0 {
		return &MissingParamsError{Missing: missing}
	}
	return nil
}

func writeTempModel(code string) (string, error) {
	f, err := os.CreateTemp("", "minizinc-*.mzn")
	if err != nil {
		return "", wrapError("failed to create temp file", err)
	}
	name := f.Name()
	if _, err := f.WriteString(code); err != nil {
		_ = f.Close()
		_ = os.Remove(name)
		return "", wrapError("failed to write model", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(name)
		return "", wrapError("failed to close temp file", err)
	}
	return name, nil
}

func runConfigFor(options *SolveOptions) runConfig {
	grace := defaultCancelGrace
	if options.HasCancelGrace {
		grace = options.CancelGrace
	}
	return runConfig{grace: grace}
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
