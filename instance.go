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

func NewInstance(model *Model, solver *Solver) *Instance {
	if model == nil {
		return nil
	}

	if solver == nil {
		solver, _ = FindSolverForModel(model)
	}

	if solver == nil {
		return nil
	}

	driver := solver.driver
	if driver == nil {
		driver, _ = DefaultDriver()
	}

	return &Instance{
		model:  model.Copy(),
		solver: solver,
		driver: driver,
	}
}

func NewInstanceAuto(model *Model) (*Instance, error) {
	solver, err := FindSolverForModel(model)
	if err != nil {
		return nil, err
	}

	return NewInstance(model, solver), nil
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

	for _, msg := range messages {
		if msg.Type == "solution" {
			result, err := parseStreamMessage(msg)
			if err != nil {
				return nil, err
			}
			lastResult = result
		} else if msg.Type == "status" {
			finalStatus = msg.Status
		}
	}

	if lastResult == nil {
		return &Result{
			Status:   finalStatus,
			Solution: make(map[string]interface{}),
		}, nil
	}

	lastResult.Status = finalStatus
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

	for _, msg := range messages {
		if msg.Type == "solution" {
			result, err := parseStreamMessage(msg)
			if err != nil {
				return nil, err
			}
			results = append(results, result)
		} else if msg.Type == "status" {
			finalStatus = msg.Status
		}
	}

	for _, r := range results {
		r.Status = finalStatus
	}

	return results, nil
}

func (inst *Instance) SolveStream(ctx context.Context, opts ...SolveOption) <-chan *Result {
	ch := make(chan *Result)

	go func() {
		defer close(ch)

		results, err := inst.SolveAll(ctx, opts...)
		if err != nil {
			ch <- &Result{
				Status: StatusError,
				Error:  err,
			}
			return
		}

		for _, result := range results {
			select {
			case ch <- result:
			case <-ctx.Done():
				return
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

	code := inst.model.getCode()
	if _, err := tmpModel.WriteString(code); err != nil {
		return nil, wrapError("failed to write model", err)
	}

	inst.tempFile = tmpModel.Name()
	args := []string{"--solver", inst.solver.ID}

	dataJSON, err := inst.model.getDataJSON()
	if err != nil {
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
