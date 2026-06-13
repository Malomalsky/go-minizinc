package minizinc

import (
	"errors"
	"fmt"
	"os/exec"
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

// MinizincError describes a failure during a MiniZinc subprocess invocation.
// Stage identifies which call failed ("solve", "list-solvers", "version").
// Stderr carries the binary's stderr output verbatim and ExitCode the process
// exit code (-1 if the process did not start or was killed by signal).
type MinizincError struct {
	Stage    string
	Stderr   string
	ExitCode int
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

func newMinizincError(stage, stderr string, err error) *MinizincError {
	code := -1
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	}
	return &MinizincError{Stage: stage, Stderr: stderr, ExitCode: code, Cause: err}
}
