package minizinc

import "strconv"

// SolverOptions is a renderer of solver-specific CLI flags. Pass an
// implementation to WithSolverOptions to append its Args() to the final argv.
// Library users can implement this interface for solvers we do not yet
// model directly.
type SolverOptions interface {
	Args() []string
}

// WithSolverOptions appends solver-specific flags to the command line. The
// flags are NOT validated against the active solver — passing GecodeOptions
// while solving with Chuffed will leak Gecode flags into Chuffed and the CLI
// will reject them.
func WithSolverOptions(opts SolverOptions) SolveOption {
	return func(o *SolveOptions) {
		if opts == nil {
			return
		}
		o.ExtraArgs = append(o.ExtraArgs, opts.Args()...)
	}
}

// GecodeOptions are tunables for the Gecode CP backend. Fields left at their
// zero value are omitted from the command line.
type GecodeOptions struct {
	// RestartStrategy is one of "none", "constant", "linear", "luby",
	// "geometric". Empty value omits the flag.
	RestartStrategy string
	// RestartScale multiplies the restart sequence base.
	RestartScale int
	// RestartBase is the base for geometric restarts.
	RestartBase float64
	// NodeLimit caps the number of search nodes.
	NodeLimit int
	// FailLimit caps the number of failures.
	FailLimit int
	// TimeLimitMS sets a solver-internal time limit in milliseconds. Prefer
	// WithTimeLimit at the option level; this field exists for parity.
	TimeLimitMS int
}

func (g GecodeOptions) Args() []string {
	var a []string
	if g.RestartStrategy != "" {
		a = append(a, "--restart", g.RestartStrategy)
	}
	if g.RestartScale > 0 {
		a = append(a, "--restart-scale", strconv.Itoa(g.RestartScale))
	}
	if g.RestartBase > 0 {
		a = append(a, "--restart-base", strconv.FormatFloat(g.RestartBase, 'g', -1, 64))
	}
	if g.NodeLimit > 0 {
		a = append(a, "--node-limit", strconv.Itoa(g.NodeLimit))
	}
	if g.FailLimit > 0 {
		a = append(a, "--fail-limit", strconv.Itoa(g.FailLimit))
	}
	if g.TimeLimitMS > 0 {
		a = append(a, "--time", strconv.Itoa(g.TimeLimitMS))
	}
	return a
}

// ChuffedOptions are tunables for the Chuffed lazy-clause-generation solver.
// Fields left zero are omitted.
type ChuffedOptions struct {
	// FreeSearch toggles `-f`, ignoring the model's search annotation.
	FreeSearch bool
	// VSIDS enables the VSIDS variable-selection heuristic.
	VSIDS bool
	// EagerLazyFD enables eager propagation of lazy FD constraints.
	EagerLazyFD bool
	// LearntPool sets the maximum number of learnt clauses retained.
	LearntPool int
}

func (c ChuffedOptions) Args() []string {
	var a []string
	if c.FreeSearch {
		a = append(a, "-f")
	}
	if c.VSIDS {
		a = append(a, "--toggle-vsids")
	}
	if c.EagerLazyFD {
		a = append(a, "--eager-lazy-fd")
	}
	if c.LearntPool > 0 {
		a = append(a, "--learnts-mlimit", strconv.Itoa(c.LearntPool))
	}
	return a
}

// CoinBCOptions are tunables for the COIN-BC MIP backend.
type CoinBCOptions struct {
	// PrintLevel controls solver verbosity (0..3).
	PrintLevel int
	// AbsGap is the absolute MIP gap tolerance.
	AbsGap float64
	// RelGap is the relative MIP gap tolerance.
	RelGap float64
	// MaxNodes caps the branch-and-bound node count.
	MaxNodes int
}

func (c CoinBCOptions) Args() []string {
	var a []string
	if c.PrintLevel > 0 {
		a = append(a, "--printLevel", strconv.Itoa(c.PrintLevel))
	}
	if c.AbsGap > 0 {
		a = append(a, "--absGap", strconv.FormatFloat(c.AbsGap, 'g', -1, 64))
	}
	if c.RelGap > 0 {
		a = append(a, "--relGap", strconv.FormatFloat(c.RelGap, 'g', -1, 64))
	}
	if c.MaxNodes > 0 {
		a = append(a, "--maxNodes", strconv.Itoa(c.MaxNodes))
	}
	return a
}
