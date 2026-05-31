package resolver

import "errors"

// Sentinel errors for gRPC code mapping. Wrap these with fmt.Errorf("... %w", ErrXxx)
// so callers can classify errors without inspecting message strings.
var (
	ErrNotFound           = errors.New("not found")
	ErrInvalidArgument    = errors.New("invalid argument")
	ErrAlreadyExists      = errors.New("already exists")
	ErrFailedPrecondition = errors.New("failed precondition")
)
