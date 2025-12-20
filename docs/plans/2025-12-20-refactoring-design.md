# go-minizinc Refactoring Design

## Goal

Full refactoring: production-ready, performant, idiomatic Go. Breaking changes allowed where needed for quality.

## Changes

### 1. API Fixes

#### 1.1 NewInstance returns error

```go
// Before
func NewInstance(model *Model, solver *Solver) *Instance

// After
func NewInstance(model *Model, solver *Solver) (*Instance, error)
```

Errors:
- `ErrNilModel` — model is nil
- `ErrNoSolver` — solver is nil and auto-selection failed
- `ErrDriverNotFound` — MiniZinc not found

#### 1.2 Fix shadowing of built-in `copy`

In `model.go:86`:
```go
// Before
copy := &Model{...}

// After
cloned := &Model{...}
```

#### 1.3 Deep copy in Model.Copy()

Current implementation does shallow copy of `parameters` map values. If a value is a slice or map, mutations in the copy affect the original.

Add recursive deep copy:
```go
func deepCopyValue(v interface{}) interface{} {
    switch val := v.(type) {
    case []interface{}:
        cp := make([]interface{}, len(val))
        for i, item := range val {
            cp[i] = deepCopyValue(item)
        }
        return cp
    case map[string]interface{}:
        cp := make(map[string]interface{})
        for k, item := range val {
            cp[k] = deepCopyValue(item)
        }
        return cp
    default:
        return v
    }
}
```

### 2. Performance

#### 2.1 strings.Builder in getCode()

```go
// Before
result := ""
for _, fragment := range m.codeFragments {
    result += fragment + "\n"
}

// After
var sb strings.Builder
for _, fragment := range m.codeFragments {
    sb.WriteString(fragment)
    sb.WriteByte('\n')
}
return sb.String()
```

#### 2.2 Handle strconv.Atoi errors

In `driver.go:102-104`, check errors instead of ignoring:
```go
major, err := strconv.Atoi(matches[1])
if err != nil {
    return newError("failed to parse major version")
}
// same for minor, patch
```

### 3. Code Quality

#### 3.1 Formatting
Run `gofmt -w solver_capabilities.go`

#### 3.2 New errors in errors.go
```go
var (
    ErrNilModel  = newError("model is nil")
    ErrNoSolver  = newError("no solver available")
)
```

#### 3.3 Godoc comments
Add documentation for all exported types and functions.

#### 3.4 Tests
- Test NewInstance error cases
- Test deep copy of parameters with nested structures
- Test getCode() with large models

## Files to Modify

| File | Changes |
|------|---------|
| `instance.go` | NewInstance signature, error handling |
| `model.go` | Fix `copy` shadowing, deep copy, strings.Builder |
| `driver.go` | Handle Atoi errors |
| `errors.go` | Add ErrNilModel, ErrNoSolver |
| `solver_capabilities.go` | Format with gofmt |
| `minizinc_test.go` | Update tests for new API, add error tests |
| All exported symbols | Add godoc comments |

## Breaking Changes

- `NewInstance(model, solver)` now returns `(*Instance, error)`
- All callers must handle the error
