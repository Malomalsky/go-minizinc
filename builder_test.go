package minizinc

import (
	"strings"
	"testing"
	"time"
)

func TestBuilder_BasicSatisfy(t *testing.T) {
	b := NewBuilder()
	x := b.IntVar("x", 1, 10)
	b.Constraint(x.Gt(Int(5)))
	b.Satisfy()

	m := b.Build()
	got := m.getCode()
	for _, want := range []string{
		"var 1..10: x;",
		"constraint (x > 5);",
		"solve satisfy;",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestBuilder_Maximize(t *testing.T) {
	b := NewBuilder()
	x := b.IntVar("x", 1, 100)
	b.Maximize(x)

	got := b.Build().getCode()
	if !strings.Contains(got, "solve maximize x;") {
		t.Errorf("missing maximize in:\n%s", got)
	}
}

func TestBuilder_ParamAndArray(t *testing.T) {
	b := NewBuilder()
	n := b.IntParam("n")
	queens := b.IntArrayVarSized("queens", n, 1, 0) // hi=0 — placeholder, recover via Raw if needed
	_ = queens
	b.Satisfy()
	got := b.Build().getCode()
	for _, want := range []string{
		"int: n;",
		"array[1..n] of var 1..0: queens;",
		"solve satisfy;",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestBuilder_AllDifferentIncludesGlobals(t *testing.T) {
	b := NewBuilder()
	x := b.IntArray("x", 5, 1, 5)
	b.Constraint(b.AllDifferent(x))
	b.Satisfy()

	got := b.Build().getCode()
	if !strings.Contains(got, `include "alldifferent.mzn";`) {
		t.Errorf("expected include of alldifferent.mzn:\n%s", got)
	}
	if !strings.Contains(got, "constraint alldifferent(x);") {
		t.Errorf("expected alldifferent(x) constraint:\n%s", got)
	}
}

func TestBuilder_DuplicateIdentifierPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on duplicate identifier")
		}
	}()
	b := NewBuilder()
	b.IntVar("x", 1, 10)
	b.IntVar("x", 1, 10)
}

func TestBuilder_InvalidIdentifierPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on invalid identifier")
		}
	}()
	b := NewBuilder()
	b.IntVar("123bad", 1, 10)
}

func TestExpr_Operators(t *testing.T) {
	a, c := Var("a"), Var("c")
	cases := []struct {
		got  Expr
		want string
	}{
		{a.Add(c), "(a + c)"},
		{a.Sub(c), "(a - c)"},
		{a.Mul(Int(2)), "(a * 2)"},
		{a.Eq(Int(5)), "(a = 5)"},
		{a.Ne(c), "(a != c)"},
		{a.And(c), `(a /\ c)`},
		{a.Or(c), `(a \/ c)`},
		{a.Implies(c), "(a -> c)"},
		{a.Iff(c), "(a <-> c)"},
		{a.Not(), "not (a)"},
		{a.At(Int(1)), "a[1]"},
		{a.At2(Int(1), Int(2)), "a[1,2]"},
		{Sum(a, c, Int(1)), "sum([a, c, 1])"},
		{Forall(Var("i"), Range(Int(1), Int(5)), a.At(Var("i")).Eq(Int(0))), "forall(i in 1..5)((a[i] = 0))"},
	}
	for _, tc := range cases {
		if tc.got.code != tc.want {
			t.Errorf("got %q, want %q", tc.got.code, tc.want)
		}
	}
}

func TestFormatFloat(t *testing.T) {
	cases := []struct {
		v    float64
		want string
	}{
		{1.0, "1.0"},
		{1.5, "1.5"},
		{-0.5, "-0.5"},
		{1e10, "1e+10"},
	}
	for _, tc := range cases {
		if got := formatFloat(tc.v); got != tc.want {
			t.Errorf("formatFloat(%v) = %q, want %q", tc.v, got, tc.want)
		}
	}
}

func TestSolverOptions_Gecode(t *testing.T) {
	g := GecodeOptions{
		RestartStrategy: "luby",
		RestartScale:    100,
		NodeLimit:       5000,
	}
	got := g.Args()
	want := []string{"--restart", "luby", "--restart-scale", "100", "--node-limit", "5000"}
	if !sliceEq(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestSolverOptions_Chuffed(t *testing.T) {
	c := ChuffedOptions{FreeSearch: true, VSIDS: true, LearntPool: 200}
	got := c.Args()
	want := []string{"-f", "--toggle-vsids", "--learnts-mlimit", "200"}
	if !sliceEq(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestSolverOptions_CoinBC(t *testing.T) {
	c := CoinBCOptions{PrintLevel: 1, RelGap: 0.01, MaxNodes: 10000}
	got := c.Args()
	want := []string{"--printLevel", "1", "--relGap", "0.01", "--maxNodes", "10000"}
	if !sliceEq(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestWithSolverOptions_Appends(t *testing.T) {
	o := &SolveOptions{}
	WithSolverOptions(GecodeOptions{NodeLimit: 100})(o)
	if !sliceEq(o.ExtraArgs, []string{"--node-limit", "100"}) {
		t.Errorf("got %v", o.ExtraArgs)
	}
}

func TestWithSolverOptions_NilNoop(t *testing.T) {
	o := &SolveOptions{}
	WithSolverOptions(nil)(o)
	if o.ExtraArgs != nil {
		t.Errorf("nil should be no-op, got %v", o.ExtraArgs)
	}
}

func TestWithCancelGrace_StoresDuration(t *testing.T) {
	o := &SolveOptions{}
	WithCancelGrace(5 * time.Second)(o)
	if o.CancelGrace != 5*time.Second {
		t.Errorf("got %v", o.CancelGrace)
	}
}

func TestRunConfigFor_DefaultsGrace(t *testing.T) {
	cfg := runConfigFor(&SolveOptions{})
	if cfg.grace != defaultCancelGrace {
		t.Errorf("expected default grace, got %v", cfg.grace)
	}
	cfg = runConfigFor(&SolveOptions{CancelGrace: 500 * time.Millisecond})
	if cfg.grace != 500*time.Millisecond {
		t.Errorf("expected user grace, got %v", cfg.grace)
	}
}

func sliceEq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
