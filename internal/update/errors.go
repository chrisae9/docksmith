package update

import (
	"errors"
	"fmt"
)

// Sentinel errors for user-facing conditions (4xx).
var (
	ErrContainerNotFound = errors.New("container not found")
	ErrNoTargetVersion   = errors.New("no target version specified")
)

// NotFoundError wraps an error to indicate a resource was not found (404).
type NotFoundError struct {
	Err error
}

func (e *NotFoundError) Error() string { return e.Err.Error() }
func (e *NotFoundError) Unwrap() error { return e.Err }

// BadRequestError wraps an error to indicate invalid user input (400).
type BadRequestError struct {
	Err error
}

func (e *BadRequestError) Error() string { return e.Err.Error() }
func (e *BadRequestError) Unwrap() error { return e.Err }

// NewNotFoundError creates a NotFoundError with a formatted message.
func NewNotFoundError(format string, args ...any) error {
	return &NotFoundError{Err: fmt.Errorf(format, args...)}
}

// NewBadRequestError creates a BadRequestError with a formatted message.
func NewBadRequestError(format string, args ...any) error {
	return &BadRequestError{Err: fmt.Errorf(format, args...)}
}
