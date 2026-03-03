// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package domain

import "errors"

// ErrorType represents the type of domain error
type ErrorType int

const (
	ErrorTypeValidation  ErrorType = iota // 400 Bad Request
	ErrorTypeNotFound                     // 404 Not Found
	ErrorTypeConflict                     // 409 Conflict
	ErrorTypeInternal                     // 500 Internal Server Error
	ErrorTypeUnavailable                  // 503 Service Unavailable
)

// DomainError represents a domain-level error with semantic type
type DomainError struct {
	Type    ErrorType
	Message string
	Err     error // underlying error for wrapping
}

// Error implements the error interface
func (e *DomainError) Error() string {
	if e.Err != nil {
		return e.Message + ": " + e.Err.Error()
	}
	return e.Message
}

// Unwrap implements error unwrapping for Go 1.13+
func (e *DomainError) Unwrap() error {
	return e.Err
}

// GetErrorType returns the ErrorType from a domain error or defaults to Internal
func GetErrorType(err error) ErrorType {
	var domainErr *DomainError
	if errors.As(err, &domainErr) {
		return domainErr.Type
	}
	return ErrorTypeInternal // default fallback
}

// NewValidationError creates a validation error (400 Bad Request)
func NewValidationError(message string, err ...error) *DomainError {
	return &DomainError{
		Type:    ErrorTypeValidation,
		Message: message,
		Err:     errors.Join(err...),
	}
}

// NewNotFoundError creates a not found error (404 Not Found)
func NewNotFoundError(message string, err ...error) *DomainError {
	return &DomainError{
		Type:    ErrorTypeNotFound,
		Message: message,
		Err:     errors.Join(err...),
	}
}

// NewConflictError creates a conflict error (409 Conflict)
func NewConflictError(message string, err ...error) *DomainError {
	return &DomainError{
		Type:    ErrorTypeConflict,
		Message: message,
		Err:     errors.Join(err...),
	}
}

// NewInternalError creates an internal server error (500 Internal Server Error)
func NewInternalError(message string, err ...error) *DomainError {
	return &DomainError{
		Type:    ErrorTypeInternal,
		Message: message,
		Err:     errors.Join(err...),
	}
}

// NewUnavailableError creates a service unavailable error (503 Service Unavailable)
func NewUnavailableError(message string, err ...error) *DomainError {
	return &DomainError{
		Type:    ErrorTypeUnavailable,
		Message: message,
		Err:     errors.Join(err...),
	}
}
