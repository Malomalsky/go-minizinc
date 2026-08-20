# Practical Go API Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add solve-scoped parameters and concise model constructors without breaking existing solve calls.

**Architecture:** Keep `Model` as reusable source and defaults, and `Instance` as the solver-bound executor. `WithParams` snapshots one solve's data, merges it over defaults, and flows through the existing temporary JSON data file. Constructors remain thin wrappers over existing model operations.

**Tech Stack:** Go 1.21, standard library, MiniZinc JSON stream CLI, Go test, race detector, vet, golangci-lint.

---

### Task 1: Checkpoint Existing Audit Fixes

**Files:**
- Modify: `README.md`
- Modify: `builder.go`
- Modify: `builder_test.go`
- Modify: `driver.go`
- Modify: `errors.go`
- Modify: `examples/auto_solver/main.go`
- Modify: `examples/builder_nqueens/main.go`
- Modify: `examples/nqueens/main.go`
- Modify: `instance.go`
- Modify: `internal_test.go`
- Modify: `minizinc_test.go`
- Modify: `model.go`
- Modify: `options.go`
- Modify: `result.go`
- Modify: `solver.go`
- Modify: `solver_capabilities.go`
- Modify: `types.go`

- [ ] **Step 1: Verify the audited baseline**

Run:

```bash
GOCACHE=/tmp/go-minizinc-cache go test -count=1 ./...
GOCACHE=/tmp/go-minizinc-cache go test -race -count=1 ./...
GOCACHE=/tmp/go-minizinc-cache go vet ./...
GOCACHE=/tmp/go-minizinc-cache GOMODCACHE=/tmp/go-minizinc-modcache GOLANGCI_LINT_CACHE=/tmp/go-minizinc-lint-cache GOPROXY=off golangci-lint run ./...
```

Expected: all tests pass and the linter reports `0 issues.`

- [ ] **Step 2: Commit only the audited baseline**

```bash
git add README.md builder.go builder_test.go driver.go errors.go examples/auto_solver/main.go examples/builder_nqueens/main.go examples/nqueens/main.go instance.go internal_test.go minizinc_test.go model.go options.go result.go solver.go solver_capabilities.go types.go
git commit -m "fix: harden MiniZinc integration"
```

### Task 2: Add Solve-Scoped Parameters

**Files:**
- Modify: `options.go`
- Modify: `model.go`
- Modify: `instance.go`
- Test: `internal_test.go`
- Test: `minizinc_test.go`

- [ ] **Step 1: Add failing parameter tests**

Add tests covering option merging, deep-copy isolation, default override, and required parameters:

```go
func TestWithParamsSnapshotsValues(t *testing.T) {
	values := []int{1, 2}
	params := Params{"n": 2, "values": values}
	options, err := applySolveOptions(WithParams(params))
	if err != nil {
		t.Fatal(err)
	}
	values[0] = 9
	params["n"] = 3
	if options.params["n"] != 2 || options.params["values"].([]int)[0] != 1 {
		t.Fatalf("parameters were not isolated: %+v", options.params)
	}
}

func TestSolveParamsOverrideDefaults(t *testing.T) {
	model := NewModel("int: n; solve satisfy;")
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
```

- [ ] **Step 2: Run the focused tests and verify failure**

Run:

```bash
GOCACHE=/tmp/go-minizinc-cache go test -run 'TestWithParams|TestSolveParams' ./...
```

Expected: compilation fails because `Params`, `WithParams`, and the merge helpers do not exist.

- [ ] **Step 3: Implement the option snapshot**

Add to `options.go`:

```go
type Params map[string]any

func WithParams(params Params) SolveOption {
	return func(options *SolveOptions) {
		if options.params == nil {
			options.params = make(Params)
		}
		maps.Copy(options.params, params)
	}
}

func applySolveOptions(opts ...SolveOption) (*SolveOptions, error) {
	options := &SolveOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(options)
		}
	}
	if err := validateSolveOptions(options); err != nil {
		return nil, err
	}
	params, err := cloneParams(options.params)
	if err != nil {
		return nil, err
	}
	options.params = params
	return options, nil
}
```

Add an unexported `params Params` field to `SolveOptions` and import `maps`.

- [ ] **Step 4: Implement parameter validation and merging**

Add to `model.go` and update callers:

