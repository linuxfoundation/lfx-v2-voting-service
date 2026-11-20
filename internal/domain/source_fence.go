// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package domain

import (
	"context"
	"errors"
)

// ErrSourceSuperseded reports an obsolete source delivery.
var ErrSourceSuperseded = errors.New("voting source delivery superseded")

// ErrResourceBusy reports another handler owning the same resource lock.
var ErrResourceBusy = errors.New("voting resource processing busy")

// ResourceLocker serializes same-source handlers across replicas. The returned
// context owns the locked session; use it sequentially until release.
type ResourceLocker interface {
	LockResource(context.Context, string) (context.Context, func(), error)
}

// SourceFence checks the authoritative source revision before effects.
type SourceFence interface {
	Current(context.Context) (bool, error)
}
type sourceFenceKey struct{}

// WithSourceFence associates a delivery with its current-source check.
func WithSourceFence(ctx context.Context, fence SourceFence) context.Context {
	return context.WithValue(ctx, sourceFenceKey{}, fence)
}

// CheckSourceFence is a no-op for direct/tool callers without a delivery fence.
func CheckSourceFence(ctx context.Context) error {
	if fence, ok := ctx.Value(sourceFenceKey{}).(SourceFence); ok {
		current, err := fence.Current(ctx)
		if err != nil {
			return err
		}
		if !current {
			return ErrSourceSuperseded
		}
	}
	return nil
}
