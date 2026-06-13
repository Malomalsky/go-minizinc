package minizinc

import "time"

// SolveOptions configures a single solve invocation. Build with the
// functional With* helpers; do not mutate directly except in tests.
type SolveOptions struct {
	AllSolutions      bool
	NumSolutions      int
	TimeLimit         time.Duration
	Processes         int
	RandomSeed        int
	HasRandomSeed     bool
	FreeSearch        bool
	OptimizationLevel int
	Verbose           bool
	Statistics        bool
	ExtraArgs         []string
	CommandHook       func([]string)
}

// SolveOption mutates a SolveOptions; pass returned values to Solve.
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
		o.HasRandomSeed = true
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

// WithCommandHook installs a callback that receives the final argv passed to
// the MiniZinc binary, just before exec. Useful for logging or diagnosing
// auto-solver selection. The slice is a copy and may be retained.
func WithCommandHook(hook func(args []string)) SolveOption {
	return func(o *SolveOptions) {
		o.CommandHook = hook
	}
}
