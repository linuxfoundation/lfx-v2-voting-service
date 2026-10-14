// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package main

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseBoolEnv(t *testing.T) {
	const key = "VOTE_PARTICIPANT_GRANTS_ENABLED_TEST"

	tests := []struct {
		name  string
		value string
		set   bool
		want  bool
	}{
		{name: "unset is off", set: false, want: false},
		{name: "empty is off", set: true, value: "", want: false},
		{name: "true", set: true, value: "true", want: true},
		{name: "1", set: true, value: "1", want: true},
		{name: "yes", set: true, value: "YES", want: true},
		{name: "false", set: true, value: "false", want: false},
		// A typo must never switch a gated behaviour on.
		{name: "unrecognised is off", set: true, value: "enabled-please", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.set {
				t.Setenv(key, tt.value)
			}
			assert.Equal(t, tt.want, parseBoolEnv(key, slog.Default()))
		})
	}
}
