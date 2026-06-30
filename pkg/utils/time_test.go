// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package utils

import "testing"

func TestNormalizeTimestamp(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"zero value", "0001-01-01T00:00:00Z", ""},
		{"invalid", "not-a-time", ""},
		{"valid", "2026-02-10T14:32:11Z", "2026-02-10T14:32:11Z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeTimestamp(tt.in); got != tt.want {
				t.Errorf("NormalizeTimestamp(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
