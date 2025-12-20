package minizinc

import "time"

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

type Method string

const (
	MethodSatisfy  Method = "satisfy"
	MethodMinimize Method = "minimize"
	MethodMaximize Method = "maximize"
)

type SolveType string

const (
	SolveTypeSatisfy  SolveType = "sat"
	SolveTypeMinimize SolveType = "min"
	SolveTypeMaximize SolveType = "max"
)

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
	Type       string                 `json:"type"`
	Status     Status                 `json:"status,omitempty"`
	Output     map[string]interface{} `json:"output,omitempty"`
	Sections   []string               `json:"sections,omitempty"`
	Time       *float64               `json:"time,omitempty"`
	Solution   map[string]interface{} `json:"solution,omitempty"`
	Statistics map[string]interface{} `json:"statistics,omitempty"`
}
