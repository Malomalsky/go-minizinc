package minizinc

import (
	"context"
	"fmt"
	"os"
	"strconv"
)

type Instance struct {
	model    *Model
	solver   *Solver
	driver   *Driver
	tempFile string
}

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

func NewInstanceAuto(model *Model) (*Instance, error) {
	solver, err := FindSolverForModel(model)
	if err != nil {
		return nil, err
	}

	return NewInstance(model, solver)
}

func (inst *Instance) SetParam(name string, value interface{}) error {
	return inst.model.SetParam(name, value)
}

func (inst *Instance) GetParam(name string) (interface{}, bool) {
	return inst.model.GetParam(name)
}

func (inst *Instance) Solve(ctx context.Context, opts ...SolveOption) (*Result, error) {
	options := &SolveOptions{}
	for _, opt := range opts {
		opt(options)
	}

	args, err := inst.buildArgs(options)
	if err != nil {
		return nil, err
	}
	defer inst.Cleanup()

	messages, err := inst.driver.runJSON(ctx, args)
	if err != nil {
		return nil, err
	}

	var lastResult *Result
	var finalStatus Status = StatusUnknown
	var finalStats Statistics
	var hasStats bool

	for _, msg := range messages {
		if msg.Type == "solution" {
			result, err := parseStreamMessage(msg)
			if err != nil {
				return nil, err
			}
			lastResult = result
		} else if msg.Type == "statistics" {
			if stats, ok := parseStatisticsFromMessage(msg); ok {
				finalStats = stats
				hasStats = true
			}
		} else if msg.Type == "status" {
			finalStatus = msg.Status
		}
	}

	if lastResult == nil {
		result := &Result{
			Status:   finalStatus,
			Solution: make(map[string]interface{}),
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

func (inst *Instance) SolveAll(ctx context.Context, opts ...SolveOption) ([]*Result, error) {
	options := &SolveOptions{}
	for _, opt := range opts {
		opt(options)
	}

	if options.NumSolutions == 0 && !options.AllSolutions {
		options.AllSolutions = true
	}

	args, err := inst.buildArgs(options)
	if err != nil {
		return nil, err
	}
	defer inst.Cleanup()

	messages, err := inst.driver.runJSON(ctx, args)
	if err != nil {
		return nil, err
	}

	var results []*Result
	var finalStatus Status = StatusUnknown
	var finalStats Statistics
	var hasStats bool

	for _, msg := range messages {
		if msg.Type == "solution" {
			result, err := parseStreamMessage(msg)
			if err != nil {
				return nil, err
			}
			results = append(results, result)
		} else if msg.Type == "statistics" {
			if stats, ok := parseStatisticsFromMessage(msg); ok {
				finalStats = stats
				hasStats = true
			}
		} else if msg.Type == "status" {
			finalStatus = msg.Status
		}
	}

	for _, r := range results {
		r.Status = finalStatus
		if hasStats {
			r.Statistics = finalStats
		}
	}

	return results, nil
}

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

		args, err := inst.buildArgs(options)
		if err != nil {
			ch <- &Result{
				Status: StatusError,
				Error:  err,
			}
			return
		}
		defer inst.Cleanup()

		var finalStatus Status = StatusUnknown
		var latestStats Statistics
		var hasStats bool

		err = inst.driver.runJSONStream(ctx, args, func(msg streamMessage) error {
			if stats, ok := parseStatisticsFromMessage(msg); ok {
				latestStats = stats
				hasStats = true
			}

			switch msg.Type {
			case "solution":
				result, err := parseStreamMessage(msg)
				if err != nil {
					return err
				}
				if finalStatus != StatusUnknown {
					result.Status = finalStatus
				}
				if hasStats {
					result.Statistics = latestStats
				}
				select {
				case ch <- result:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			case "status":
				finalStatus = msg.Status
			}

			return nil
		})
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			ch <- &Result{
				Status: StatusError,
				Error:  err,
			}
		}
	}()

	return ch
}

func (inst *Instance) Cleanup() error {
	if inst.tempFile != "" {
		err := os.Remove(inst.tempFile)
		inst.tempFile = ""
		return err
	}
	return nil
}

func (inst *Instance) buildArgs(options *SolveOptions) ([]string, error) {
	tmpModel, err := os.CreateTemp("", "minizinc-*.mzn")
	if err != nil {
		return nil, wrapError("failed to create temp file", err)
	}
	defer tmpModel.Close()
	cleanupTemp := func() {
		_ = os.Remove(tmpModel.Name())
	}

	code := inst.model.getCode()
	if _, err := tmpModel.WriteString(code); err != nil {
		cleanupTemp()
		return nil, wrapError("failed to write model", err)
	}

	inst.tempFile = tmpModel.Name()
	args := []string{"--solver", inst.solver.ID}

	dataJSON, err := inst.model.getDataJSON()
	if err != nil {
		cleanupTemp()
		return nil, err
	}

	if dataJSON != "" {
		args = append(args, "--cmdline-json-data", dataJSON)
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

	if options.RandomSeed > 0 {
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
	args = append(args, tmpModel.Name())

	return args, nil
}
