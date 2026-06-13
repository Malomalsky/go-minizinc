package minizinc

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Error is the package's error type. It always carries a human-readable
// message and may wrap an underlying cause that is exposed via Unwrap so that
// errors.Is and errors.As work as expected.
type Error struct {
	Message string
	Cause   error
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	return e.Message
}

func (e *Error) Unwrap() error {
	return e.Cause
}

func newError(msg string) *Error {
	return &Error{Message: msg}
}

func wrapError(msg string, err error) *Error {
	return &Error{Message: msg, Cause: err}
}

// Sentinel errors returned by the package.
var (
	ErrDriverNotFound     = newError("minizinc executable not found in PATH")
	ErrInvalidVersion     = newError("minizinc version is too old, need 2.6.0 or higher")
	ErrSolverNotFound     = newError("solver not found")
	ErrNilModel           = newError("model is nil")
	ErrNoSolver           = newError("no solver available")
	ErrMultipleAssignment = newError("parameter already assigned")
)

// MissingParamsError is returned by Solve when the model was built via the
// Builder DSL and one or more Builder-declared parameters lack values. It
// names every missing parameter at once so the caller can fix them in a
// single round trip.
type MissingParamsError struct {
	Missing []string
}

func (e *MissingParamsError) Error() string {
	return "minizinc: required parameters not set: " + strings.Join(e.Missing, ", ")
}

// ErrorCategory classifies a MinizincError by the kind of underlying failure
// the stderr suggests. Use errors.Is with one of the sentinel values below to
// branch on the category programmatically.
type ErrorCategory int

const (
	// CategoryUnknown is the fallback when no specific pattern matched.
	CategoryUnknown ErrorCategory = iota
	// CategorySyntax — model failed to parse / type-check.
	CategorySyntax
	// CategoryType — type inconsistency reported by the type-checker.
	CategoryType
	// CategoryTimeout — solver hit its time limit.
	CategoryTimeout
	// CategoryRuntime — solver crashed or aborted at runtime.
	CategoryRuntime
)

// Sentinel errors for use with errors.Is(err, ErrSyntax) etc. They have no
// payload of their own; equality is checked by MinizincError.Is.
var (
	ErrSyntax  = newError("minizinc syntax/type error")
	ErrType    = newError("minizinc type error")
	ErrTimeout = newError("minizinc time limit exceeded")
	ErrRuntime = newError("minizinc runtime error")
)

// MinizincError describes a failure during a MiniZinc subprocess invocation.
// Stage identifies which call failed ("solve", "list-solvers", "version").
// Stderr carries the binary's stderr output verbatim and ExitCode the process
// exit code (-1 if the process did not start or was killed by signal).
// Category coarse-bins the failure for callers that need to react
// differently to a model bug vs a timeout vs a solver crash.
type MinizincError struct {
	Stage    string
	Stderr   string
	ExitCode int
	Category ErrorCategory
	Cause    error
}

func (e *MinizincError) Error() string {
	msg := fmt.Sprintf("minizinc %s failed (exit=%d)", e.Stage, e.ExitCode)
	if e.Stderr != "" {
		msg += ": " + e.Stderr
	} else if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

func (e *MinizincError) Unwrap() error {
	return e.Cause
}

// Is implements errors.Is so callers can branch on category with the
// sentinels above (errors.Is(err, ErrSyntax)).
func (e *MinizincError) Is(target error) bool {
	switch target {
	case ErrSyntax:
		return e.Category == CategorySyntax
	case ErrType:
		return e.Category == CategoryType
	case ErrTimeout:
		return e.Category == CategoryTimeout
	case ErrRuntime:
		return e.Category == CategoryRuntime
	}
	return false
}

func newMinizincError(stage, stderr string, err error) *MinizincError {
	code := -1
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	}
	return &MinizincError{
		Stage:    stage,
		Stderr:   stderr,
		ExitCode: code,
		Category: classifyStderr(stderr),
		Cause:    err,
	}
}

func classifyStderr(stderr string) ErrorCategory {
	if stderr == "" {
		return CategoryUnknown
	}
	low := strings.ToLower(stderr)
	switch {
	case strings.Contains(low, "time limit") || strings.Contains(low, "timed out"):
		return CategoryTimeout
	case strings.Contains(low, "type error"):
		return CategoryType
	case strings.Contains(low, "syntax error") || strings.Contains(low, "parse error"):
		return CategorySyntax
	case strings.Contains(low, "runtime error") ||
		strings.Contains(low, "aborted") ||
		strings.Contains(low, "segmentation fault") ||
		strings.Contains(low, "internal error"):
		return CategoryRuntime
	}
	return CategoryUnknown
}
