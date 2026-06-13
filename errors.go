package minizinc

import "fmt"

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
