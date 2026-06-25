// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package domain

import "context"

// UserReader looks up LFID user data by email.
type UserReader interface {
	UsernameByEmail(ctx context.Context, email string) (string, error)
}
