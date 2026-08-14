// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package utils

// StringPtr returns a pointer to the given string value
func StringPtr(s string) *string {
	return &s
}

// BoolPtr returns a pointer to the given bool value
func BoolPtr(b bool) *bool {
	return &b
}

// IntPtr returns a pointer to the given int value
func IntPtr(i int) *int {
	return &i
}
