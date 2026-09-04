// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	votesvc "github.com/linuxfoundation/lfx-v2-voting-service/gen/vote"
	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/models/itx"
	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/utils"
)

// TestConvertCreateVotePayloadToDomainEndTimeTimezone guards the request-side hop:
// the optional timezone must reach the domain request when set, and stay empty
// (so `,omitempty` keeps it off the ITX wire) when the client omits it.
func TestConvertCreateVotePayloadToDomainEndTimeTimezone(t *testing.T) {
	t.Run("forwards end_time_timezone when set", func(t *testing.T) {
		payload := &votesvc.CreateVotePayload{
			EndTimeTimezone: utils.StringPtr("America/New_York"),
		}

		req := ConvertCreateVotePayloadToDomain(payload)

		assert.Equal(t, "America/New_York", req.EndTimeTimezone)
	})

	t.Run("leaves end_time_timezone empty when omitted", func(t *testing.T) {
		req := ConvertCreateVotePayloadToDomain(&votesvc.CreateVotePayload{})

		assert.Empty(t, req.EndTimeTimezone)
	})
}

// TestConvertUpdateVotePayloadToDomainEndTimeTimezone guards the update hop: a field
// dropped here would silently ignore a requested timezone change — omission preserves
// the previously stored timezone, so the change would be lost without any error.
func TestConvertUpdateVotePayloadToDomainEndTimeTimezone(t *testing.T) {
	t.Run("forwards end_time_timezone when set", func(t *testing.T) {
		payload := &votesvc.UpdateVotePayload{
			EndTimeTimezone: utils.StringPtr("America/New_York"),
		}

		req := ConvertUpdateVotePayloadToDomain(payload)

		assert.Equal(t, "America/New_York", req.EndTimeTimezone)
	})

	t.Run("leaves end_time_timezone empty when omitted", func(t *testing.T) {
		req := ConvertUpdateVotePayloadToDomain(&votesvc.UpdateVotePayload{})

		assert.Empty(t, req.EndTimeTimezone)
	})
}

// TestConvertPollResponseToVoteResultEndTimeTimezone guards the response-side hop:
// the result field must stay nil (absent from the API response) when ITX has no
// stored timezone, and be a populated pointer when it does.
func TestConvertPollResponseToVoteResultEndTimeTimezone(t *testing.T) {
	t.Run("maps end_time_timezone when ITX returns one", func(t *testing.T) {
		poll := &itx.PollResponse{EndTimeTimezone: "America/New_York"}

		result := ConvertPollResponseToVoteResult(poll)

		require.NotNil(t, result.EndTimeTimezone)
		assert.Equal(t, "America/New_York", *result.EndTimeTimezone)
	})

	t.Run("leaves end_time_timezone nil when ITX returns empty", func(t *testing.T) {
		result := ConvertPollResponseToVoteResult(&itx.PollResponse{})

		assert.Nil(t, result.EndTimeTimezone)
	})
}

// TestConvertVoteResultsToResultPollEndTimeTimezone guards the results hop: same
// absent-vs-populated contract as the vote result, keyed off poll_end_time.
func TestConvertVoteResultsToResultPollEndTimeTimezone(t *testing.T) {
	t.Run("maps poll_end_time_timezone when ITX returns one", func(t *testing.T) {
		results := &itx.VoteResults{PollEndTimeTimezone: "America/New_York"}

		result := ConvertVoteResultsToResult(results)

		require.NotNil(t, result.PollEndTimeTimezone)
		assert.Equal(t, "America/New_York", *result.PollEndTimeTimezone)
	})

	t.Run("leaves poll_end_time_timezone nil when ITX returns empty", func(t *testing.T) {
		result := ConvertVoteResultsToResult(&itx.VoteResults{})

		assert.Nil(t, result.PollEndTimeTimezone)
	})
}
