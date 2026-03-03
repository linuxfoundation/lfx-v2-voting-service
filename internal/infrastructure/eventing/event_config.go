// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package eventing

import "time"

// Config holds the configuration for the event processor
type Config struct {
	NATSURL       string
	ConsumerName  string
	StreamName    string
	FilterSubject string
	MaxDeliver    int
	AckWait       time.Duration
	MaxAckPending int
}
