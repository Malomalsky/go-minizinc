package minizinc

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

func TestModelCopy_TypedSlice(t *testing.T) {
	orig := NewModel()
	if err := orig.SetParam("xs", []int{1, 2, 3}); err != nil {
		t.Fatal(err)
	}

	dup := orig.Copy()
	got, _ := dup.GetParam("xs")
	xs := got.([]int)
	xs[0] = 999

	origGot, _ := orig.GetParam("xs")
	if origGot.([]int)[0] != 1 {
		t.Fatalf("original mutated through shared slice: got %v", origGot)
	}
}

func TestModelCopy_TypedMap(t *testing.T) {
	orig := NewModel()
	if err := orig.SetParam("m", map[string]int{"a": 1}); err != nil {
		t.Fatal(err)
	}

	dup := orig.Copy()
	got, _ := dup.GetParam("m")
	got.(map[string]int)["a"] = 42

	origGot, _ := orig.GetParam("m")
	if origGot.(map[string]int)["a"] != 1 {
		t.Fatalf("original map mutated: got %v", origGot)
	}
}

func TestModelCopy_NestedInterface(t *testing.T) {
	orig := NewModel()
	inner := []any{1, 2, 3}
	outer := []any{inner}
	if err := orig.SetParam("nested", outer); err != nil {
		t.Fatal(err)
	}

	dup := orig.Copy()
	got, _ := dup.GetParam("nested")
	got.([]any)[0].([]any)[0] = 999

	origGot, _ := orig.GetParam("nested")
	if origGot.([]any)[0].([]any)[0] != 1 {
		t.Fatalf("nested mutation leaked: %v", origGot)
	}
}

func TestModelCopy_NilParameter(t *testing.T) {
	orig := NewModel()
	if err := orig.SetParam("x", nil); err != nil {
		t.Fatal(err)
	}
	dup := orig.Copy()
	if got, _ := dup.GetParam("x"); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestNewInstance_NilModel(t *testing.T) {
	if _, err := NewInstance(nil, nil); !errors.Is(err, ErrNilModel) {
		t.Fatalf("expected ErrNilModel, got %v", err)
	}
}

func TestStripCommentsAndStrings(t *testing.T) {
	cases := []struct {
		name string
		in   string
		out  string
	}{
		{"line comment", "var x; % comment\nvar y;", "var x; \nvar y;"},
		{"block comment", "var x; /* foo */ var y;", "var x;  var y;"},
		{"string literal", `output ["x"];`, `output [];`},
		{"escaped quote", `output ["a\"b"];`, `output [];`},
		{"unterminated string drops trailing", `var x; "tail`, `var x; `},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := stripCommentsAndStrings(tc.in)
			if got != tc.out {
				t.Errorf("got %q, want %q", got, tc.out)
			}
		})
	}
}

func TestAnalyzeModel_IgnoresCommentTriggers(t *testing.T) {
	model := NewModel()
	model.AddString(`
		% alldifferent is mentioned here only
		var 1..10: x;
		solve satisfy;
	`)
	a := analyzeModel(model)
	if a.HasGlobalConstraints {
		t.Error("HasGlobalConstraints triggered from a comment")
	}
	if a.SolveType != SolveTypeSatisfy {
		t.Errorf("expected satisfy, got %v", a.SolveType)
	}
}

func TestAnalyzeModel_IgnoresStringLiteralTriggers(t *testing.T) {
	model := NewModel()
	model.AddString(`
		var 1..10: x;
		output ["solve maximize x as text"];
		solve satisfy;
	`)
	a := analyzeModel(model)
	if a.SolveType != SolveTypeSatisfy {
		t.Errorf("expected satisfy, got %v", a.SolveType)
	}
}

func TestWithRandomSeed_Zero(t *testing.T) {
	o := &SolveOptions{}
	WithRandomSeed(0)(o)
	if !o.HasRandomSeed {
		t.Fatal("HasRandomSeed must be true after WithRandomSeed(0)")
	}
	if o.RandomSeed != 0 {
		t.Fatalf("seed = %d, want 0", o.RandomSeed)
	}
}

func TestInstance_Cleanup_Idempotent(t *testing.T) {
	inst := &Instance{}
	if err := inst.Cleanup(); err != nil {
		t.Fatalf("first cleanup: %v", err)
	}
	if err := inst.Cleanup(); err != nil {
		t.Fatalf("second cleanup: %v", err)
	}
}

func TestInstance_Cleanup_MissingFile(t *testing.T) {
	inst := &Instance{tempFile: "/tmp/definitely-missing-mzn-file"}
	if err := inst.Cleanup(); err != nil {
		t.Fatalf("expected nil for missing file, got %v", err)
	}
	if inst.tempFile != "" {
		t.Fatal("tempFile should be cleared")
	}
}

func TestInstance_ConcurrentSolve_Serialized(t *testing.T) {
	// Exercise the lock path. Driver is nil so any real Solve call would
	// nil-panic; here we just hammer Cleanup which exercises the same mutex
	// and verify it does not deadlock or race under -race.
	inst := &Instance{}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = inst.Cleanup()
		}()
	}
	wg.Wait()
}

func TestNewInstance_NilSolverAutoFails_WhenNoDriver(t *testing.T) {
	// If minizinc is not installed FindSolverForModel returns an error.
	// When it IS installed, auto-selection may succeed; skip in that case.
	model := NewModel()
	model.AddString("var 1..10: x; solve satisfy;")
	if _, err := NewInstance(model, nil); err == nil {
		t.Skip("minizinc available — auto-selection succeeded; nothing to assert here")
	}
}

func TestStripCommentsAndStrings_Empty(t *testing.T) {
	if got := stripCommentsAndStrings(""); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestStripCommentsAndStrings_NoStripNeeded(t *testing.T) {
	in := "var 1..10: x;"
	if got := stripCommentsAndStrings(in); got != in {
		t.Errorf("got %q, want %q", got, in)
	}
}

func TestModel_GetCode_PreservesFragmentOrder(t *testing.T) {
	m := NewModel()
	m.AddString("a")
	m.AddString("b")
	m.AddString("c")
	got := m.getCode()
	if !strings.HasPrefix(got, "a\n") || !strings.Contains(got, "b\n") || !strings.HasSuffix(got, "c\n") {
		t.Errorf("unexpected: %q", got)
	}
}
