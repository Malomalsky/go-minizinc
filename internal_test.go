package minizinc

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNewModelAcceptsFragments(t *testing.T) {
	model := NewModel("int: n;", "solve satisfy;")
	if got := model.getCode(); got != "int: n;\nsolve satisfy;\n" {
		t.Fatalf("got %q", got)
	}
}

func TestLoadModel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "model.mzn")
	if err := os.WriteFile(path, []byte("solve satisfy;"), 0o600); err != nil {
		t.Fatal(err)
	}
	model, err := LoadModel(path)
	if err != nil {
		t.Fatal(err)
	}
	if model.getCode() != "solve satisfy;\n" {
		t.Fatalf("got %q", model.getCode())
	}
}

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

func TestModelParameterIsolation(t *testing.T) {
	type payload struct {
		Values []int
	}
	original := payload{Values: []int{1, 2, 3}}
	model := NewModel()
	if err := model.SetParam("p", original); err != nil {
		t.Fatal(err)
	}
	original.Values[0] = 9
	got, _ := model.GetParam("p")
	if got.(payload).Values[0] != 1 {
		t.Fatalf("stored value changed: %v", got)
	}
	copy := got.(payload)
	copy.Values[1] = 9
	again, _ := model.GetParam("p")
	if again.(payload).Values[1] != 2 {
		t.Fatalf("returned value aliases model: %v", again)
	}
}

func TestModelRejectsCyclicParameter(t *testing.T) {
	value := map[string]any{}
	value["self"] = value
	if err := NewModel().SetParam("value", value); err == nil {
		t.Fatal("expected cyclic value error")
	}
}

func TestWithParamsSnapshotsValues(t *testing.T) {
	values := []int{1, 2}
	params := Params{"n": 2, "values": values}
	options, err := applySolveOptions(
		WithParams(Params{"first": 1, "n": 1}),
		WithParams(params),
	)
	if err != nil {
		t.Fatal(err)
	}
	values[0] = 9
	params["n"] = 3
	if options.params["first"] != 1 || options.params["n"] != 2 || options.params["values"].([]int)[0] != 1 {
		t.Fatalf("parameters were not isolated: %+v", options.params)
	}
}

func TestSolveParamsOverrideDefaults(t *testing.T) {
	model := NewModel()
	if err := model.SetParam("n", 1); err != nil {
		t.Fatal(err)
	}
	data, err := model.getDataJSON(Params{"n": 2})
	if err != nil {
		t.Fatal(err)
	}
	if data != `{"n":2}` {
		t.Fatalf("got %s", data)
	}
	value, _ := model.GetParam("n")
	if value != 1 {
		t.Fatalf("default changed to %v", value)
	}
}

func TestSolveParamsSatisfyRequiredParams(t *testing.T) {
	builder := NewBuilder()
	builder.IntParam("n")
	model := builder.Build()
	if missing := model.missingParams(Params{"n": 8}); len(missing) != 0 {
		t.Fatalf("got %v", missing)
	}
}

func TestWithParamsRejectsInvalidValue(t *testing.T) {
	cyclic := map[string]any{}
	cyclic["self"] = cyclic
	if _, err := applySolveOptions(WithParams(Params{"bad": cyclic})); err == nil {
		t.Fatal("expected parameter validation error")
	}
}

func TestSolveMethodsRejectInvalidParams(t *testing.T) {
	cyclic := map[string]any{}
	cyclic["self"] = cyclic
	option := WithParams(Params{"bad": cyclic})
	instance := &Instance{}

	if _, err := instance.Solve(context.Background(), option); err == nil {
		t.Fatal("Solve accepted invalid parameters")
	}
	if _, err := instance.SolveAll(context.Background(), option); err == nil {
		t.Fatal("SolveAll accepted invalid parameters")
	}
	results := instance.SolveStream(context.Background(), option)
	result, ok := <-results
	if !ok || result.Error == nil || result.Status != StatusError {
		t.Fatalf("got %+v, open=%v", result, ok)
	}
	if _, ok := <-results; ok {
		t.Fatal("stream emitted more than one result")
	}
}

