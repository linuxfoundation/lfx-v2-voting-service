// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package utils

import "time"

// NormalizeTimestamp returns "" for empty or zero-value (0001-01-01T00:00:00Z) RFC3339
// timestamps so omitempty can elide them. The NATS KV path reads raw DynamoDB attributes
// that bypass ITX's PollDB.MarshalJSON guard, which would otherwise leak a bogus close date.
func NormalizeTimestamp(s string) string {
	if t, err := time.Parse(time.RFC3339, s); err != nil || t.IsZero() {
		return ""
	}
	return s
}
