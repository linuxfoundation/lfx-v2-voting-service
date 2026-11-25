// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package domain

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDomainErrorConstructors(t *testing.T) {
	cases := []struct {
		name string
		make func(string, ...error) *DomainError
		want ErrorType
	}{
		{"validation", NewValidationError, ErrorTypeValidation},
		{"not found", NewNotFoundError, ErrorTypeNotFound},
		{"conflict", NewConflictError, ErrorTypeConflict},
		{"internal", NewInternalError, ErrorTypeInternal},
		{"unavailable", NewUnavailableError, ErrorTypeUnavailable},
	}

	for _, tc := range cases {
		t.Run(tc.name+" sets type and message", func(t *testing.T) {
			err := tc.make("something happened")

			assert.Equal(t, tc.want, err.Type)
			assert.Equal(t, "something happened", err.Message)
			assert.Equal(t, "something happened", err.Error())
			assert.NoError(t, err.Err, "no underlying error when none supplied")
			assert.Equal(t, tc.want, GetErrorType(err))
		})

		t.Run(tc.name+" wraps the underlying error", func(t *testing.T) {
			cause := errors.New("root cause")

			err := tc.make("something happened", cause)

			assert.Equal(t, "something happened: root cause", err.Error())
			assert.ErrorIs(t, err, cause)
			assert.Equal(t, cause.Error(), errors.Unwrap(err).Error())
		})
	}
}

func TestDomainError_Error(t *testing.T) {
	t.Run("joins multiple underlying errors", func(t *testing.T) {
		a, b := errors.New("first"), errors.New("second")

		err := NewInternalError("failed", a, b)

		assert.Contains(t, err.Error(), "failed: ")
		assert.Contains(t, err.Error(), "first")
		assert.Contains(t, err.Error(), "second")
		assert.ErrorIs(t, err, a)
		assert.ErrorIs(t, err, b)
	})

	t.Run("nil entries alone do not produce an underlying error", func(t *testing.T) {
		err := NewValidationError("bad input", nil)

		assert.NoError(t, err.Err)
		assert.Equal(t, "bad input", err.Error())
	})
}

func TestDomainError_Unwrap(t *testing.T) {
	t.Run("returns nil when nothing is wrapped", func(t *testing.T) {
		assert.NoError(t, (&DomainError{Message: "x"}).Unwrap())
	})

	t.Run("returns the wrapped error", func(t *testing.T) {
		cause := errors.New("cause")
		assert.Same(t, cause, (&DomainError{Message: "x", Err: cause}).Unwrap())
	})
}

func TestGetErrorType(t *testing.T) {
	t.Run("finds a domain error through fmt.Errorf wrapping", func(t *testing.T) {
		err := fmt.Errorf("outer: %w", NewNotFoundError("missing"))

		assert.Equal(t, ErrorTypeNotFound, GetErrorType(err))
	})

	t.Run("outermost domain error wins over a nested one", func(t *testing.T) {
		err := NewConflictError("outer", NewNotFoundError("inner"))

		assert.Equal(t, ErrorTypeConflict, GetErrorType(err))
	})

	t.Run("plain errors default to internal", func(t *testing.T) {
		assert.Equal(t, ErrorTypeInternal, GetErrorType(errors.New("boom")))
	})

	t.Run("nil defaults to internal", func(t *testing.T) {
		assert.Equal(t, ErrorTypeInternal, GetErrorType(nil))
	})
}

func TestDomainError_ErrorsAs(t *testing.T) {
	err := fmt.Errorf("wrapped: %w", NewValidationError("bad"))

	var domainErr *DomainError
	require.ErrorAs(t, err, &domainErr)
	assert.Equal(t, "bad", domainErr.Message)
}