func TestModelAddFileTracksPaths(t *testing.T) {
	dir := t.TempDir()
	modelPath := filepath.Join(dir, "model.MZN")
	dataPath := filepath.Join(dir, "data.JSON")
	if err := os.WriteFile(modelPath, []byte("solve satisfy;"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dataPath, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	model := NewModel()
	if err := model.AddFile(modelPath); err != nil {
		t.Fatal(err)
	}
	if err := model.AddFile(dataPath); err != nil {
		t.Fatal(err)
	}
	if !sliceEq(model.includeDirs, []string{dir}) {
		t.Fatalf("include dirs: %v", model.includeDirs)
	}
	if !sliceEq(model.dataFiles, []string{dataPath}) {
		t.Fatalf("data files: %v", model.dataFiles)
	}
}

func TestNewInstance_NilModel(t *testing.T) {
	if _, err := NewInstance(nil, nil); !errors.Is(err, ErrNilModel) {
		t.Fatalf("expected ErrNilModel, got %v", err)
	}
}

func TestNilDriverErrors(t *testing.T) {
	if _, err := FindSolverWithDriver("gecode", nil); !errors.Is(err, ErrNilDriver) {
		t.Fatalf("FindSolverWithDriver: %v", err)
	}
	if _, err := FindBestSolverWithDriver(SolverFilter{}, nil); !errors.Is(err, ErrNilDriver) {
		t.Fatalf("FindBestSolverWithDriver: %v", err)
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

func TestResult_Decode_Basic(t *testing.T) {
	r := &Result{
		Solution: map[string]any{
			"x":      json.Number("42"),
			"name":   "queens",
			"queens": []any{json.Number("1"), json.Number("3"), json.Number("5")},
		},
	}
	var out struct {
		X      int    `json:"x"`
		Name   string `json:"name"`
		Queens []int  `json:"queens"`
	}
	if err := r.Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.X != 42 || out.Name != "queens" || len(out.Queens) != 3 || out.Queens[2] != 5 {
		t.Fatalf("decode mismatch: %+v", out)
	}
}

func TestResult_Decode_NilResult(t *testing.T) {
	var r *Result
	var out struct{}
	if err := r.Decode(&out); err == nil {
		t.Fatal("expected error on nil receiver")
	}
}

func TestMinizincError_Format(t *testing.T) {
	e := &MinizincError{
		Stage:    "solve",
		Stderr:   "syntax error at line 1",
		ExitCode: 1,
	}
	msg := e.Error()
	if !strings.Contains(msg, "solve") || !strings.Contains(msg, "exit=1") || !strings.Contains(msg, "syntax error") {
		t.Errorf("bad message: %q", msg)
	}
}

func TestMinizincError_Unwrap(t *testing.T) {
	cause := errors.New("boom")
	e := &MinizincError{Stage: "version", Cause: cause}
	if !errors.Is(e, cause) {
		t.Fatal("Unwrap not threaded through errors.Is")
	}
}

func TestSolveAll_StatusTimingFixture(t *testing.T) {
	// Simulate the inner loop of SolveAll: three solution messages then a
	// terminal status. The synthesis logic should keep intermediates as
	// SATISFIED and only mark the last result optimal.
	msgs := []streamMessage{
		{Type: "solution", Solution: map[string]any{"x": 1}},
		{Type: "solution", Solution: map[string]any{"x": 2}},
		{Type: "solution", Solution: map[string]any{"x": 3}},
		{Type: "status", Status: StatusOptimal},
	}

	var results []*Result
	var finalStatus = StatusUnknown
	for _, msg := range msgs {
		switch msg.Type {
		case "solution":
			r, err := parseStreamMessage(msg)
			if err != nil {
				t.Fatal(err)
			}
			results = append(results, r)
		case "status":
			finalStatus = msg.Status
		}
	}
	if len(results) > 0 {
		results[len(results)-1].Status = finalStatus
	}

	if results[0].Status != StatusSatisfied || results[1].Status != StatusSatisfied {
		t.Errorf("intermediates should be SATISFIED, got %v, %v", results[0].Status, results[1].Status)
	}
	if results[2].Status != StatusOptimal {
		t.Errorf("last should be OPTIMAL_SOLUTION, got %v", results[2].Status)
	}
}

func TestWriteTempJSON_Roundtrip(t *testing.T) {
	const payload = `{"a":1,"b":[2,3,4]}`
	p, err := writeTempJSON(payload)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(p) }()
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != payload {
		t.Errorf("payload mismatch: got %q", string(data))
	}
}

func TestParseJSONStreamLargeMessage(t *testing.T) {
	large := strings.Repeat("x", 11*1024*1024)
	data, err := json.Marshal(streamMessage{
		Type:   "solution",
		Output: map[string]any{"default": large},
	})
	if err != nil {
		t.Fatal(err)
	}
	messages, err := parseJSONStream(strings.NewReader(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].Output["default"] != large {
		t.Fatal("large JSON message was not preserved")
	}
}

func TestRunJSONReturnsContextError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires a POSIX shell")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	driver := &Driver{executable: "/bin/sh"}
	_, err := driver.runJSON(ctx, []string{"-c", "sleep 5"}, runConfig{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v", err)
	}
}

func TestSolverCacheReturnsCopies(t *testing.T) {
	driver := &Driver{
		solversLoaded: true,
		solvers: []Solver{
			{
				ID:         "solver",
				Tags:       []string{"cp"},
				StdFlags:   []string{"-a"},
				ExtraFlags: []any{map[string]any{"name": "flag"}},
			},
			{ID: "without-extra-flags"},
		},
	}
	first, err := driver.listSolvers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	first[0].ID = "changed"
	first[0].Tags[0] = "changed"
	first[0].ExtraFlags[0].(map[string]any)["name"] = "changed"
	second, err := driver.listSolvers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if second[0].ID != "solver" || second[0].Tags[0] != "cp" ||
		second[0].ExtraFlags[0].(map[string]any)["name"] != "flag" {
		t.Fatalf("cache mutated through caller: %+v", second[0])
	}
	if second[1].ExtraFlags != nil {
		t.Fatalf("got extra flags: %+v", second[1].ExtraFlags)
	}
}

func TestNewInstanceCopiesSolver(t *testing.T) {
	driver := &Driver{}
	solver := &Solver{
		ID:         "solver",
		Tags:       []string{"cp"},
		ExtraFlags: []any{map[string]any{"name": "flag"}},
		driver:     driver,
	}
	instance, err := NewInstance(NewModel("solve satisfy;"), solver)
	if err != nil {
		t.Fatal(err)
	}
	solver.ID = "changed"
	solver.Tags[0] = "changed"
	solver.ExtraFlags[0].(map[string]any)["name"] = "changed"
	if instance.solver.ID != "solver" || instance.solver.Tags[0] != "cp" ||
		instance.solver.ExtraFlags[0].(map[string]any)["name"] != "flag" {
		t.Fatalf("instance solver changed: %+v", instance.solver)
	}
}

func TestDriverVersionReturnsCopy(t *testing.T) {
	driver := &Driver{version: &Version{Major: 2, Minor: 6}}
	version := driver.Version()
	version.Major = 9
	if driver.Version().Major != 2 {
		t.Fatal("driver version mutated through caller")
	}
}

func TestFindSolverForModelWithCustomDriver(t *testing.T) {
	driver := &Driver{
		solversLoaded: true,
		solvers: []Solver{{
			ID:   "mip",
			Tags: []string{"mip", "int"},
		}},
	}
	model := NewModel()
	model.AddString("array[1..2] of var 1..2: x; constraint alldifferent(x); solve satisfy;")
	solver, err := FindSolverForModelWithDriver(model, driver)
	if err != nil {
		t.Fatal(err)
	}
	if solver.ID != "mip" {
		t.Fatalf("got %q", solver.ID)
	}
}

func TestBuildArgsDefaultsToJSON(t *testing.T) {
	model := NewModel()
	model.AddString("solve satisfy;")
	if err := model.SetParam("x", 1); err != nil {
		t.Fatal(err)
	}
	inst := &Instance{model: model, solver: &Solver{ID: "test"}}
	args, _, err := inst.buildArgsLocked(&SolveOptions{
		OptimizationLevel:    0,
		HasOptimizationLevel: true,
		TimeLimit:            time.Microsecond,
	})
	defer func() { _ = inst.cleanupLocked() }()
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{"--output-mode json", "--output-output-item", "--output-objective", "-O0", "-d", "--time-limit 1"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in %q", want, joined)
		}
	}
	if strings.Contains(joined, "--cmdline-json-data") {
		t.Fatalf("parameters leaked into argv: %q", joined)
	}
}

func TestValidateSolveOptions(t *testing.T) {
	cases := []*SolveOptions{
		{OutputMode: "xml"},
		{NumSolutions: -1},
		{AllSolutions: true, NumSolutions: 1},
		{TimeLimit: -time.Second},
		{Processes: -1},
		{OptimizationLevel: 6, HasOptimizationLevel: true},
		{RandomSeed: -1, HasRandomSeed: true},
		{CancelGrace: -time.Second, HasCancelGrace: true},
	}
	for _, options := range cases {
		if err := validateSolveOptions(options); err == nil {
			t.Fatalf("expected validation error for %+v", options)
		}
	}
}

func TestBuildArgsRejectsUnsupportedEnumeration(t *testing.T) {
	instance := &Instance{
		model:  NewModel("var 1..2: x; solve satisfy;"),
		solver: &Solver{ID: "mip", StdFlags: []string{"-p", "-s"}},
	}
	defer func() { _ = instance.cleanupLocked() }()

	for _, options := range []*SolveOptions{
		{AllSolutions: true},
		{NumSolutions: 2},
	} {
		if _, _, err := instance.buildArgsLocked(options); err == nil {
			t.Fatalf("expected unsupported enumeration error for %+v", options)
		}
	}

	args, _, err := instance.buildArgsLocked(&SolveOptions{NumSolutions: 1})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(args, " "), "--num-solutions") {
		t.Fatalf("single solution should use the solver default: %v", args)
	}
}

func TestStatusUnsatOrUnboundedValue(t *testing.T) {
	if StatusUnsatOrUnbounded != "UNSAT_OR_UNBOUNDED" {
		t.Fatalf("got %q", StatusUnsatOrUnbounded)
	}
}

func TestResultGetIntRejectsNonInteger(t *testing.T) {
	result := &Result{Solution: map[string]any{"x": 1.5}}
	if _, err := result.GetInt("x"); err == nil {
		t.Fatal("expected fractional value error")
	}
}

func TestHitTimeLimit(t *testing.T) {
	cases := []struct {
		options   SolveOptions
		status    Status
		solutions int
		solveType SolveType
		want      bool
	}{
		{options: SolveOptions{TimeLimit: time.Second}, solutions: 0, want: true},
		{options: SolveOptions{TimeLimit: time.Second}, solutions: 1, solveType: SolveTypeSatisfy},
		{options: SolveOptions{TimeLimit: time.Second}, solutions: 1, solveType: SolveTypeMaximize, want: true},
		{options: SolveOptions{TimeLimit: time.Second, AllSolutions: true}, solutions: 1, want: true},
		{options: SolveOptions{TimeLimit: time.Second, NumSolutions: 2}, solutions: 1, want: true},
		{options: SolveOptions{TimeLimit: time.Second, NumSolutions: 1}, solutions: 1},
		{options: SolveOptions{TimeLimit: time.Second}, status: StatusOptimal, solutions: 1, solveType: SolveTypeMaximize},
	}
	for _, test := range cases {
		if got := hitTimeLimit(&test.options, test.status, test.solutions, test.solveType); got != test.want {
			t.Fatalf("got %v for %+v", got, test)
		}
	}
}

func TestInstance_Cleanup_RemovesMultipleTempFiles(t *testing.T) {
	f1, err := os.CreateTemp("", "mz-clean-*.mzn")
	if err != nil {
		t.Fatal(err)
	}
	f2, err := os.CreateTemp("", "mz-clean-*.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := f1.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f2.Close(); err != nil {
		t.Fatal(err)
	}

	inst := &Instance{
		tempFile:  f1.Name(),
		tempFiles: []string{f2.Name()},
	}
	if err := inst.Cleanup(); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{f1.Name(), f2.Name()} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("expected %s removed, stat err=%v", p, err)
		}
	}
	if inst.tempFile != "" || inst.tempFiles != nil {
		t.Errorf("cleanup did not reset state: %+v", inst)
	}
}

