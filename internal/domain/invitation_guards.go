// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package domain

import (
	"context"
	"errors"
)

// ErrEventStateExists reports a lost create-only state claim.
var ErrEventStateExists = errors.New("event state already exists")

// ErrEventStateRevisionMismatch reports a stale state completion revision.
var ErrEventStateRevisionMismatch = errors.New("event state revision mismatch")

// InvitationGuard is the persisted dispatch state for one source response.
type InvitationGuard struct {
	Exists   bool
	Pending  bool
	InviteID string
	Revision uint64
}

// InvitationGuards owns dispatch guards and new-response eligibility. Storage
// failures return ErrorTypeUnavailable; no remote failure authorizes a resend.
type InvitationGuards interface {
	// LookupInvitation reads a guard; Exists=false is a confirmed absence.
	LookupInvitation(context.Context, string) (InvitationGuard, error)
	// ClaimInvitation creates a pending guard only if no guard exists.
	ClaimInvitation(context.Context, string) (uint64, error)
	// CompleteInvitation records the result only under the claimed revision.
	CompleteInvitation(context.Context, string, uint64, string) error
	// PrepareInvitation records eligibility before processed-state persistence.
	PrepareInvitation(context.Context, string) error
	// InvitationPrepared allows updated redeliveries to resume eligible work.
	InvitationPrepared(context.Context, string) (bool, error)
	// ResolveInvitation permanently records a terminal no-invite decision.
	ResolveInvitation(context.Context, string) error
}
