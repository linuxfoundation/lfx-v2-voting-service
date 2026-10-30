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

// TestConvertVoteResultsToResultPresence guards optional-field presence on the results hop:
// held and anonymous shapes must not gain zero/false/empty metadata, and named results
// must keep legitimate zero and false values.
func TestConvertVoteResultsToResultPresence(t *testing.T) {
	t.Run("held shape forwards the flag and counts without result data", func(t *testing.T) {
		result := ConvertVoteResultsToResult(&itx.VoteResults{
			ResultsHeld:   utils.BoolPtr(true),
			NumRecipients: 10,
			NumVotesCast:  4,
			PollEndTime:   "2030-01-01T00:00:00Z",
		})

		require.NotNil(t, result.ResultsHeld)
		assert.True(t, *result.ResultsHeld)
		assert.Equal(t, 10, result.NumRecipients)
		assert.Equal(t, 4, result.NumVotesCast)
		assert.Nil(t, result.NumAbstained)
		assert.Nil(t, result.PollResults)
		assert.Nil(t, result.CommentResults)
	})

	t.Run("missing metadata stays absent and explicit false/zero survive", func(t *testing.T) {
		absent := ConvertVoteResultsToResult(&itx.VoteResults{})
		assert.Nil(t, absent.ResultsHeld)
		assert.Nil(t, absent.NumAbstained)

		explicit := ConvertVoteResultsToResult(&itx.VoteResults{ResultsHeld: utils.BoolPtr(false), NumAbstained: utils.IntPtr(0)})
		require.NotNil(t, explicit.ResultsHeld)
		assert.False(t, *explicit.ResultsHeld)
		require.NotNil(t, explicit.NumAbstained)
		assert.Equal(t, 0, *explicit.NumAbstained)
	})

	t.Run("anonymous comments stay text-only, duplicated and in order", func(t *testing.T) {
		result := ConvertVoteResultsToResult(&itx.VoteResults{
			CommentResults: []itx.CommentResult{{
				Prompt: itx.PollCommentPromptOutput{PromptID: "c1"},
				Responses: []itx.CommentResponseWithUser{
					{CommentText: "zeta"}, {CommentText: "alpha"}, {CommentText: "alpha"},
				},
			}},
		})

		require.Len(t, result.CommentResults, 1)
		responses := result.CommentResults[0].Responses
		require.Len(t, responses, 3)
		for i, want := range []string{"zeta", "alpha", "alpha"} {
			assert.Equal(t, want, responses[i].CommentText)
			assert.Nil(t, responses[i].VoteID)
			assert.Nil(t, responses[i].VoteCreationTime)
			assert.Nil(t, responses[i].Abstained)
			assert.Nil(t, responses[i].UserID)
		}
	})

	t.Run("named comments keep ids, times and both abstained values", func(t *testing.T) {
		result := ConvertVoteResultsToResult(&itx.VoteResults{
			NumAbstained: utils.IntPtr(0),
			CommentResults: []itx.CommentResult{{
				Prompt: itx.PollCommentPromptOutput{PromptID: "c1"},
				Responses: []itx.CommentResponseWithUser{
					{VoteID: utils.StringPtr("v1"), UserID: "u1", CommentText: "a", VoteCreationTime: utils.StringPtr("2030-01-01T00:00:00Z"), Abstained: utils.BoolPtr(false)},
					{VoteID: utils.StringPtr("v2"), CommentText: "b", VoteCreationTime: utils.StringPtr("2030-01-02T00:00:00Z"), Abstained: utils.BoolPtr(true)},
				},
			}},
		})

		require.NotNil(t, result.NumAbstained)
		assert.Equal(t, 0, *result.NumAbstained)
		responses := result.CommentResults[0].Responses
		require.Len(t, responses, 2)
		assert.Equal(t, "v1", *responses[0].VoteID)
		assert.Equal(t, "2030-01-01T00:00:00Z", *responses[0].VoteCreationTime)
		require.NotNil(t, responses[0].Abstained)
		assert.False(t, *responses[0].Abstained)
		require.NotNil(t, responses[1].Abstained)
		assert.True(t, *responses[1].Abstained)
		assert.Equal(t, "u1", *responses[0].UserID)
	})

	t.Run("populated generic and ranked results keep order", func(t *testing.T) {
		result := ConvertVoteResultsToResult(&itx.VoteResults{
			PollResults: []itx.PollResult{
				{GenericChoiceVotes: []itx.GenericChoiceVote{{ChoiceID: "b", VoteCount: 0}, {ChoiceID: "a", VoteCount: 2, Percentage: 100}}},
				{RankedChoiceVotes: []itx.RankedChoiceVote{{ChoiceID: "x", RankCounts: []itx.RankCount{{Rank: 1, Count: 3}}}}},
			},
		})

		require.Len(t, result.PollResults, 2)
		generic := result.PollResults[0].GenericChoiceVotes
		require.Len(t, generic, 2)
		assert.Equal(t, "b", generic[0].ChoiceID)
		assert.Equal(t, 0, generic[0].VoteCount)
		assert.Equal(t, "a", generic[1].ChoiceID)
		require.Len(t, result.PollResults[1].RankedChoiceVotes, 1)
		assert.Equal(t, 3, result.PollResults[1].RankedChoiceVotes[0].RankCounts[0].Count)
	})
}
