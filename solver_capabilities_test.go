package minizinc

import (
	"fmt"
	"strings"
	"testing"
)

func TestAnalyzeModel_SolveType(t *testing.T) {
	tests := []struct {
		code     string
		expected SolveType
	}{
		{"var 1..10: x; solve satisfy;", SolveTypeSatisfy},
		{"var 1..10: x; solve minimize x;", SolveTypeMinimize},
		{"var 1..10: x; solve maximize x;", SolveTypeMaximize},
	}

	for _, tt := range tests {
		model := NewModel()
		model.AddString(tt.code)

		analysis := analyzeModel(model)
		if analysis.SolveType != tt.expected {
			t.Errorf("Expected %v, got %v for code: %s", tt.expected, analysis.SolveType, tt.code)
		}
	}
}

func TestAnalyzeModel_GlobalConstraints(t *testing.T) {
	model := NewModel()
	model.AddString(`
		array[1..10] of var 1..10: x;
		constraint alldifferent(x);
		solve satisfy;
	`)

	analysis := analyzeModel(model)
	if !analysis.HasGlobalConstraints {
		t.Error("Expected HasGlobalConstraints to be true")
	}

	if len(analysis.GlobalConstraintsUsed) == 0 {
		t.Error("Expected GlobalConstraintsUsed to contain 'alldifferent'")
	}
}

func TestAnalyzeModel_Scheduling(t *testing.T) {
	model := NewModel()
	model.AddString(`
		array[1..5] of var 1..10: start;
		array[1..5] of int: duration = [2,3,4,5,6];
		array[1..5] of int: resource = [1,1,1,1,1];
		constraint cumulative(start, duration, resource, 3);
		solve satisfy;
	`)

	analysis := analyzeModel(model)
	if !analysis.HasScheduling {
		t.Error("Expected HasScheduling to be true")
	}

	if !analysis.HasGlobalConstraints {
		t.Error("Scheduling should also set HasGlobalConstraints")
	}
}

func TestAnalyzeModel_Float(t *testing.T) {
	model := NewModel()
	model.AddString(`
		var float: x;
		constraint x >= 0.5;
		solve maximize x;
	`)

	analysis := analyzeModel(model)
	if !analysis.UsesFloats {
		t.Error("Expected UsesFloats to be true")
	}

	hasFloatCap := false
	for _, cap := range analysis.RequiredCapabilities {
		if cap == CapabilityFloat {
			hasFloatCap = true
			break
		}
	}

	if !hasFloatCap {
		t.Error("Expected RequiredCapabilities to contain CapabilityFloat")
	}
}

func TestAnalyzeModel_Sets(t *testing.T) {
	model := NewModel()
	model.AddString(`
		var set of 1..10: s;
		constraint card(s) = 5;
		solve satisfy;
	`)

	analysis := analyzeModel(model)
	if !analysis.HasSets {
		t.Error("Expected HasSets to be true")
	}
}

func TestAnalyzeModel_Size(t *testing.T) {
	smallModel := NewModel()
	smallModel.AddString("var 1..10: x; solve satisfy;")

	mediumModel := NewModel()
	var sb strings.Builder
	for i := 0; i < 150; i++ {
		fmt.Fprintf(&sb, "var 1..10: x%d;\n", i)
	}
	sb.WriteString("solve satisfy;")
	mediumModel.AddString(sb.String())

	smallAnalysis := analyzeModel(smallModel)
	if smallAnalysis.EstimatedSize != SizeSmall {
		t.Errorf("Expected SizeSmall, got %v", smallAnalysis.EstimatedSize)
	}

	mediumAnalysis := analyzeModel(mediumModel)
	if mediumAnalysis.EstimatedSize != SizeMedium {
		t.Errorf("Expected SizeMedium, got %v", mediumAnalysis.EstimatedSize)
	}
}

func TestScoreSolver(t *testing.T) {
	analysis := &ModelAnalysis{
		SolveType:            SolveTypeMaximize,
		HasGlobalConstraints: true,
		UsesFloats:           false,
		EstimatedSize:        SizeSmall,
	}

	cpSolver := &Solver{
		Tags: []string{"cp", "gecode"},
	}

	mipSolver := &Solver{
		Tags: []string{"mip"},
	}

	cpScore := scoreSolver(cpSolver, analysis)
	mipScore := scoreSolver(mipSolver, analysis)

	if cpScore.Score <= mipScore.Score {
		t.Errorf("CP solver should score higher than MIP solver for global constraints. CP: %d, MIP: %d",
			cpScore.Score, mipScore.Score)
	}

	if len(mipScore.Warnings) == 0 {
		t.Error("MIP solver should have warnings for global constraints")
	}
}

func TestFindSolverForModelWithWarnings(t *testing.T) {
	model := NewModel()
	model.AddString("var 1..10: x; solve maximize x;")

	solver, warnings, err := FindSolverForModelWithWarnings(model)
	if err != nil {
		t.Skipf("No solver found: %v", err)
	}

	if solver == nil {
		t.Fatal("Solver should not be nil")
	}

	t.Logf("Selected solver: %s with %d warnings", solver.Name, len(warnings))
	for _, w := range warnings {
		t.Logf("Warning: %s", w)
	}
}
