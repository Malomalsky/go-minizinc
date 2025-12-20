package minizinc

import (
	"context"
	"testing"
	"time"
)

func TestDriver(t *testing.T) {
	driver, err := DefaultDriver()
	if err != nil {
		t.Skipf("minizinc not found: %v", err)
	}

	if driver.Version() == nil {
		t.Fatal("version should not be nil")
	}

	if !driver.Version().AtLeast(2, 6, 0) {
		t.Fatalf("version too old: %s", driver.Version())
	}
}

func TestFindSolver(t *testing.T) {
	solver, err := FindSolver("coin-bc")
	if err != nil {
		t.Skipf("solver not found: %v", err)
	}

	if solver.Name == "" {
		t.Fatal("solver name should not be empty")
	}

	if solver.ID == "" {
		t.Fatal("solver ID should not be empty")
	}
}

func TestListSolvers(t *testing.T) {
	solvers, err := ListSolvers()
	if err != nil {
		t.Skipf("cannot list solvers: %v", err)
	}

	if len(solvers) == 0 {
		t.Fatal("should have at least one solver")
	}
}

func TestModel(t *testing.T) {
	model := NewModel()

	model.AddString("var 1..10: x;")
	model.AddString("solve maximize x;")

	err := model.SetParam("a", 5)
	if err != nil {
		t.Fatalf("failed to set param: %v", err)
	}

	val, ok := model.GetParam("a")
	if !ok {
		t.Fatal("param should exist")
	}

	if val != 5 {
		t.Fatalf("expected 5, got %v", val)
	}

	err = model.SetParam("a", 10)
	if err == nil {
		t.Fatal("should not allow multiple assignments")
	}
}

func TestModelCopy(t *testing.T) {
	model := NewModel()
	model.AddString("var 1..10: x;")
	model.SetParam("a", 5)

	copy := model.Copy()
	copy.AddString("solve maximize x;")
	copy.SetParam("b", 10)

	codeOrig := model.getCode()
	codeCopy := copy.getCode()

	if codeOrig == codeCopy {
		t.Fatal("copy should have different code")
	}

	_, ok := model.GetParam("b")
	if ok {
		t.Fatal("original should not have param b")
	}
}

func TestSimpleSolve(t *testing.T) {
	solver, err := FindSolver("coin-bc")
	if err != nil {
		t.Skipf("solver not found: %v", err)
	}

	model := NewModel()
	model.AddString("var 1..10: x; solve maximize x;")

	instance, err := NewInstance(model, solver)
	if err != nil {
		t.Fatalf("failed to create instance: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := instance.Solve(ctx)
	if err != nil {
		t.Fatalf("solve failed: %v", err)
	}

	if result.Status != StatusOptimal {
		t.Fatalf("expected optimal, got %s", result.Status)
	}

	x, err := result.GetInt("x")
	if err != nil {
		t.Fatalf("failed to get x: %v", err)
	}

	if x != 10 {
		t.Fatalf("expected x=10, got %d", x)
	}
}

func TestSolveWithParams(t *testing.T) {
	solver, err := FindSolver("coin-bc")
	if err != nil {
		t.Skipf("solver not found: %v", err)
	}

	model := NewModel()
	model.AddString(`
		int: a;
		int: b;
		var 1..100: x;
		constraint a * x = b;
		solve satisfy;
	`)

	instance, err := NewInstance(model, solver)
	if err != nil {
		t.Fatalf("failed to create instance: %v", err)
	}
	instance.SetParam("a", 2)
	instance.SetParam("b", 20)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := instance.Solve(ctx)
	if err != nil {
		t.Fatalf("solve failed: %v", err)
	}

	x, err := result.GetInt("x")
	if err != nil {
		t.Fatalf("failed to get x: %v", err)
	}

	if x != 10 {
		t.Fatalf("expected x=10, got %d", x)
	}
}

func TestSolveAll(t *testing.T) {
	solver, err := FindSolver("coin-bc")
	if err != nil {
		t.Skipf("solver not found: %v", err)
	}

	model := NewModel()
	model.AddString("var 1..3: x; var 1..3: y; constraint x < y; solve satisfy;")

	instance, err := NewInstance(model, solver)
	if err != nil {
		t.Fatalf("failed to create instance: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	results, err := instance.SolveAll(ctx)
	if err != nil {
		t.Fatalf("solve failed: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("should have at least one solution")
	}

	for _, result := range results {
		x, err := result.GetInt("x")
		if err != nil {
			t.Fatalf("failed to get x: %v", err)
		}

		y, err := result.GetInt("y")
		if err != nil {
			t.Fatalf("failed to get y: %v", err)
		}

		if x >= y {
			t.Fatalf("constraint violated: x=%d, y=%d", x, y)
		}
	}
}

func TestSolveStream(t *testing.T) {
	solver, err := FindSolver("coin-bc")
	if err != nil {
		t.Skipf("solver not found: %v", err)
	}

	model := NewModel()
	model.AddString("var 1..5: x; solve satisfy;")

	instance, err := NewInstance(model, solver)
	if err != nil {
		t.Fatalf("failed to create instance: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	count := 0
	for result := range instance.SolveStream(ctx) {
		count++
		_, err := result.GetInt("x")
		if err != nil {
			t.Fatalf("failed to get x: %v", err)
		}
	}

	if count == 0 {
		t.Fatal("should have received at least one solution")
	}
}

func TestOptions(t *testing.T) {
	solver, err := FindSolver("coin-bc")
	if err != nil {
		t.Skipf("solver not found: %v", err)
	}

	model := NewModel()
	model.AddString("var 1..10: x; solve satisfy;")

	instance, err := NewInstance(model, solver)
	if err != nil {
		t.Fatalf("failed to create instance: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := instance.Solve(ctx,
		WithTimeLimit(5*time.Second),
	)

	if err != nil {
		t.Fatalf("solve failed: %v", err)
	}

	if result == nil {
		t.Fatal("result should not be nil")
	}

	results := []*Result{result}

	if len(results) == 0 {
		t.Fatal("should have at least one solution")
	}
}

func TestAutoSolver(t *testing.T) {
	model := NewModel()
	model.AddString("var 1..10: x; solve maximize x;")

	instance, err := NewInstanceAuto(model)
	if err != nil {
		t.Skipf("auto solver selection failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := instance.Solve(ctx)
	if err != nil {
		t.Fatalf("solve failed: %v", err)
	}

	x, err := result.GetInt("x")
	if err != nil {
		t.Fatalf("failed to get x: %v", err)
	}

	if x != 10 {
		t.Fatalf("expected x=10, got %d", x)
	}
}

func TestFindBestSolver(t *testing.T) {
	filter := SolverFilter{
		RequiredTags: []string{"mip"},
	}

	solver, err := FindBestSolver(filter)
	if err != nil {
		t.Skipf("no matching solver found: %v", err)
	}

	if !solver.HasTag("mip") {
		t.Fatal("solver should have mip tag")
	}
}