func TestWithCommandHook_StoresHook(t *testing.T) {
	o := &SolveOptions{}
	var captured []string
	WithCommandHook(func(args []string) {
		captured = args
	})(o)
	if o.CommandHook == nil {
		t.Fatal("hook not stored")
	}
	o.CommandHook([]string{"a", "b"})
	if len(captured) != 2 {
		t.Fatalf("hook not invoked: %v", captured)
	}
}

func TestSolveMethodsAttachDiagnostics(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires a POSIX shell")
	}

	for _, withSolution := range []bool{true, false} {
		t.Run(map[bool]string{true: "solution", false: "no_solution"}[withSolution], func(t *testing.T) {
			instance := newDiagnosticFixtureInstance(t, withSolution)

			result, err := instance.Solve(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			assertDiagnostics(t, result)

			results, err := instance.SolveAll(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			wantResults := 1
			if withSolution {
				wantResults = 2
			}
			if len(results) != wantResults {
				t.Fatalf("SolveAll returned %d results", len(results))
			}
			for _, result := range results[:len(results)-1] {
				assertNoDiagnostics(t, result)
			}
			assertDiagnostics(t, results[len(results)-1])

			var streamed []*Result
			for result := range instance.SolveStream(context.Background()) {
				if result.Error != nil {
					t.Fatal(result.Error)
				}
				streamed = append(streamed, result)
			}
			if len(streamed) != wantResults {
				t.Fatalf("SolveStream returned %d results", len(streamed))
			}
			for _, result := range streamed[:len(streamed)-1] {
				if !result.IsIntermediate {
					t.Fatal("intermediate result marked terminal")
				}
				assertNoDiagnostics(t, result)
			}
			if streamed[len(streamed)-1].IsIntermediate {
				t.Fatal("terminal result marked intermediate")
			}
			assertDiagnostics(t, streamed[len(streamed)-1])
		})
	}
}

func TestSolveDiagnosticsCopiesPayloads(t *testing.T) {
	warningLocation := map[string]any{"filename": "before.mzn"}
	warningFrame := map[string]any{"description": "before"}
	checkerOutput := map[string]any{"nested": map[string]any{"value": "before"}}
	checkerStats := map[string]any{"checks": json.Number("1")}
	checkerLocation := map[string]any{"filename": "checker.mzn"}
	checkerFrame := map[string]any{"description": "checker"}

	var diagnostics solveDiagnostics
	diagnostics.add(streamMessage{
		Type:     "warning",
		Message:  "careful",
		Location: warningLocation,
		Stack:    []any{warningFrame},
	})
	diagnostics.add(streamMessage{
		Type: "checker",
		Messages: []streamMessage{{
			Type:       "warning",
			Output:     checkerOutput,
			Sections:   []string{"default"},
			Statistics: checkerStats,
			What:       "checker warning",
			Message:    "checked",
			Location:   checkerLocation,
			Stack:      []any{checkerFrame},
		}},
	})

	warningLocation["filename"] = "after.mzn"
	warningFrame["description"] = "after"
	checkerOutput["nested"].(map[string]any)["value"] = "after"
	checkerStats["checks"] = json.Number("2")
	checkerLocation["filename"] = "after.mzn"
	checkerFrame["description"] = "after"

	result := &Result{}
	diagnostics.attach(result)
	assertDiagnostics(t, result)
}

func TestSolveStreamAttachesDiagnosticsToError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires a POSIX shell")
	}
	instance := newJSONFixtureInstance(t, []string{
		`{"type":"warning","message":"before failure"}`,
		`{`,
	})
	var results []*Result
	for result := range instance.SolveStream(context.Background()) {
		results = append(results, result)
	}
	if len(results) != 1 || results[0].Error == nil {
		t.Fatalf("got %+v", results)
	}
	if len(results[0].Warnings) != 1 || results[0].Warnings[0].Message != "before failure" {
		t.Fatalf("warnings got %+v", results[0].Warnings)
	}
}

