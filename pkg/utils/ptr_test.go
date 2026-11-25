// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPointerHelpers(t *testing.T) {
	t.Run("StringPtr", func(t *testing.T) {
		p := StringPtr("alice")
		require.NotNil(t, p)
		assert.Equal(t, "alice", *p)

		empty := StringPtr("")
		require.NotNil(t, empty, "an empty string is still a non-nil pointer")
		assert.Equal(t, "", *empty)
	})

	t.Run("BoolPtr keeps false distinct from nil", func(t *testing.T) {
		f := BoolPtr(false)
		require.NotNil(t, f)
		assert.False(t, *f)

		tr := BoolPtr(true)
		require.NotNil(t, tr)
		assert.True(t, *tr)
	})

	t.Run("IntPtr keeps zero distinct from nil", func(t *testing.T) {
		zero := IntPtr(0)
		require.NotNil(t, zero)
		assert.Equal(t, 0, *zero)

		n := IntPtr(-7)
		require.NotNil(t, n)
		assert.Equal(t, -7, *n)
	})

	t.Run("each call returns an independent pointer", func(t *testing.T) {
		a, b := IntPtr(1), IntPtr(1)
		assert.NotSame(t, a, b)
		*a = 2
		assert.Equal(t, 1, *b)
	})
}
