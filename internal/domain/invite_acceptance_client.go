// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package domain

import "context"

// InviteAcceptanceClient enriches ITX vote records after a user accepts an LFID invite.
type InviteAcceptanceClient interface {
	AcceptInvite(ctx context.Context, email, username string) error
}