func newDiagnosticFixtureInstance(t *testing.T, withSolution bool) *Instance {
	t.Helper()
	messages := []string{
		`{"type":"warning","message":"careful","location":{"filename":"before.mzn"},"stack":[{"description":"before"}]}`,
		`{"type":"checker","messages":[{"type":"warning","output":{"nested":{"value":"before"}},"sections":["default"],"statistics":{"checks":1},"what":"checker warning","message":"checked","location":{"filename":"checker.mzn"},"stack":[{"description":"checker"}]}]}`,
	}
	if withSolution {
		messages = append(messages,
			`{"type":"solution","output":{"json":{"x":1}},"sections":["json"]}`,
			`{"type":"solution","output":{"json":{"x":1}},"sections":["json"]}`,
			`{"type":"status","status":"ALL_SOLUTIONS"}`,
		)
	}
	return newJSONFixtureInstance(t, messages)
}

func newJSONFixtureInstance(t *testing.T, messages []string) *Instance {
	t.Helper()
	path := filepath.Join(t.TempDir(), "minizinc-fixture")
	script := "#!/bin/sh\ncat <<'EOF'\n" + strings.Join(messages, "\n") + "\nEOF\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	driver := &Driver{
		executable: path,
		version:    &Version{Major: 2, Minor: 10},
	}
	solver := &Solver{
		ID:       "fixture",
		Name:     "fixture",
		StdFlags: []string{"-a", "-n"},
		driver:   driver,
	}
	instance, err := NewInstance(NewModel("var 1..1: x; solve satisfy;"), solver)
	if err != nil {
		t.Fatal(err)
	}
	return instance
}

