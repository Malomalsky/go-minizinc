# go-minizinc

Go bindings for MiniZinc constraint solver.

## Installation

```bash
go get github.com/Malomalsky/go-minizinc
```

## Requirements

- Go 1.18 or higher
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
    x, _ := result.GetInt("x")
    fmt.Printf("Solution: x=%d\n", x)
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

## Examples

See [examples/](examples/) directory for complete examples:

- [simple](examples/simple/) - Basic usage
- [auto_solver](examples/auto_solver/) - Automatic solver selection

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
- `Get(name string) (interface{}, bool)` - Get raw value
- `GetInt(name string) (int, error)` - Get integer
- `GetFloat(name string) (float64, error)` - Get float
- `GetBool(name string) (bool, error)` - Get boolean
- `GetString(name string) (string, error)` - Get string
- `GetArray(name string) ([]interface{}, error)` - Get array

**Result fields:**
- `Status` - Solution status (OPTIMAL_SOLUTION, UNSATISFIABLE, etc.)
- `Solution` - Map of variable names to values
- `Statistics` - Solver statistics
- `Error` - Error that occurred during solving (used in SolveStream)

### Solve Options

- `WithAllSolutions()` - Find all solutions
- `WithTimeLimit(d time.Duration)` - Set time limit
- `WithProcesses(n int)` - Set parallel processes
- `WithRandomSeed(seed int)` - Set random seed
- `WithFreeSearch()` - Use free search
- `WithOptimizationLevel(level int)` - Set optimization level (0-5)
- `WithVerbose()` - Enable verbose output
- `WithStatistics()` - Enable statistics
- `WithExtraArgs(args ...string)` - Add custom arguments

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
