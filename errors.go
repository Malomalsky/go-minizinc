package minizinc

import "fmt"

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

var (
	ErrDriverNotFound     = newError("minizinc executable not found in PATH")
	ErrInvalidVersion     = newError("minizinc version is too old, need 2.6.0 or higher")
	ErrSolverNotFound     = newError("solver not found")
	ErrInvalidModel       = newError("invalid model")
	ErrNoSolution         = newError("no solution found")
	ErrExecutionFailed    = newError("minizinc execution failed")
	ErrInvalidJSON        = newError("invalid JSON response from minizinc")
	ErrParameterNotSet    = newError("required parameter not set")
	ErrInvalidParameter   = newError("invalid parameter value")
	ErrMultipleAssignment = newError("parameter already assigned")
)
