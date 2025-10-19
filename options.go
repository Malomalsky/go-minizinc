package minizinc

import "time"

type SolveOptions struct {
	AllSolutions      bool
	NumSolutions      int
	TimeLimit         time.Duration
	Processes         int
	RandomSeed        int
	FreeSearch        bool
	OptimizationLevel int
	Verbose           bool
	Statistics        bool
	ExtraArgs         []string
}

type SolveOption func(*SolveOptions)

func WithAllSolutions() SolveOption {
	return func(o *SolveOptions) {
		o.AllSolutions = true
	}
}

func WithNumSolutions(n int) SolveOption {
	return func(o *SolveOptions) {
		o.NumSolutions = n
	}
}

func WithTimeLimit(d time.Duration) SolveOption {
	return func(o *SolveOptions) {
		o.TimeLimit = d
	}
}

func WithProcesses(n int) SolveOption {
	return func(o *SolveOptions) {
		o.Processes = n
	}
}

func WithRandomSeed(seed int) SolveOption {
	return func(o *SolveOptions) {
		o.RandomSeed = seed
	}
}

func WithFreeSearch() SolveOption {
	return func(o *SolveOptions) {
		o.FreeSearch = true
	}
}

func WithOptimizationLevel(level int) SolveOption {
	return func(o *SolveOptions) {
		o.OptimizationLevel = level
	}
}

func WithVerbose() SolveOption {
	return func(o *SolveOptions) {
		o.Verbose = true
	}
}

func WithStatistics() SolveOption {
	return func(o *SolveOptions) {
		o.Statistics = true
	}
}

func WithExtraArgs(args ...string) SolveOption {
	return func(o *SolveOptions) {
		o.ExtraArgs = append(o.ExtraArgs, args...)
	}
}
