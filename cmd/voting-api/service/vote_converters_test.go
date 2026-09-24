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
// the required timezone must reach the domain request — a field dropped here would
// silently strip the client's timezone before ITX ever sees it.
func TestConvertCreateVotePayloadToDomainEndTimeTimezone(t *testing.T) {
	t.Run("forwards end_time_timezone when set", func(t *testing.T) {
		payload := &votesvc.CreateVotePayload{
			EndTimeTimezone: "America/New_York",
		}

		req := ConvertCreateVotePayloadToDomain(payload)

		assert.Equal(t, "America/New_York", req.EndTimeTimezone)
	})
}

// TestConvertUpdateVotePayloadToDomainEndTimeTimezone guards the update hop: a field
// dropped here would send "" which `,omitempty` keeps off the wire — on update, ITX
// preserves the previously stored timezone rather than clearing it, so the client's
// requested timezone change would be silently ignored.
func TestConvertUpdateVotePayloadToDomainEndTimeTimezone(t *testing.T) {
	t.Run("forwards end_time_timezone when set", func(t *testing.T) {
		payload := &votesvc.UpdateVotePayload{
			EndTimeTimezone: "America/New_York",
		}

		req := ConvertUpdateVotePayloadToDomain(payload)

		assert.Equal(t, "America/New_York", req.EndTimeTimezone)
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

// TestConvertCreateVotePayloadToDomainNumWinners guards the request-side hop for
// num_winners (#2922): only an explicitly provided value may reach the domain
// request — the Goa schema carries no default, so nil must stay nil.
func TestConvertCreateVotePayloadToDomainNumWinners(t *testing.T) {
	t.Run("forwards num_winners when set", func(t *testing.T) {
		payload := &votesvc.CreateVotePayload{NumWinners: utils.IntPtr(5)}

		req := ConvertCreateVotePayloadToDomain(payload)

		require.NotNil(t, req.NumWinners)
		assert.Equal(t, 5, *req.NumWinners)
	})

	t.Run("keeps num_winners nil when omitted", func(t *testing.T) {
		req := ConvertCreateVotePayloadToDomain(&votesvc.CreateVotePayload{})

		assert.Nil(t, req.NumWinners)
	})
}

// TestConvertUpdateVotePayloadToDomainNumWinners guards the update hop under the
// same #2922 rule.
func TestConvertUpdateVotePayloadToDomainNumWinners(t *testing.T) {
	t.Run("forwards num_winners when set", func(t *testing.T) {
		payload := &votesvc.UpdateVotePayload{NumWinners: utils.IntPtr(5)}

		req := ConvertUpdateVotePayloadToDomain(payload)

		require.NotNil(t, req.NumWinners)
		assert.Equal(t, 5, *req.NumWinners)
	})

	t.Run("keeps num_winners nil when omitted", func(t *testing.T) {
		req := ConvertUpdateVotePayloadToDomain(&votesvc.UpdateVotePayload{})

		assert.Nil(t, req.NumWinners)
	})
}

// TestConvertPollResponseToVoteResultNumWinners guards the response-side hop: the
// result field stays nil (absent from the API response) when ITX stores no winner
// count, and is a populated pointer when it does (#2922).
func TestConvertPollResponseToVoteResultNumWinners(t *testing.T) {
	t.Run("maps num_winners when ITX returns one", func(t *testing.T) {
		poll := &itx.PollResponse{NumWinners: 3}

		result := ConvertPollResponseToVoteResult(poll)

		require.NotNil(t, result.NumWinners)
		assert.Equal(t, 3, *result.NumWinners)
	})

	t.Run("leaves num_winners nil when ITX returns zero", func(t *testing.T) {
		result := ConvertPollResponseToVoteResult(&itx.PollResponse{})

		assert.Nil(t, result.NumWinners)
	})
}