func assertNoDiagnostics(t *testing.T, result *Result) {
	t.Helper()
	if len(result.Warnings) != 0 || len(result.CheckerMessages) != 0 {
		t.Fatalf("unexpected diagnostics: %+v %+v", result.Warnings, result.CheckerMessages)
	}
}

func assertDiagnostics(t *testing.T, result *Result) {
	t.Helper()
	if len(result.Warnings) != 1 {
		t.Fatalf("got %d warnings", len(result.Warnings))
	}
	warning := result.Warnings[0]
	if warning.Message != "careful" {
		t.Fatalf("warning got %+v", warning)
	}
	if warning.Location.(map[string]any)["filename"] != "before.mzn" {
		t.Fatalf("warning location got %+v", warning.Location)
	}
	if warning.Stack[0].(map[string]any)["description"] != "before" {
		t.Fatalf("warning stack got %+v", warning.Stack)
	}

	if len(result.CheckerMessages) != 1 {
		t.Fatalf("got %d checker messages", len(result.CheckerMessages))
	}
	checker := result.CheckerMessages[0]
	if checker.Type != "warning" || checker.What != "checker warning" || checker.Message != "checked" {
		t.Fatalf("checker got %+v", checker)
	}
	if !reflect.DeepEqual(checker.SectionOrder, []string{"default"}) {
		t.Fatalf("checker sections got %v", checker.SectionOrder)
	}
	if checker.Output["nested"].(map[string]any)["value"] != "before" {
		t.Fatalf("checker output got %+v", checker.Output)
	}
	if checker.Statistics["checks"] != json.Number("1") {
		t.Fatalf("checker statistics got %+v", checker.Statistics)
	}
	if checker.Location.(map[string]any)["filename"] != "checker.mzn" {
		t.Fatalf("checker location got %+v", checker.Location)
	}
	if checker.Stack[0].(map[string]any)["description"] != "checker" {
		t.Fatalf("checker stack got %+v", checker.Stack)
	}
}
