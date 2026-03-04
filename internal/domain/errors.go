package domain

// ErrorCode identifies a deterministic domain error category.
type ErrorCode string

const (
	// ErrorCodeCapacityExceeded indicates configured active desk limit has been reached.
	ErrorCodeCapacityExceeded ErrorCode = "capacity exceeded"
	// ErrorCodeStorageInvariant indicates in-memory/indexed state invariant was violated.
	ErrorCodeStorageInvariant ErrorCode = "storage invariant violated"
)

// Error describes a deterministic coordination-domain failure.
type Error struct {
	code ErrorCode
}

// Error returns human-readable error text.
func (e Error) Error() string {
	return string(e.code)
}

// Code returns the stable error code for programmatic mapping.
func (e Error) Code() ErrorCode {
	return e.code
}

// NewError creates a new domain error with provided code.
func NewError(code ErrorCode) error {
	return Error{code: code}
}
