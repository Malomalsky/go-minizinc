# go-minizinc

Go bindings for MiniZinc constraint solver.

## Installation

```bash
go get github.com/Malomalsky/go-minizinc
```

## Requirements

- Go 1.21 or higher
- MiniZinc 2.6.0 or higher installed and available in PATH

## Quick Start

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    "github.com/Malomalsky/go-minizinc"
)

func main() {
    solver, err := minizinc.FindSolver("coin-bc")
    if err != nil {
        log.Fatal(err)
    }

    model := minizinc.NewModel()
    model.AddString("var 1..10: x; solve maximize x;")

    instance := minizinc.NewInstance(model, solver)

    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    result, err := instance.Solve(ctx)
    if err != nil {
        log.Fatal(err)
    }

    x, _ := result.GetInt("x")
    fmt.Printf("x = %d\n", x)
}
```

## Features

- Full MiniZinc functionality via CLI integration
- Go-idiomatic API with context support
- **Smart solver selection** based on model analysis
- Streaming solutions via channels
- Type-safe result access
- Functional options pattern
- Thread-safe operations
- Model analysis and capability detection

## Usage

### Creating Models

```go
model := minizinc.NewModel()

model.AddString(`
    int: n;
    array[1..n] of var 1..n: queens;
    constraint alldifferent(queens);
    solve satisfy;
`)

model.AddFile("model.mzn")
```

### Setting Parameters

```go
instance := minizinc.NewInstance(model, solver)
instance.SetParam("n", 8)
instance.SetParam("values", []int{1, 2, 3})
```

### Finding Solvers

**Manual selection:**
```go
solver, err := minizinc.FindSolver("coin-bc")
```

**Automatic selection:**
```go
instance, err := minizinc.NewInstanceAuto(model)
```

**With warnings:**
```go
solver, warnings, err := minizinc.FindSolverForModelWithWarnings(model)
for _, w := range warnings {
    log.Printf("Warning: %s", w)
}
```

**List all solvers:**
```go
solvers, err := minizinc.ListSolvers()
for _, s := range solvers {
    fmt.Printf("%s: %s\n", s.Name, s.Version)
}
```

**Advanced filtering:**
```go
filter := minizinc.SolverFilter{
    RequiredTags: []string{"mip"},
    PreferredTags: []string{"coin-bc"},
    ExcludedTags: []string{"sat"},
}
solver, err := minizinc.FindBestSolver(filter)
```

### Solving

```go
result, err := instance.Solve(ctx)
if err != nil {
    log.Fatal(err)
}

x, err := result.GetInt("x")
y, err := result.GetFloat("y")
arr, err := result.GetArray("array")
```

### Decoding into Structs

```go
var out struct {
    X      int   `json:"x"`
    Queens []int `json:"queens"`
}
if err := result.Decode(&out); err != nil {
    log.Fatal(err)
}
```

### Typed Errors

CLI failures unwrap to `*minizinc.MinizincError` which exposes the stage,
verbatim stderr and exit code so callers can branch on the underlying cause:

```go
var mzErr *minizinc.MinizincError
if errors.As(err, &mzErr) {
    log.Printf("minizinc %s failed (exit %d): %s", mzErr.Stage, mzErr.ExitCode, mzErr.Stderr)
}
```

Categories make programmatic branching simpler:

```go
switch {
case errors.Is(err, minizinc.ErrSyntax), errors.Is(err, minizinc.ErrType):
    // bug in the model
case errors.Is(err, minizinc.ErrRuntime):
    // solver crashed
}
```

**Note on timeouts**: MiniZinc's `--time-limit` (set via `WithTimeLimit`)
expires with `exit=0` and `Result.Status == StatusUnknown` — no error is
raised, so `ErrTimeout` does not fire for the normal cooperative timeout
path. Check `result.Status == minizinc.StatusUnknown` after a bounded solve.
`ErrTimeout` remains for solver-emitted timeout messages that some
configurations do produce on stderr.

### Getting All Solutions

```go
results, err := instance.SolveAll(ctx)
for _, result := range results {
    fmt.Println(result.Solution)
}
```

### Streaming Solutions

```go
for result := range instance.SolveStream(ctx) {
    if result.Error != nil {
        log.Fatal(result.Error)
    }
    if result.IsIntermediate {
        fmt.Printf("improving: x=%v\n", result.Solution["x"])
        continue
    }
    fmt.Printf("final %s: x=%v\n", result.Status, result.Solution["x"])
}
```

### Solve Options

```go
result, err := instance.Solve(ctx,
    minizinc.WithAllSolutions(),
    minizinc.WithTimeLimit(30*time.Second),
    minizinc.WithProcesses(4),
    minizinc.WithOptimizationLevel(3),
    minizinc.WithVerbose(),
)
```

## Building Models Programmatically

Instead of pasting MiniZinc as strings, use the typed `Builder` API:

```go
b := minizinc.NewBuilder()

