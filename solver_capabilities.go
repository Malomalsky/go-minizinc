package minizinc

import (
	"context"
	"regexp"
	"strings"
)

// stripCommentsAndStrings removes MiniZinc line comments (% ... \n), block
// comments (/* ... */) and string literals from the source so that substring
// analysis does not get false positives from non-code text.
func stripCommentsAndStrings(code string) string {
	var sb strings.Builder
	sb.Grow(len(code))

	i := 0
	for i < len(code) {
		c := code[i]
		switch {
		case c == '%':
			for i < len(code) && code[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(code) && code[i+1] == '*':
			i += 2
			for i+1 < len(code) && !(code[i] == '*' && code[i+1] == '/') {
				i++
			}
			if i+1 < len(code) {
				i += 2
			} else {
				i = len(code)
			}
		case c == '"':
			i++
			for i < len(code) && code[i] != '"' {
				if code[i] == '\\' && i+1 < len(code) {
					i += 2
					continue
				}
				i++
			}
			if i < len(code) {
				i++
			}
		default:
			sb.WriteByte(c)
			i++
		}
	}
	return sb.String()
}

var floatVarRegexp = regexp.MustCompile(`var\s+-?\d+\.\d+`)

type SolverCapability string

const (
	CapabilityCP       SolverCapability = "cp"
	CapabilityMIP      SolverCapability = "mip"
	CapabilitySAT      SolverCapability = "sat"
	CapabilityFloat    SolverCapability = "float"
	CapabilityInt      SolverCapability = "int"
	CapabilityGlobal   SolverCapability = "global"
	CapabilityRestart  SolverCapability = "restart"
	CapabilityThreads  SolverCapability = "threads"
	CapabilityOptimize SolverCapability = "optimize"
)

type ModelSize string

const (
	SizeSmall  ModelSize = "small"
	SizeMedium ModelSize = "medium"
	SizeLarge  ModelSize = "large"
)

type ModelAnalysis struct {
	SolveType             SolveType
	HasGlobalConstraints  bool
	HasScheduling         bool
	HasSets               bool
	UsesFloats            bool
	EstimatedSize         ModelSize
	RequiredCapabilities  []SolverCapability
	GlobalConstraintsUsed []string
}

type SolverFilter struct {
	RequiredTags         []string
	RequiredCapabilities []SolverCapability
	PreferredTags        []string
	ExcludedTags         []string
}

func (s *Solver) HasTag(tag string) bool {
	tagLower := strings.ToLower(tag)
	for _, t := range s.Tags {
		if strings.ToLower(t) == tagLower {
			return true
		}
	}
	return false
}

func (s *Solver) HasCapability(cap SolverCapability) bool {
	return s.HasTag(string(cap))
}

func (s *Solver) SupportsGlobalConstraints() bool {
	return !s.HasTag("mip") || s.HasTag("global")
}

type SolverScore struct {
	Solver   *Solver
	Score    int
	Warnings []string
}

func scoreSolver(solver *Solver, analysis *ModelAnalysis) *SolverScore {
	score := &SolverScore{
		Solver:   solver,
		Score:    0,
		Warnings: make([]string, 0),
	}

	if analysis.HasGlobalConstraints && solver.HasTag("mip") {
		score.Warnings = append(score.Warnings, "MIP solver may not support all global constraints efficiently")
		score.Score -= 50
	}

	if solver.HasTag("cp") {
		score.Score += 30
	}

	if solver.HasTag("gecode") {
		score.Score += 20
	}

	if analysis.UsesFloats {
		if solver.HasTag("float") {
			score.Score += 25
		} else {
			score.Warnings = append(score.Warnings, "Solver may not support float variables")
			score.Score -= 100
		}
	}

	if analysis.HasScheduling {
		if solver.HasTag("cp") {
			score.Score += 15
		}
	}

	if analysis.EstimatedSize == SizeLarge {
		if solver.HasTag("threads") || solver.HasTag("parallel") {
			score.Score += 10
		}
	}

	if analysis.SolveType == SolveTypeMaximize || analysis.SolveType == SolveTypeMinimize {
		if solver.HasTag("mip") {
			score.Score += 10
		}
	}

	return score
}

func FindBestSolver(filter SolverFilter) (*Solver, error) {
	driver, err := DefaultDriver()
	if err != nil {
		return nil, err
	}

	return FindBestSolverWithDriver(filter, driver)
}

func FindBestSolverWithDriver(filter SolverFilter, driver *Driver) (*Solver, error) {
	solvers, err := driver.listSolvers(context.Background())
	if err != nil {
		return nil, err
	}

	var scores []*SolverScore

	for i := range solvers {
		solver := &solvers[i]

		excluded := false
		for _, excTag := range filter.ExcludedTags {
			if solver.HasTag(excTag) {
				excluded = true
				break
			}
		}
		if excluded {
			continue
		}

		matchesRequired := true
		for _, reqTag := range filter.RequiredTags {
			if !solver.HasTag(reqTag) {
				matchesRequired = false
				break
			}
		}

		if matchesRequired {
			for _, reqCap := range filter.RequiredCapabilities {
				if !solver.HasCapability(reqCap) {
					matchesRequired = false
					break
				}
			}
		}

		if !matchesRequired {
			continue
		}

		score := &SolverScore{Solver: solver, Score: 0}

		for _, prefTag := range filter.PreferredTags {
			if solver.HasTag(prefTag) {
				score.Score += 50
			}
		}

		scores = append(scores, score)
	}

	if len(scores) == 0 {
		return nil, ErrSolverNotFound
	}

	bestScore := scores[0]
	for _, s := range scores[1:] {
		if s.Score > bestScore.Score {
			bestScore = s
		}
	}

	return bestScore.Solver, nil
}

func FindBestSolverWithAnalysis(analysis *ModelAnalysis) (*Solver, *SolverScore, error) {
	driver, err := DefaultDriver()
	if err != nil {
		return nil, nil, err
	}

	solvers, err := driver.listSolvers(context.Background())
	if err != nil {
		return nil, nil, err
	}

	var scores []*SolverScore

	for i := range solvers {
		solver := &solvers[i]

		excluded := false
		if analysis.HasGlobalConstraints && solver.HasTag("mip") && !solver.HasTag("global") {
			excluded = true
		}

		if !excluded {
			score := scoreSolver(solver, analysis)

			if analysis.UsesFloats && !solver.HasTag("float") {
				continue
			}

			scores = append(scores, score)
		}
	}

	if len(scores) == 0 {
		return nil, nil, ErrSolverNotFound
	}

	bestScore := scores[0]
	for _, s := range scores[1:] {
		if s.Score > bestScore.Score {
			bestScore = s
		}
	}

	return bestScore.Solver, bestScore, nil
}

func analyzeModel(model *Model) *ModelAnalysis {
	if model == nil {
		return &ModelAnalysis{
			SolveType:             SolveTypeSatisfy,
			EstimatedSize:         SizeSmall,
			GlobalConstraintsUsed: make([]string, 0),
			RequiredCapabilities:  make([]SolverCapability, 0),
		}
	}

	code := stripCommentsAndStrings(model.getCode())
	codeLower := strings.ToLower(code)

	analysis := &ModelAnalysis{
		SolveType:             SolveTypeSatisfy,
		EstimatedSize:         SizeSmall,
		GlobalConstraintsUsed: make([]string, 0),
		RequiredCapabilities:  make([]SolverCapability, 0),
	}

	if strings.Contains(codeLower, "solve maximize") {
		analysis.SolveType = SolveTypeMaximize
	} else if strings.Contains(codeLower, "solve minimize") {
		analysis.SolveType = SolveTypeMinimize
	}

	globalConstraints := []string{
		"alldifferent", "global_cardinality", "circuit",
		"table", "regular", "inverse",
	}

	schedulingConstraints := []string{
		"cumulative", "disjunctive",
	}

	setConstraints := []string{
		"set of", "var set", "card", "union", "intersect", "diff",
	}

	// Look only for `name(` so that identifiers containing the same string
	// (e.g. a variable called all_different_count) do not trigger.
	for _, g := range globalConstraints {
		if strings.Contains(codeLower, g+"(") {
			analysis.HasGlobalConstraints = true
			analysis.GlobalConstraintsUsed = append(analysis.GlobalConstraintsUsed, g)
		}
	}

	for _, s := range schedulingConstraints {
		if strings.Contains(codeLower, s+"(") {
			analysis.HasScheduling = true
			analysis.HasGlobalConstraints = true
			analysis.GlobalConstraintsUsed = append(analysis.GlobalConstraintsUsed, s)
		}
	}

	for _, s := range setConstraints {
		if strings.Contains(codeLower, s) {
			analysis.HasSets = true
		}
	}

	if strings.Contains(codeLower, "float") ||
		strings.Contains(codeLower, "var 0.0..") ||
		floatVarRegexp.MatchString(codeLower) {
		analysis.UsesFloats = true
		analysis.RequiredCapabilities = append(analysis.RequiredCapabilities, CapabilityFloat)
	}

	if analysis.HasGlobalConstraints {
		analysis.RequiredCapabilities = append(analysis.RequiredCapabilities, CapabilityCP)
	}

	varCount := strings.Count(codeLower, "var ")
	constraintCount := strings.Count(codeLower, "constraint ")

	if varCount > 1000 || constraintCount > 500 {
		analysis.EstimatedSize = SizeLarge
	} else if varCount > 100 || constraintCount > 50 {
		analysis.EstimatedSize = SizeMedium
	}

	return analysis
}

func FindSolverForModel(model *Model) (*Solver, error) {
	analysis := analyzeModel(model)
	solver, _, err := FindBestSolverWithAnalysis(analysis)
	return solver, err
}

func FindSolverForModelWithWarnings(model *Model) (*Solver, []string, error) {
	analysis := analyzeModel(model)
	solver, score, err := FindBestSolverWithAnalysis(analysis)
	if err != nil {
		return nil, nil, err
	}
	return solver, score.Warnings, nil
}