```go
func cloneParams(params Params) (Params, error) {
	if len(params) == 0 {
		return nil, nil
	}
	if _, err := json.Marshal(params); err != nil {
		return nil, wrapError("invalid parameter value", err)
	}
	cloned := make(Params, len(params))
	for name, value := range params {
		cloned[name] = deepCopyValue(value)
	}
	return cloned, nil
}

func (m *Model) missingParams(params Params) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var missing []string
	for _, name := range m.requiredParams {
		if !m.assigned[name] {
			if _, ok := params[name]; !ok {
				missing = append(missing, name)
			}
		}
	}
	return missing
}

func (m *Model) getDataJSON(params Params) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	merged := maps.Clone(m.parameters)
	if merged == nil {
		merged = make(map[string]any)
	}
	maps.Copy(merged, params)
	if len(merged) == 0 {
		return "", nil
	}
	data, err := json.Marshal(merged)
	if err != nil {
		return "", wrapError("failed to marshal parameters", err)
	}
	return string(data), nil
}
```

Change `MissingParams` to delegate to `missingParams(nil)`. Pass `options.params` to required-parameter checks and `getDataJSON`.

- [ ] **Step 5: Apply options synchronously in every solve path**

Replace the repeated option loops in `Solve`, `SolveAll`, and `SolveStream` with `applySolveOptions`. In `SolveStream`, call it before launching the worker goroutine; on error, return a channel that emits one `Result{Status: StatusError, Error: err}` and closes.

- [ ] **Step 6: Run focused and race tests**

Add an integration test in `minizinc_test.go` that uses one instance with
solve-scoped `n` values across every solve method:

```go
func TestSolveScopedParams(t *testing.T) {
	solver, err := FindSolver("gecode")
	if err != nil {
		t.Skipf("solver not found: %v", err)
	}
	model := NewModel("int: n; var 1..n: x; solve satisfy;")
	instance, err := NewInstance(model, solver)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := instance.Solve(ctx, WithParams(Params{"n": 2}))
	if err != nil {
		t.Fatal(err)
	}
	if x, err := result.GetInt("x"); err != nil || x < 1 || x > 2 {
		t.Fatalf("x=%d, err=%v", x, err)
	}
	results, err := instance.SolveAll(ctx, WithParams(Params{"n": 2}))
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d solutions", len(results))
	}
	count := 0
	for result := range instance.SolveStream(ctx, WithParams(Params{"n": 3})) {
		if result.Error != nil {
			t.Fatal(result.Error)
		}
		count++
	}
	if count != 3 {
		t.Fatalf("got %d streamed solutions", count)
	}
}
```

Run:

```bash
gofmt -w options.go model.go instance.go internal_test.go minizinc_test.go
GOCACHE=/tmp/go-minizinc-cache go test -run 'TestWithParams|TestSolveParams|TestBuilder_RequiredParams' ./...
GOCACHE=/tmp/go-minizinc-cache go test -race ./...
```

Expected: all tests pass.

- [ ] **Step 7: Commit solve-scoped parameters**

```bash
git add options.go model.go instance.go internal_test.go minizinc_test.go
git commit -m "feat: add solve-scoped parameters"
```

### Task 3: Add Concise Model Constructors

**Files:**
- Modify: `model.go`
- Test: `internal_test.go`

- [ ] **Step 1: Add failing constructor tests**

```go
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
```

- [ ] **Step 2: Run tests and verify failure**

```bash
GOCACHE=/tmp/go-minizinc-cache go test -run 'TestNewModelAcceptsFragments|TestLoadModel' ./...
```

Expected: compilation fails on the new constructor calls.

- [ ] **Step 3: Implement constructors**

```go
func NewModel(code ...string) *Model {
	return &Model{
		codeFragments: append([]string(nil), code...),
		dataFiles:     make([]string, 0),
		parameters:    make(map[string]any),
		assigned:      make(map[string]bool),
	}
}

func LoadModel(path string) (*Model, error) {
	model := NewModel()
	if err := model.AddFile(path); err != nil {
		return nil, err
	}
	return model, nil
}
```

- [ ] **Step 4: Run tests and commit**

```bash
gofmt -w model.go internal_test.go
GOCACHE=/tmp/go-minizinc-cache go test ./...
git add model.go internal_test.go
git commit -m "feat: add concise model constructors"
```

Expected: all tests pass and the constructor commit is created.

### Task 4: Refresh Logo and README

