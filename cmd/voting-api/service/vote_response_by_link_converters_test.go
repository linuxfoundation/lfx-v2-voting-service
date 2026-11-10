// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	votesvc "github.com/linuxfoundation/lfx-v2-voting-service/gen/vote"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
)

func byLinkAnswers() []*votesvc.VoteAnswerInput {
	return []*votesvc.VoteAnswerInput{
		{QuestionID: "q1", ChoiceIds: []string{"yes"}},
		{
			QuestionID: "q2",
			RankedChoices: []*votesvc.RankedChoiceInput{
				{ChoiceID: "a", ChoiceRank: 1},
				{ChoiceID: "b", ChoiceRank: 2},
			},
		},
	}
}

func TestConvertCreateVoteResponseByLinkPayloadToDomain(t *testing.T) {
	t.Run("maps answers, ranked choices, abstain and comments", func(t *testing.T) {
		req, err := ConvertCreateVoteResponseByLinkPayloadToDomain(&votesvc.CreateVoteResponseByLinkPayload{
			VoteToken:       "token-from-link",
			UserVoteContent: byLinkAnswers(),
			Abstain:         true,
			CommentResponses: []*votesvc.CommentResponse{
				{PromptID: "p1", CommentText: "Looks good"},
			},
		})

		require.NoError(t, err)
		require.Len(t, req.UserVoteContent, 2)
		assert.Equal(t, "q1", req.UserVoteContent[0].QuestionID)
		assert.Equal(t, []string{"yes"}, req.UserVoteContent[0].ChoiceIDs)
		assert.Empty(t, req.UserVoteContent[0].RankedChoices)
		assert.Equal(t, "q2", req.UserVoteContent[1].QuestionID)
		require.Len(t, req.UserVoteContent[1].RankedChoices, 2)
		assert.Equal(t, "a", req.UserVoteContent[1].RankedChoices[0].ChoiceID)
		assert.Equal(t, 1, req.UserVoteContent[1].RankedChoices[0].ChoiceRank)
		assert.Equal(t, "b", req.UserVoteContent[1].RankedChoices[1].ChoiceID)
		assert.Equal(t, 2, req.UserVoteContent[1].RankedChoices[1].ChoiceRank)
		assert.True(t, req.Abstain)
		require.Len(t, req.CommentResponses, 1)
		assert.Equal(t, "p1", req.CommentResponses[0].PromptID)
		assert.Equal(t, "Looks good", req.CommentResponses[0].CommentText)
	})

	t.Run("never takes the vote ID from the request", func(t *testing.T) {
		req, err := ConvertCreateVoteResponseByLinkPayloadToDomain(&votesvc.CreateVoteResponseByLinkPayload{
			VoteToken:       "token-from-link",
			UserVoteContent: byLinkAnswers(),
		})

		require.NoError(t, err)
		assert.Empty(t, req.VoteID, "the service sets it from the verified token so a ballot can only come from the link")
	})

	t.Run("nil answers and comments yield an empty answer list and nil comments", func(t *testing.T) {
		req, err := ConvertCreateVoteResponseByLinkPayloadToDomain(&votesvc.CreateVoteResponseByLinkPayload{VoteToken: "t"})

		require.NoError(t, err)
		require.NotNil(t, req.UserVoteContent)
		assert.Empty(t, req.UserVoteContent)
		assert.Nil(t, req.CommentResponses)
	})

	t.Run("rejects null entries with a validation error", func(t *testing.T) {
		cases := map[string]*votesvc.CreateVoteResponseByLinkPayload{
			"null answer": {
				UserVoteContent: []*votesvc.VoteAnswerInput{nil},
			},
			"null ranked choice": {
				UserVoteContent: []*votesvc.VoteAnswerInput{{QuestionID: "q", RankedChoices: []*votesvc.RankedChoiceInput{nil}}},
			},
			"null comment": {
				CommentResponses: []*votesvc.CommentResponse{nil},
			},
		}
		for name, payload := range cases {
			t.Run(name, func(t *testing.T) {
				req, err := ConvertCreateVoteResponseByLinkPayloadToDomain(payload)

				require.Error(t, err)
				assert.Nil(t, req)
				assert.Equal(t, domain.ErrorTypeValidation, domain.GetErrorType(err))
				assert.Contains(t, err.Error(), "must not be null")
			})
		}
	})
}

