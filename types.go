package minizinc

import "time"

// Status is the solver-reported outcome of a solve.
type Status string

const (
	StatusUnknown          Status = "UNKNOWN"
	StatusSatisfied        Status = "SATISFIED"
	StatusAllSolutions     Status = "ALL_SOLUTIONS"
	StatusOptimal          Status = "OPTIMAL_SOLUTION"
	StatusUnsatisfiable    Status = "UNSATISFIABLE"
	StatusUnbounded        Status = "UNBOUNDED"
	StatusUnsatOrUnbounded Status = "UNSATISFIABLE_OR_UNBOUNDED"
	StatusError            Status = "ERROR"
)

// SolveType describes the objective of a MiniZinc model: satisfy, minimize or
// maximize.
type SolveType string

const (
	SolveTypeSatisfy  SolveType = "sat"
	SolveTypeMinimize SolveType = "min"
	SolveTypeMaximize SolveType = "max"
)

// Statistics aggregates the most common solver counters and timings. Fields
// not reported by the solver remain zero. PropagatorRuns and Propagations are
// reported under the keys "propagations" and "propags" respectively — both
// kept because different solvers populate one or the other.
type Statistics struct {
	Nodes          int64         `json:"nodes,omitempty"`
	Failures       int64         `json:"failures,omitempty"`
	RestartCount   int64         `json:"restarts,omitempty"`
	Variables      int64         `json:"variables,omitempty"`
	PropagatorRuns int64         `json:"propagations,omitempty"`
	Propagations   int64         `json:"propags,omitempty"`
	PeakDepth      int64         `json:"peakDepth,omitempty"`
	NoGoods        int64         `json:"nogoods,omitempty"`
	Backtracks     int64         `json:"backjumps,omitempty"`
	SolveTime      time.Duration `json:"solveTime,omitempty"`
	InitTime       time.Duration `json:"initTime,omitempty"`
	FlatTime       time.Duration `json:"flatTime,omitempty"`
	Paths          int64         `json:"nPaths,omitempty"`
}

type streamMessage struct {
	Type       string         `json:"type"`
	Status     Status         `json:"status,omitempty"`
	Output     map[string]any `json:"output,omitempty"`
	Sections   []string       `json:"sections,omitempty"`
	Solution   map[string]any `json:"solution,omitempty"`
	Statistics map[string]any `json:"statistics,omitempty"`

	// Populated for type=="error" / type=="warning" messages emitted on
	// stdout. MiniZinc 2.6+ surfaces syntax and type errors here rather
	// than on stderr.
	What     string `json:"what,omitempty"`
	Message  string `json:"message,omitempty"`
	Location any    `json:"location,omitempty"`
}
