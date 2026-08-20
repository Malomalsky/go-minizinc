# Practical Go API Design

## Goal

Make common MiniZinc solves concise while preserving the existing `Model`,
`Instance`, `SolveOption`, and `Result` API.

The primary workflow is a reusable model and solver-bound instance with data
provided for each solve:

```go
model := minizinc.NewModel(`
    int: n;
    var 1..n: x;
    solve maximize x;
`)

instance, err := minizinc.NewInstanceAuto(model)
if err != nil {
    return err
}

result, err := instance.Solve(ctx,
    minizinc.WithParams(minizinc.Params{"n": 10}),
    minizinc.WithTimeLimit(5*time.Second),
)
if err != nil {
    return err
}

var solution struct {
    X int `json:"x"`
}
if err := result.Decode(&solution); err != nil {
    return err
}
```

## Public API

```go
type Params map[string]any

func WithParams(params Params) SolveOption
func NewModel(code ...string) *Model
func LoadModel(path string) (*Model, error)
```

Existing direct calls to `NewModel()` remain source-compatible.

`WithParams` works with `Solve`, `SolveAll`, and `SolveStream`. No parallel
`SolveWith`, `SolveAllWith`, or `SolveStreamWith` method family is added.

## Parameter Semantics

Values set through `Model.SetParam` or `Instance.SetParam` are defaults stored
on that object. Values supplied through `WithParams` apply only to one solve
and override defaults with the same name.

The merged parameter set is validated as JSON before MiniZinc starts. Invalid
values return an error from `Solve` and `SolveAll`; `SolveStream` emits one
error result and closes its channel.

Solve-time parameters are copied before process execution. Mutating the input
map or nested values after a solve starts cannot change that solve or later
solves.

Required parameters declared by `Builder` are checked against the merged
defaults and solve-time parameters.

## Model Construction

`NewModel(code ...string)` appends fragments in argument order. It remains
valid to create an empty model and call `AddString` manually.

`LoadModel(path)` is equivalent to creating a model and calling `AddFile`.
It accepts the same `.mzn`, `.dzn`, and `.json` paths and returns the same
errors.

## Concurrency

An `Instance` continues to serialize its solve methods. Per-solve parameters
do not mutate instance state and cannot leak between calls. Callers that need
parallel solves create separate instances from the same model and solver.

`SolveStream` applies and snapshots options before launching its goroutine so
captured maps and slices are not first read asynchronously after the method
returns.

## Errors

No new error hierarchy is introduced. Parameter encoding and model loading
reuse the package's existing wrapped errors. MiniZinc process failures remain
`*MinizincError` values.

## Results

`Result.Decode` remains the preferred typed solution API. No generic getters,
reflection-based solver methods, or generated solution types are added.

## Compatibility

Existing `SetParam`, solve methods, options, and result accessors keep their
behavior. Changing `NewModel` to a variadic function preserves direct calls,
but code storing it as an exact `func() *Model` value must adapt.

The implementation continues to support Go 1.21 and MiniZinc 2.6 or newer on
the operating systems supported by the existing process driver.

## Documentation

The README quick start will use `NewModel(code...)`, `NewInstanceAuto`,
`WithParams`, and `Result.Decode`. Stateful defaults and explicit solver
selection remain documented as advanced workflows.

## Tests

Tests cover parameter precedence, required-parameter validation, deep-copy
isolation, invalid JSON values, all three solve methods, ordered model
fragments, file loading, race detection, linting, and cross-platform builds.

## Non-Goals

- No `Client`, `Session`, `Runner`, or new interface abstraction.
- No package-level wrappers duplicating every `Instance` solve method.
- No mutable parameter overwrite or Python-style branch object.
- No incremental compilation claim; the CLI driver still starts MiniZinc for
  each solve.
- No breaking solve-method signatures.