func TestConvertUpdateVoteResponseByLinkPayloadToDomain(t *testing.T) {
	t.Run("maps answers, ranked choices and abstain", func(t *testing.T) {
		req, err := ConvertUpdateVoteResponseByLinkPayloadToDomain(&votesvc.UpdateVoteResponseByLinkPayload{
			VoteToken:       "token-from-link",
			UserVoteContent: byLinkAnswers(),
			Abstain:         true,
		})

		require.NoError(t, err)
		require.Len(t, req.UserVoteContent, 2)
		assert.Equal(t, "q1", req.UserVoteContent[0].QuestionID)
		assert.Equal(t, []string{"yes"}, req.UserVoteContent[0].ChoiceIDs)
		require.Len(t, req.UserVoteContent[1].RankedChoices, 2)
		assert.Equal(t, 2, req.UserVoteContent[1].RankedChoices[1].ChoiceRank)
		assert.True(t, req.Abstain)
	})

	t.Run("omitted comment_responses preserves existing comments", func(t *testing.T) {
		req, err := ConvertUpdateVoteResponseByLinkPayloadToDomain(&votesvc.UpdateVoteResponseByLinkPayload{VoteToken: "t"})

		require.NoError(t, err)
		assert.Nil(t, req.CommentResponses)
	})

	t.Run("an empty comment_responses list clears comments", func(t *testing.T) {
		req, err := ConvertUpdateVoteResponseByLinkPayloadToDomain(&votesvc.UpdateVoteResponseByLinkPayload{
			VoteToken:        "t",
			CommentResponses: []*votesvc.CommentResponse{},
		})

		require.NoError(t, err)
		require.NotNil(t, req.CommentResponses)
		assert.Empty(t, *req.CommentResponses)
	})

	t.Run("provided comments are converted", func(t *testing.T) {
		req, err := ConvertUpdateVoteResponseByLinkPayloadToDomain(&votesvc.UpdateVoteResponseByLinkPayload{
			VoteToken:        "t",
			CommentResponses: []*votesvc.CommentResponse{{PromptID: "p1", CommentText: "Updated"}},
		})

		require.NoError(t, err)
		require.NotNil(t, req.CommentResponses)
		require.Len(t, *req.CommentResponses, 1)
		assert.Equal(t, "p1", (*req.CommentResponses)[0].PromptID)
		assert.Equal(t, "Updated", (*req.CommentResponses)[0].CommentText)
	})

	t.Run("rejects null entries with a validation error", func(t *testing.T) {
		cases := map[string]*votesvc.UpdateVoteResponseByLinkPayload{
			"null answer": {
				UserVoteContent: []*votesvc.VoteAnswerInput{nil},
			},
			"null ranked choice": {
				UserVoteContent: []*votesvc.VoteAnswerInput{{QuestionID: "q", RankedChoices: []*votesvc.RankedChoiceInput{nil}}},
			},
			"null comment": {
				CommentResponses: []*votesvc.CommentResponse{nil},
			},
		}
		for name, payload := range cases {
			t.Run(name, func(t *testing.T) {
				req, err := ConvertUpdateVoteResponseByLinkPayloadToDomain(payload)

				require.Error(t, err)
				assert.Nil(t, req)
				assert.Equal(t, domain.ErrorTypeValidation, domain.GetErrorType(err))
				assert.Contains(t, err.Error(), "must not be null")
			})
		}
	})
}
