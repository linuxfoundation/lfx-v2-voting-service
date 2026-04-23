// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package domain

import "context"

// V1UserLookup defines the interface for translating v1 usernames to Auth0 subjects
type V1UserLookup interface {
	// MapUsernameToAuthSub converts a v1 LFX username to the Auth0 "sub" format expected by v2 services
	// by calling the auth service over NATS. Returns an error if the lookup fails.
	MapUsernameToAuthSub(ctx context.Context, username string) (string, error)
}
