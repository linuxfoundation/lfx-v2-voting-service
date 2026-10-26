// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package eventing

import (
	"context"
	"errors"
	"fmt"

	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/nats-io/nats.go/jetstream"
)

// pollIsPseudoAnonymous reads the parent itx-poll record from the v1-objects bucket and
// reports whether its vote responses must be indexed without ballot data. It fails closed:
// a nil bucket, a missing record or an undecodable record all count as pseudo-anonymous.
// Only a read error other than not-found is returned, so the caller can NAK for retry.
func pollIsPseudoAnonymous(ctx context.Context, objectsKV jetstream.KeyValue, pollID string) (bool, error) {
	if objectsKV == nil {
		return true, nil
	}

	pollKey := fmt.Sprintf("itx-poll.%s", pollID)
	entry, err := objectsKV.Get(ctx, pollKey)
	if errors.Is(err, jetstream.ErrKeyNotFound) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to read parent poll %s: %w", pollKey, err)
	}

	data, err := decodeKVEntryData(entry.Value())
	if err != nil {
		return true, nil
	}

	return domain.PollRecordIsPseudoAnonymous(data), nil
}
