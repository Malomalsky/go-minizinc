package minizinc

import (
	"encoding/json"
	"errors"
	"math"
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
	for _, name := range []string{"123bad", "_", "_1", "__x", "default", "infinity", "solve", "type", "variant_record"} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Fatal("expected panic on invalid identifier")
				}
			}()
			NewBuilder().IntVar(name, 1, 10)
		})
	}
}

func TestBuilder_ValidIdentifiers(t *testing.T) {
	builder := NewBuilder()
	for _, name := range []string{"op", "_x", "x_1"} {
		builder.IntVar(name, 1, 10)
	}
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

func TestFormatFloatRejectsNonFiniteValues(t *testing.T) {
	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		func() {
			defer func() {
				if r := recover(); r == nil {
					t.Fatalf("expected panic for %v", value)
				}
			}()
			Float(value)
		}()
	}
}

func TestSumAcceptsArrayExpression(t *testing.T) {
	if got := Sum(Var("xs")).String(); got != "sum(xs)" {
		t.Fatalf("got %q", got)
	}
}

func TestSolverOptions_Gecode(t *testing.T) {
	g := GecodeOptions{
		RestartStrategy: "luby",
		RestartScale:    100,
		RestartBase:     1.5,
		NodeLimit:       5000,
		FailLimit:       4000,
		TimeLimitMS:     3000,
	}
	got := g.Args()
	want := []string{
		"--restart", "luby",
		"--restart-scale", "100",
		"--restart-base", "1.5",
		"--node", "5000",
		"--fail", "4000",
		"-t", "3000",
	}
	if !sliceEq(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestSolverOptions_Chuffed(t *testing.T) {
	c := ChuffedOptions{FreeSearch: true, VSIDS: true, EagerLazyFD: true, EagerLimit: 1000, LearntPool: 200}
	got := c.Args()
	want := []string{"-f", "--fzn-flags", "--vsids --lazy on --eager-limit 1000 --n-of-learnts 200"}
	if !sliceEq(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestSolverOptions_CoinBC(t *testing.T) {
	c := CoinBCOptions{PrintLevel: 1, RelGap: 0.01, MaxNodes: 10000}
	got := c.Args()
	want := []string{"--cbc-logLevel", "1", "--relGap", "0.01", "--cbc-maxNodes", "10000"}
	if !sliceEq(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestWithSolverOptions_Appends(t *testing.T) {
	o := &SolveOptions{}
	WithSolverOptions(GecodeOptions{NodeLimit: 100})(o)
	if !sliceEq(o.ExtraArgs, []string{"--node", "100"}) {
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
	cfg = runConfigFor(&SolveOptions{CancelGrace: 500 * time.Millisecond, HasCancelGrace: true})
	if cfg.grace != 500*time.Millisecond {
		t.Errorf("expected user grace, got %v", cfg.grace)
	}
	// WithCancelGrace(0) must disable, not fall back to default
	cfg = runConfigFor(&SolveOptions{HasCancelGrace: true})
	if cfg.grace != 0 {
		t.Errorf("expected disabled (0), got %v", cfg.grace)
	}
}

func TestBuilder_IntArray2D(t *testing.T) {
	b := NewBuilder()
	m := b.IntArray2D("m", 3, 4, 0, 9)
	b.Constraint(m.At2(Int(1), Int(2)).Eq(Int(5)))
	b.Satisfy()

	got := b.Build().getCode()
	for _, want := range []string{
		"array[1..3, 1..4] of var 0..9: m;",
		"constraint (m[1,2] = 5);",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestBuilder_SetVarAndParam(t *testing.T) {
	b := NewBuilder()
	s := b.IntSetVar("s", 1, 10)
	p := b.IntSetParam("p")
	_ = s
	_ = p
	b.Satisfy()
	got := b.Build().getCode()
	for _, want := range []string{
		"var set of 1..10: s;",
		"set of int: p;",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestBuilder_AnnotationsOnSolve(t *testing.T) {
	b := NewBuilder()
	xs := b.IntArray("xs", 5, 1, 5)
	ann := IntSearch(xs, "first_fail", "indomain_min", "")
	b.Satisfy(ann)
	got := b.Build().getCode()
	want := "solve :: int_search(xs, first_fail, indomain_min, complete) satisfy;"
	if !strings.Contains(got, want) {
		t.Errorf("missing %q in:\n%s", want, got)
	}
}

func TestBuilder_SeqSearch(t *testing.T) {
	b := NewBuilder()
	xs := b.IntArray("xs", 3, 1, 3)
	ys := b.IntArray("ys", 3, 1, 3)
	a1 := IntSearch(xs, "first_fail", "indomain_min", "")
	a2 := IntSearch(ys, "input_order", "indomain_max", "")
	b.Maximize(Sum(xs), SeqSearch(a1, a2))
	got := b.Build().getCode()
	want := "seq_search(["
	if !strings.Contains(got, want) {
		t.Errorf("missing seq_search wrapper in:\n%s", got)
	}
}

func TestClassifyStderr(t *testing.T) {
	cases := []struct {
		stderr string
		want   ErrorCategory
	}{
		{"", CategoryUnknown},
		{"Error: type error: x has wrong type", CategoryType},
		{"syntax error at line 1", CategorySyntax},
		{"Time limit exceeded", CategoryTimeout},
		{"Solver timed out at 30s", CategoryTimeout},
		{"Aborted by signal", CategoryRuntime},
		{"Segmentation fault", CategoryRuntime},
		{"random other message", CategoryUnknown},
	}
	for _, tc := range cases {
		if got := classifyStderr(tc.stderr); got != tc.want {
			t.Errorf("classify(%q): got %v, want %v", tc.stderr, got, tc.want)
		}
	}
}

func TestBuilder_Comprehension(t *testing.T) {
	i := Var("i")
	got := Comprehension(i.Mul(Int(2)), In(i, Range(Int(1), Int(5))))
	want := "[(i * 2) | i in 1..5]"
	if got.code != want {
		t.Errorf("got %q, want %q", got.code, want)
	}
}

func TestBuilder_ForallGWhere(t *testing.T) {
	i, j := Var("i"), Var("j")
	body := i.Lt(j)
	got := ForallG(body,
		In(i, Range(Int(1), Int(5))),
		InWhere(j, Range(Int(1), Int(5)), i.Lt(j)))
	want := "forall(i in 1..5, j in 1..5 where (i < j))((i < j))"
	if got.code != want {
		t.Errorf("got %q, want %q", got.code, want)
	}
}

func TestBuilder_ConjAndDisj(t *testing.T) {
	a, b, c := Var("a"), Var("b"), Var("c")
	conj := ConjOf(a, b, c)
	if conj.code != `(a /\ b /\ c)` {
		t.Errorf("conj: %q", conj.code)
	}
	disj := DisjOf(a, b)
	if disj.code != `(a \/ b)` {
		t.Errorf("disj: %q", disj.code)
	}
	if ConjOf().code != "true" {
		t.Error("empty ConjOf should be true")
	}
	if DisjOf().code != "false" {
		t.Error("empty DisjOf should be false")
	}
	if ConjOf(a).code != "a" {
		t.Error("single ConjOf should unwrap")
	}
}

func TestBuilder_MoreSearchAnnotations(t *testing.T) {
	xs := Var("xs")
	cases := []struct {
		got  Annotation
		want string
	}{
		{BoolSearch(xs, "input_order", "indomain_min", ""), "bool_search(xs, input_order, indomain_min, complete)"},
		{SetSearch(xs, "input_order", "indomain_min", ""), "set_search(xs, input_order, indomain_min, complete)"},
		{FloatSearch(xs, 0.001, "input_order", "indomain_min", ""), "float_search(xs, 0.001, input_order, indomain_min, complete)"},
	}
	for _, tc := range cases {
		if tc.got.code != tc.want {
			t.Errorf("got %q, want %q", tc.got.code, tc.want)
		}
	}
}

func TestResult_Sections(t *testing.T) {
	msg := streamMessage{
		Type: "solution",
		Output: map[string]any{
			"dzn":      "x = 1;",
			"explain":  "x picked because of constraint c1",
			"checker":  "OK",
			"badField": 42, // non-string ignored
		},
	}
	r, err := parseStreamMessage(msg)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := r.Section("explain"); !ok || !strings.Contains(v, "constraint") {
		t.Errorf("explain section missing or wrong: %q", v)
	}
	if _, ok := r.Section("dzn"); ok {
		t.Error("dzn should not be exposed as a section")
	}
	if _, ok := r.Section("badField"); ok {
		t.Error("non-string field should be skipped")
	}
}

func TestBuilder_RequiredParamsExposedOnModel(t *testing.T) {
	b := NewBuilder()
	b.IntParam("n")
	b.IntParam("k")
	b.IntVar("x", 1, 10) // not a param
	b.Satisfy()
	m := b.Build()

	got := m.MissingParams()
	if !sliceEq(got, []string{"n", "k"}) {
		t.Errorf("missing should list both params, got %v", got)
	}
}

func TestBuilder_MissingParamsClearedAsSet(t *testing.T) {
	b := NewBuilder()
	b.IntParam("n")
	b.IntParam("k")
	b.Satisfy()
	m := b.Build()
	if err := m.SetParam("n", 8); err != nil {
		t.Fatal(err)
	}
	missing := m.MissingParams()
	if !sliceEq(missing, []string{"k"}) {
		t.Errorf("got %v, want [k]", missing)
	}
	_ = m.SetParam("k", 3)
	if got := m.MissingParams(); len(got) != 0 {
		t.Errorf("expected none, got %v", got)
	}
}

func TestMissingParamsError_Message(t *testing.T) {
	e := &MissingParamsError{Missing: []string{"n", "k"}}
	if !strings.Contains(e.Error(), "n, k") {
		t.Errorf("bad message: %s", e.Error())
	}
}

func TestAnalyzeModel_SolveBoundaries(t *testing.T) {
	// Identifier containing the literal "solve maximize" cannot exist because
	// MiniZinc identifiers do not have spaces — but with word boundaries we
	// guard against any other substring trap of the same shape (e.g. inside
	// stripped string artifacts that survived).
	m := NewModel()
	m.AddString(`
		var 1..10: solve_max_count;
		solve satisfy;
	`)
	a := analyzeModel(m)
	if a.SolveType != SolveTypeSatisfy {
		t.Errorf("expected satisfy, got %v", a.SolveType)
	}

	m2 := NewModel()
	m2.AddString(`
		var 1..10: x;
		solve maximize x;
	`)
	if analyzeModel(m2).SolveType != SolveTypeMaximize {
		t.Error("real solve maximize should match")
	}
}

func TestAnalyzeModel_FloatWordBoundary(t *testing.T) {
	// `floatation_count` should NOT trigger float capability.
	m := NewModel()
	m.AddString(`
		var 1..10: floatation_count;
		solve satisfy;
	`)
	a := analyzeModel(m)
	if a.UsesFloats {
		t.Error("substring 'float' in identifier should not trigger UsesFloats")
	}

	// Real float var should trigger.
	m2 := NewModel()
	m2.AddString(`
		var float: y;
		solve satisfy;
	`)
	if !analyzeModel(m2).UsesFloats {
		t.Error("real var float should trigger UsesFloats")
	}
}

func TestParseStreamMessage_FallbackToDefaultSection(t *testing.T) {
	// MiniZinc emits formatted dzn-like text under "default" when the model
	// has its own output [...] block. parseStreamMessage should fall back
	// to parsing that as DZN.
	msg := streamMessage{
		Type: "solution",
		Output: map[string]any{
			"default": "x = 42;\n",
		},
	}
	r, err := parseStreamMessage(msg)
	if err != nil {
		t.Fatal(err)
	}
	v, ok := r.Solution["x"]
	if !ok {
		t.Fatalf("Solution should contain x: %v", r.Solution)
	}
	if n, ok := v.(json.Number); !ok || n.String() != "42" {
		t.Errorf("expected x=42, got %v (%T)", v, v)
	}

	// Non-DZN-shaped "default" leaves Solution empty.
	msg2 := streamMessage{
		Type:   "solution",
		Output: map[string]any{"default": "hello world\n"},
	}
	r2, _ := parseStreamMessage(msg2)
	if len(r2.Solution) != 0 {
		t.Errorf("non-DZN default should leave Solution empty, got %v", r2.Solution)
	}
}

func TestParseStreamMessageJSONOutput(t *testing.T) {
	msg := streamMessage{
		Type:     "solution",
		Sections: []string{"json", "explain"},
		Output: map[string]any{
			"json": map[string]any{
				"x":   json.Number("9007199254740993"),
				"set": []any{json.Number("1"), json.Number("3")},
			},
			"explain": "ok",
			"typed":   map[string]any{"value": true},
		},
	}
	result, err := parseStreamMessage(msg)
	if err != nil {
		t.Fatal(err)
	}
	if result.Solution["x"].(json.Number).String() != "9007199254740993" {
		t.Fatalf("integer precision lost: %v", result.Solution["x"])
	}
	if result.Output["typed"].(map[string]any)["value"] != true {
		t.Fatalf("typed output lost: %v", result.Output)
	}
	if !sliceEq(result.SectionOrder, []string{"json", "explain"}) {
		t.Fatalf("section order lost: %v", result.SectionOrder)
	}
}

func TestStatisticsKeepsUnknownFields(t *testing.T) {
	stats := parseStatistics(map[string]any{
		"nodes":        json.Number("5"),
		"solverCustom": "value",
	})
	if stats.Nodes != 5 || stats.Raw["solverCustom"] != "value" {
		t.Fatalf("statistics lost fields: %+v", stats)
	}
}

func TestStatisticsMessagesMerge(t *testing.T) {
	first := parseStatistics(map[string]any{"flatTime": json.Number("0.5")})
	second := parseStatistics(map[string]any{"nodes": json.Number("7")})
	merged := mergeStatistics(first, second)
	if merged.FlatTime != 500*time.Millisecond || merged.Nodes != 7 {
		t.Fatalf("statistics did not merge: %+v", merged)
	}
	copy := cloneStatistics(merged)
	copy.Raw["nodes"] = json.Number("9")
	if merged.Raw["nodes"].(json.Number).String() != "7" {
		t.Fatal("statistics clone shares raw map")
	}
}

func TestParseDZNRejectsTrailingSyntax(t *testing.T) {
	value := parseDZN("x = 1..3;")["x"]
	if value != "1..3" {
		t.Fatalf("got %v (%T)", value, value)
	}
}

func TestBuilderIntArray2DParamIsRequired(t *testing.T) {
	builder := NewBuilder()
	rows := builder.IntParam("rows")
	cols := builder.IntParam("cols")
	builder.IntArray2DParamSized("matrix", rows, cols)
	missing := builder.Build().MissingParams()
	if !sliceEq(missing, []string{"rows", "cols", "matrix"}) {
		t.Fatalf("got %v", missing)
	}
}

func TestAnalyzeModelAllowsWhitespaceBeforeCall(t *testing.T) {
	model := NewModel()
	model.AddString("array[1..2] of var 1..2: x; constraint alldifferent (x); solve satisfy;")
	if !analyzeModel(model).HasGlobalConstraints {
		t.Fatal("global constraint was not detected")
	}
}

func TestAnalyzeModelIgnoresQuotedIdentifier(t *testing.T) {
	model := NewModel()
	model.AddString("var 1..2: 'alldifferent (fake)'; solve satisfy;")
	if analyzeModel(model).HasGlobalConstraints {
		t.Fatal("quoted identifier triggered global detection")
	}
}

func TestVarRejectsInvalidIdentifier(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	Var("not valid")
}

func TestResult_HitTimeLimit_Field(t *testing.T) {
	r := &Result{HitTimeLimit: true, Status: StatusUnknown}
	if !r.HitTimeLimit {
		t.Fatal("flag not honored")
	}
}

func TestCollectStreamErrors(t *testing.T) {
	msgs := []streamMessage{
		{Type: "solution", Solution: map[string]any{"x": 1}},
		{Type: "error", What: "syntax error", Message: "unexpected token"},
		{Type: "error", What: "type error", Message: "x has wrong type"},
	}
	got := collectStreamErrors(msgs)
	if !strings.Contains(got, "syntax error: unexpected token") {
		t.Errorf("missing syntax error: %q", got)
	}
	if !strings.Contains(got, "type error: x has wrong type") {
		t.Errorf("missing type error: %q", got)
	}
}

func TestCombineErrorText(t *testing.T) {
	streamErr := []streamMessage{{Type: "error", What: "syntax error", Message: "x"}}
	if got := combineErrorText(nil, ""); got != "" {
		t.Errorf("both empty -> %q", got)
	}
	if got := combineErrorText(streamErr, ""); got != "syntax error: x" {
		t.Errorf("stream only -> %q", got)
	}
	if got := combineErrorText(nil, "stderr text"); got != "stderr text" {
		t.Errorf("stderr only -> %q", got)
	}
	if got := combineErrorText(streamErr, "stderr text"); got != "syntax error: x\nstderr text" {
		t.Errorf("both -> %q", got)
	}
}

func TestClassifyStderr_FromStreamMessage(t *testing.T) {
	// The driver routes type=="error" payload into Stderr via combineErrorText
	// before classify runs, so the heuristic still catches it.
	cat := classifyStderr("syntax error: unexpected token")
	if cat != CategorySyntax {
		t.Errorf("expected Syntax, got %v", cat)
	}
}

func TestAnalyzeModel_GlobalRequiresFunctionCall(t *testing.T) {
	// Identifier containing the keyword must not trigger a global match.
	m := NewModel()
	m.AddString(`
		var 1..10: alldifferent_count;
		constraint alldifferent_count > 0;
		solve satisfy;
	`)
	a := analyzeModel(m)
	if a.HasGlobalConstraints {
		t.Errorf("identifier with substring should not trigger globals: %+v", a.GlobalConstraintsUsed)
	}

	// Real call must still trigger.
	m2 := NewModel()
	m2.AddString(`
		array[1..3] of var 1..3: q;
		constraint alldifferent(q);
		solve satisfy;
	`)
	a2 := analyzeModel(m2)
	if !a2.HasGlobalConstraints {
		t.Error("real alldifferent(q) call should trigger")
	}
}

func TestWithCancelGrace_ZeroDisables(t *testing.T) {
	o := &SolveOptions{}
	WithCancelGrace(0)(o)
	if !o.HasCancelGrace {
		t.Fatal("HasCancelGrace must be true after explicit WithCancelGrace(0)")
	}
	if o.CancelGrace != 0 {
		t.Fatalf("grace = %v, want 0", o.CancelGrace)
	}
	cfg := runConfigFor(o)
	if cfg.grace != 0 {
		t.Errorf("expected grace 0 (disabled), got %v", cfg.grace)
	}
}

func TestWithModelViaStdin_SetsFlag(t *testing.T) {
	o := &SolveOptions{}
	WithModelViaStdin()(o)
	if !o.ModelViaStdin {
		t.Fatal("flag not set")
	}
}

func TestMinizincError_Is_BranchesByCategory(t *testing.T) {
	cases := []struct {
		cat    ErrorCategory
		target error
	}{
		{CategoryTimeout, ErrTimeout},
		{CategorySyntax, ErrSyntax},
		{CategoryType, ErrType},
		{CategoryRuntime, ErrRuntime},
	}
	for _, tc := range cases {
		e := &MinizincError{Category: tc.cat}
		if !errors.Is(e, tc.target) {
			t.Errorf("category=%v should match %v", tc.cat, tc.target)
		}
	}
	other := &MinizincError{Category: CategoryUnknown}
	if errors.Is(other, ErrTimeout) {
		t.Error("Unknown category should not match ErrTimeout")
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