n := b.IntParam("n")
queens := b.IntArrayVarSized("queens", n, 1, 8)

b.Constraint(b.AllDifferent(queens))

i, j := minizinc.Var("i"), minizinc.Var("j")
b.Constraint(minizinc.Forall(i, minizinc.Range(minizinc.Int(1), n),
    minizinc.Forall(j, minizinc.Range(i.Add(minizinc.Int(1)), n),
        queens.At(i).Add(i).Ne(queens.At(j).Add(j)).
            And(queens.At(i).Sub(i).Ne(queens.At(j).Sub(j))))))
b.Satisfy()

model := b.Build()
```

Identifier collisions and invalid names panic at build time, not at solve
time. Generated code is plain MiniZinc — `model.AddString` still works for
fragments the DSL does not cover, and `Builder.Include` registers extra
include files.

### Comprehensions and Generators

```go
i, j := minizinc.Var("i"), minizinc.Var("j")
// [i*2 | i in 1..n where (i mod 2 = 0)]
even := minizinc.Comprehension(i.Mul(minizinc.Int(2)),
    minizinc.InWhere(i, minizinc.Range(minizinc.Int(1), n),
        i.Mod(minizinc.Int(2)).Eq(minizinc.Int(0))))

// forall(i in 1..n, j in 1..n where i < j)(queens[i] != queens[j])
b.Constraint(minizinc.ForallG(
    queens.At(i).Ne(queens.At(j)),
    minizinc.In(i, minizinc.Range(minizinc.Int(1), n)),
    minizinc.InWhere(j, minizinc.Range(minizinc.Int(1), n), i.Lt(j)),
))
```

`ConjOf` / `DisjOf` collapse a list of expressions into a single boolean,
which is handy for multi-constraint `forall` bodies.

### Required Parameter Validation

Parameters declared via `IntParam` / `FloatParam` / `BoolParam` /
`IntSetParam` / `IntArrayParamSized` are tracked as required. `Solve` returns
a `*MissingParamsError` listing the unset names before any subprocess runs.
Inspect ahead of time with `model.MissingParams()`.

## Solver-Specific Options

Pass typed structs instead of raw flags via `WithExtraArgs`:

```go
instance.Solve(ctx,
    minizinc.WithSolverOptions(minizinc.GecodeOptions{
        RestartStrategy: "luby",
        RestartScale:    100,
        NodeLimit:       50_000,
    }),
)
```

`ChuffedOptions` and `CoinBCOptions` are also provided; implement
`SolverOptions` to plug in your own.

## Model via stdin

By default the assembled model is written to a temporary `.mzn` file. Pass
`WithModelViaStdin()` to stream it through `--input-from-stdin` instead —
the option avoids the tmp file (and its lifecycle bookkeeping) at the cost
of requiring MiniZinc ≥ 2.6's stdin support.

## Output Sections

When the model uses MiniZinc's `output [...]` or `output_to_section()`,
every string-valued section reaches `Result.Sections`. Access with
`result.Section("explain")`. The default `dzn` section is consumed to fill
`Result.Solution`.

**Caveat**: a model that defines its own `output [...]` REPLACES MiniZinc's
default DZN output. In that case `Result.Solution` will be empty and you
must read values via `result.Section("default")` (or whatever section name
you used). To keep `Solution` populated alongside human-readable output,
use `output_to_section("explain", [...])` and leave the default `dzn`
section alone.

## Cooperative Cancellation

By default the driver gives MiniZinc two seconds to flush statistics and
exit cleanly after the context is cancelled before SIGKILL. Override with
`WithCancelGrace(d)`.

## Examples

See [examples/](examples/) directory for complete examples:

- [simple](examples/simple/) - Basic usage
- [auto_solver](examples/auto_solver/) - Automatic solver selection
- [builder_nqueens](examples/builder_nqueens/) - 8-queens via the typed DSL

## API Reference

### Core Types

- `Driver` - MiniZinc executable interface
- `Model` - Constraint model representation
- `Solver` - Solver configuration
- `Instance` - Model instance with data
- `Result` - Solution results

### Key Methods

**Model:**
- `AddString(code string)` - Add MiniZinc code
- `AddFile(path string) error` - Load from file
- `SetParam(name string, value interface{}) error` - Set parameter
- `Copy() *Model` - Create copy

**Instance:**
- `NewInstance(model *Model, solver *Solver) *Instance` - Create instance with specific solver
- `NewInstanceAuto(model *Model) (*Instance, error)` - Create instance with automatic solver selection
- `Solve(ctx context.Context, opts ...SolveOption) (*Result, error)` - Find one solution
- `SolveAll(ctx context.Context, opts ...SolveOption) ([]*Result, error)` - Find all solutions
- `SolveStream(ctx context.Context, opts ...SolveOption) <-chan *Result` - Stream solutions via channel
- `SetParam(name string, value interface{}) error` - Set model parameter
- `GetParam(name string) (interface{}, bool)` - Get model parameter
- `Cleanup() error` - Remove temporary files

**Result:**
- `Get(name string) (any, bool)` - Get raw value
- `GetInt(name string) (int, error)` - Get integer
- `GetFloat(name string) (float64, error)` - Get float
- `GetBool(name string) (bool, error)` - Get boolean
- `GetString(name string) (string, error)` - Get string
- `GetArray(name string) ([]any, error)` - Get array
- `Decode(dst any) error` - Decode the solution map into a struct via `json:` tags

**Result fields:**
- `Status` - Solution status (OPTIMAL_SOLUTION, UNSATISFIABLE, etc.)
- `Solution` - Map of variable names to values
- `Statistics` - Solver statistics
- `Error` - Error that occurred during solving (used in SolveStream)

### Solve Options

- `WithAllSolutions()` - Find all solutions
- `WithNumSolutions(n int)` - Limit number of solutions
- `WithTimeLimit(d time.Duration)` - Set time limit
- `WithProcesses(n int)` - Set parallel processes
- `WithRandomSeed(seed int)` - Set random seed
- `WithFreeSearch()` - Use free search
- `WithOptimizationLevel(level int)` - Set optimization level (0-5)
- `WithVerbose()` - Enable verbose output
- `WithStatistics()` - Enable statistics
- `WithExtraArgs(args ...string)` - Add custom arguments
- `WithCommandHook(func([]string))` - Inspect the final CLI argv before exec

## Smart Solver Selection

The library includes intelligent solver selection that analyzes your model to choose the best solver automatically:

```go
model := minizinc.NewModel()
model.AddString(`
    array[1..n] of var 1..n: queens;
    constraint alldifferent(queens);
    solve satisfy;
`)

instance, err := minizinc.NewInstanceAuto(model)
```

### What gets analyzed:

- **Solve type**: satisfy, minimize, or maximize
- **Global constraints**: alldifferent, circuit, cumulative, etc.
- **Scheduling constraints**: cumulative, disjunctive
- **Set constraints**: set operations
- **Float variables**: float type detection
- **Model size**: small, medium, or large

### Solver scoring:

The library scores each solver based on:
- Support for required constraints
- Problem type (CP vs MIP vs SAT)
- Optimization capabilities
- Model size and parallelization support

## Testing

```bash
go test -v
```

## License

MIT