**Files:**
- Create: `docs/assets/go-minizinc-logo.png`
- Modify: `README.md`
- Modify: `examples/auto_solver/main.go`
- Modify: `examples/builder_nqueens/main.go`

- [ ] **Step 1: Create the cropped logo asset**

Use the built-in image editor with the supplied PNG as the edit target and
this exact prompt:

```text
Use case: precise-object-edit
Asset type: GitHub README project logo
Primary request: crop away the excess white space above and below the existing horizontal logo, leaving a small even white margin around the visible artwork
Input images: Image 1 is the edit target
Constraints: preserve the mascot, puzzle board, graph marks, wordmark, colors, proportions, edges, and white background exactly; crop only; no redraw; no restyling; no added text; no watermark
```

Inspect the output and copy the accepted image to
`docs/assets/go-minizinc-logo.png`.

- [ ] **Step 2: Update examples**

Use `WithParams(Params{"n": value})` in the `Solve` call instead of mutating the instance before solving. Keep explicit error handling and `Result.Decode`.

- [ ] **Step 3: Rewrite README around user workflows**

Replace the existing README structure with:

1. Centered logo and one-sentence package description.
2. Requirements and installation.
3. Copy-paste quick start using `NewModel(code...)`, `NewInstanceAuto`,
   `WithParams`, and `Result.Decode`.
4. Model loading and solve-scoped parameter examples.
5. Explicit and automatic solver selection.
6. Single, all, and streaming solution examples.
7. Typed decoding, output sections, statistics, errors, and cancellation.
8. Compatibility, examples, and test commands.

Remove marketing claims, repeated feature lists, the duplicated symbol
inventory, excessive bold text, and claims of incremental reuse. Keep the
Builder DSL and escape hatches discoverable without reproducing GoDoc.

- [ ] **Step 4: Verify the asset, examples, and README links**

```bash
test -f docs/assets/go-minizinc-logo.png
rg -n 'docs/assets/go-minizinc-logo.png|WithParams|LoadModel|Result.Decode' README.md
gofmt -w examples/auto_solver/main.go examples/builder_nqueens/main.go
GOCACHE=/tmp/go-minizinc-cache go test ./...
```

Expected: the asset exists, README references the practical API, examples
compile, and tests pass.

- [ ] **Step 5: Commit the documentation refresh**

```bash
git add README.md docs/assets/go-minizinc-logo.png examples/auto_solver/main.go examples/builder_nqueens/main.go
git commit -m "docs: refresh project README"
```

### Task 5: Full Compatibility Validation

**Files:**
- Modify only if a validation failure is caused by this feature.

- [ ] **Step 1: Run all local checks**

```bash
git diff --check
GOCACHE=/tmp/go-minizinc-cache go test -count=1 ./...
GOCACHE=/tmp/go-minizinc-cache go test -race -count=1 ./...
GOCACHE=/tmp/go-minizinc-cache go vet ./...
GOCACHE=/tmp/go-minizinc-cache GOMODCACHE=/tmp/go-minizinc-modcache GOLANGCI_LINT_CACHE=/tmp/go-minizinc-lint-cache GOPROXY=off golangci-lint run ./...
```

Expected: tests and vet pass; golangci-lint reports `0 issues.`

- [ ] **Step 2: Run platform builds**

```bash
GOOS=linux GOARCH=amd64 GOCACHE=/tmp/go-minizinc-cache GOMODCACHE=/tmp/go-minizinc-modcache GOPROXY=off go build ./...
GOOS=windows GOARCH=amd64 GOCACHE=/tmp/go-minizinc-cache GOMODCACHE=/tmp/go-minizinc-modcache GOPROXY=off go build ./...
GOOS=darwin GOARCH=arm64 GOCACHE=/tmp/go-minizinc-cache GOMODCACHE=/tmp/go-minizinc-modcache GOPROXY=off go build ./...
```

Expected: all builds exit successfully.

- [ ] **Step 3: Run MiniZinc integration tests**

```bash
PATH="/opt/homebrew/bin:$PATH" GOCACHE=/tmp/go-minizinc-cache go test -count=1 -run 'TestSimpleSolve|TestSolveWithParams|TestSolveAll|TestSolveStream|TestAutoSolver' -v
```

Expected: tests pass against the installed MiniZinc 2.10 executable.

- [ ] **Step 4: Confirm repository state**

```bash
git status --short
git log --oneline -6
```

Expected: no uncommitted source changes and separate commits for the audit, parameters, constructors, and usage documentation.
