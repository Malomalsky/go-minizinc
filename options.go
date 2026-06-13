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
	CancelGrace       time.Duration
	ModelViaStdin     bool
}

// defaultCancelGrace is the time we give MiniZinc to flush stats and exit
// cleanly after receiving SIGTERM when the context is cancelled. The Go
// runtime escalates to SIGKILL once this elapses.
const defaultCancelGrace = 2 * time.Second

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

// WithCancelGrace overrides the time the solver is allowed to exit cleanly
// after the context is cancelled before SIGKILL. The default is two seconds;
// pass a positive duration to lengthen or shorten it.
func WithCancelGrace(d time.Duration) SolveOption {
	return func(o *SolveOptions) {
		o.CancelGrace = d
	}
}

// WithModelViaStdin streams the assembled model to MiniZinc via stdin
// (--input-from-stdin) instead of writing a tmp .mzn file. Reduces disk
// I/O and the race surface around tmp file cleanup, at the cost of
// requiring MiniZinc 2.6+ stdin support. Safe to use only when the model
// has no AddFile (-d) data files referenced by relative path that depend
// on cwd resolution.
func WithModelViaStdin() SolveOption {
	return func(o *SolveOptions) {
		o.ModelViaStdin = true
	}
}
