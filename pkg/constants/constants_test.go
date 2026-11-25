// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package constants

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsHealthCheckPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/health", true},
		{"/livez", true},
		{"/readyz", true},
		{"/votes", false},
		{"/health/", false},
		{"/HEALTH", false},
		{"/healthz", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			assert.Equal(t, tt.want, IsHealthCheckPath(tt.path))
		})
	}

	t.Run("every declared health path is recognised", func(t *testing.T) {
		for _, p := range HealthCheckPaths {
			assert.True(t, IsHealthCheckPath(p), p)
		}
	})
}
