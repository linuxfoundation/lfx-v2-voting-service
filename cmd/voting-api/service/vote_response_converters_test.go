// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	votesvc "github.com/linuxfoundation/lfx-v2-voting-service/gen/vote"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/service"
	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/models/itx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConvertVoteResponseToResult_FieldRenames is the primary regression guard for the
// vote-response field renames:
//
//	itx.VoteResponse.VoteID    → VoteResponseResult.VoteResponseUID
//	itx.VoteResponse.PollID    → VoteResponseResult.VoteUID
//	itx.VoteResponse.ProjectID → VoteResponseResult.ProjectUID
//
// These three renames are the most likely to be broken by a copy/paste refactor.
func TestConvertVoteResponseToResult_FieldRenames(t *testing.T) {
	resp := &itx.VoteResponse{
		VoteID:    "vote-abc-111",
		PollID:    "poll-def-222",
		ProjectID: "proj-ghi-333",
	}

	result := ConvertVoteResponseToResult(resp)

	if result.VoteResponseUID != resp.VoteID {
		t.Errorf("VoteResponseUID: want %q (from VoteID), got %q", resp.VoteID, result.VoteResponseUID)
	}
	if result.VoteUID != resp.PollID {
		t.Errorf("VoteUID: want %q (from PollID), got %q", resp.PollID, result.VoteUID)
	}
	if result.ProjectUID != resp.ProjectID {
		t.Errorf("ProjectUID: want %q (from ProjectID), got %q", resp.ProjectID, result.ProjectUID)
	}
}

// TestConvertCreateVoteResponsePayloadToDomain_NullCommentEntryIsRejected guards the
// nil-pointer check added in response to the generated Goa validator skipping nil
// array elements. A nil entry would otherwise reach the comment_text access and panic.
// This test pins the behaviour so a refactor that removes the nil check fails here
// instead of panicking in production.
func TestConvertCreateVoteResponsePayloadToDomain_NullCommentEntryIsRejected(t *testing.T) {
	payload := &votesvc.CreateVoteResponsePayload{
		CommentResponses: []*votesvc.CommentResponse{nil}, // null entry in array
	}

	_, err := ConvertCreateVoteResponsePayloadToDomain(payload)
	if err == nil {
		t.Fatal("expected validation error for nil comment_responses entry, got nil")
	}

	var dErr *domain.DomainError
	if !errors.As(err, &dErr) {
		t.Fatalf("expected *domain.DomainError, got %T: %v", err, err)
	}
	if dErr.Type != domain.ErrorTypeValidation {
		t.Errorf("expected ErrorTypeValidation, got %d", dErr.Type)
	}
}

// TestConvertUpdateVoteResponsePayloadToDomain_NullCommentEntryIsRejected applies the
// same nil guard to the update path.
func TestConvertUpdateVoteResponsePayloadToDomain_NullCommentEntryIsRejected(t *testing.T) {
	nullEntries := []*votesvc.CommentResponse{nil}
	payload := &votesvc.UpdateVoteResponsePayload{
		CommentResponses: nullEntries,
	}

	_, err := ConvertUpdateVoteResponsePayloadToDomain(payload)
	if err == nil {
		t.Fatal("expected validation error for nil comment_responses entry, got nil")
	}

	var dErr *domain.DomainError
	if !errors.As(err, &dErr) {
		t.Fatalf("expected *domain.DomainError, got %T: %v", err, err)
	}
	if dErr.Type != domain.ErrorTypeValidation {
		t.Errorf("expected ErrorTypeValidation, got %d", dErr.Type)
	}
}

// TestConvertUpdateVoteResponsePayloadToDomain_OmittedCommentResponses verifies that
// when comment_responses is omitted from the update payload (nil slice), the
// converted request has nil CommentResponses — indicating "do not modify" rather
// than "clear all comments". This is the critical semantic for the update path.
func TestConvertUpdateVoteResponsePayloadToDomain_OmittedCommentResponses(t *testing.T) {
	payload := &votesvc.UpdateVoteResponsePayload{
		CommentResponses: nil, // field omitted from request
	}

	req, err := ConvertUpdateVoteResponsePayloadToDomain(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.CommentResponses != nil {
		t.Errorf("expected nil CommentResponses (omit = preserve), got %+v", req.CommentResponses)
	}
}

// TestConvertUpdateVoteResponsePayloadToDomain_EmptyCommentResponsesClearsComments
// verifies that an explicit empty slice (comment_responses: []) signals "clear all
// comments", which is distinct from omitting the field entirely.
func TestConvertUpdateVoteResponsePayloadToDomain_EmptyCommentResponsesClearsComments(t *testing.T) {
	empty := []*votesvc.CommentResponse{}
	payload := &votesvc.UpdateVoteResponsePayload{
		CommentResponses: empty,
	}

	req, err := ConvertUpdateVoteResponsePayloadToDomain(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.CommentResponses == nil {
		t.Error("expected non-nil CommentResponses for explicit empty slice (clear semantics)")
	}
	if len(*req.CommentResponses) != 0 {
		t.Errorf("expected empty CommentResponses, got len=%d", len(*req.CommentResponses))
	}
}

// TestConvertVoteResponsePayloads_NullAnswerEntriesAreRejected covers null user_vote_content
// and ranked_choices entries on every vote response converter, including the anonymous
// vote-link ones, where a nil dereference would otherwise panic before token verification.
func TestConvertVoteResponsePayloads_NullAnswerEntriesAreRejected(t *testing.T) {
	nullAnswer := []*votesvc.VoteAnswerInput{nil}
	nullRanked := []*votesvc.VoteAnswerInput{{QuestionID: "q1", RankedChoices: []*votesvc.RankedChoiceInput{nil}}}

	for name, answers := range map[string][]*votesvc.VoteAnswerInput{"null answer": nullAnswer, "null ranked choice": nullRanked} {
		converters := map[string]func() error{
			"create": func() error {
				_, err := ConvertCreateVoteResponsePayloadToDomain(&votesvc.CreateVoteResponsePayload{UserVoteContent: answers})
				return err
			},
			"update": func() error {
				_, err := ConvertUpdateVoteResponsePayloadToDomain(&votesvc.UpdateVoteResponsePayload{UserVoteContent: answers})
				return err
			},
			"create by link": func() error {
				_, err := ConvertCreateVoteResponseByLinkPayloadToDomain(&votesvc.CreateVoteResponseByLinkPayload{UserVoteContent: answers})
				return err
			},
			"update by link": func() error {
				_, err := ConvertUpdateVoteResponseByLinkPayloadToDomain(&votesvc.UpdateVoteResponseByLinkPayload{UserVoteContent: answers})
				return err
			},
		}
		for conv, run := range converters {
			t.Run(name+"/"+conv, func(t *testing.T) {
				var dErr *domain.DomainError
				if err := run(); !errors.As(err, &dErr) || dErr.Type != domain.ErrorTypeValidation {
					t.Errorf("expected validation error, got %v", err)
				}
			})
		}
	}
}

func voteLinkBallotFixture(pseudo bool, status string) *service.VoteLinkBallot {
	return &service.VoteLinkBallot{
		Poll: &itx.PollResponse{
			PollID: "vote-1", Name: "TSC election", Description: "Pick two", Status: "active",
			EndTime: "2026-12-01T00:00:00Z", ProjectID: "a0941000002wBz4AAE", ProjectName: "cncf", CommitteeID: "c1",
			CommitteeName: "TSC", TotalVotingRequestInvitations: 9, NumResponseReceived: 4,
			PseudoAnonymity: pseudo, PseudoAnonymityKnown: true, PollType: "generic", AllowAbstain: true,
			PollQuestions: []itx.PollQuestionOutput{{QuestionID: "q1", Prompt: "Who?", Type: "single_choice",
				Choices: []itx.PollChoiceOutput{{ChoiceID: "c-a", ChoiceText: "Alice"}}}},
		},
		Ballot: &itx.VoteResponse{
			VoteID: "ballot-1", PollID: "vote-1", VoteStatus: status, Abstained: false, UserEmail: "voter@example.org",
			PollAnswers:      []itx.PollAnswer{{QuestionID: "q1", Prompt: "Who?", Type: "single_choice", UserChoice: []itx.PollChoiceAnswer{{ChoiceID: "c-a", ChoiceText: "Alice"}}}},
			CommentResponses: []itx.CommentResponse{{PromptID: "p1", CommentText: "Great"}},
		},
		ProjectUID:            "7cad5a8d-19d0-41a4-81a6-043453daf9ee",
		IncludeBallotContents: !pseudo,
	}
}

func TestConvertVoteLinkBallotToResult(t *testing.T) {
	t.Run("returns ballot fields and own answers", func(t *testing.T) {
		got := ConvertVoteLinkBallotToResult(voteLinkBallotFixture(false, "responded"))
		if got.VoteUID != "vote-1" || got.Name != "TSC election" || got.Status != "active" || len(got.PollQuestions) != 1 {
			t.Fatalf("vote fields not mapped: %+v", got)
		}
		if *got.PollQuestions[0].QuestionID != "q1" || *got.PollQuestions[0].Choices[0].ChoiceID != "c-a" {
			t.Errorf("questions not mapped: %+v", got.PollQuestions[0])
		}
		b := got.Ballot
		if !b.AlreadyVoted || b.VoteStatus != "responded" || b.Abstained == nil || len(b.PollAnswers) != 1 || len(b.CommentResponses) != 1 {
			t.Errorf("ballot contents not returned: %+v", b)
		}
	})

	t.Run("pseudo-anonymous vote never returns ballot contents", func(t *testing.T) {
		b := ConvertVoteLinkBallotToResult(voteLinkBallotFixture(true, "responded")).Ballot
		if !b.AlreadyVoted || b.Abstained != nil || b.PollAnswers != nil || b.CommentResponses != nil {
			t.Errorf("pseudo-anonymous ballot leaked contents: %+v", b)
		}
	})

	t.Run("not yet voted returns no contents", func(t *testing.T) {
		b := ConvertVoteLinkBallotToResult(voteLinkBallotFixture(false, "awaiting_response")).Ballot
		if b.AlreadyVoted || b.PollAnswers != nil || b.Abstained != nil || b.CommentResponses != nil {
			t.Errorf("unexpected ballot contents before voting: %+v", b)
		}
	})

	t.Run("submitted ballot on an ended vote is already voted", func(t *testing.T) {
		b := ConvertVoteLinkBallotToResult(voteLinkBallotFixture(false, "ended")).Ballot
		if !b.AlreadyVoted || b.VoteStatus != "ended" || len(b.PollAnswers) != 1 || b.Abstained == nil || b.CommentResponses == nil {
			t.Errorf("ended (submitted) ballot not treated as voted: %+v", b)
		}
	})

	t.Run("unsubmitted ballot on an ended vote is not already voted", func(t *testing.T) {
		b := ConvertVoteLinkBallotToResult(voteLinkBallotFixture(false, "awaiting_response_but_poll_ended")).Ballot
		if b.AlreadyVoted || b.PollAnswers != nil || b.Abstained != nil || b.CommentResponses != nil {
			t.Errorf("unsubmitted ended ballot treated as voted: %+v", b)
		}
	})

	t.Run("voter_removed is mapped", func(t *testing.T) {
		fixture := voteLinkBallotFixture(false, "awaiting_response")
		if ConvertVoteLinkBallotToResult(fixture).Ballot.VoterRemoved {
			t.Fatal("voter_removed should be false by default")
		}
		fixture.Ballot.VoterRemoved = true
		if !ConvertVoteLinkBallotToResult(fixture).Ballot.VoterRemoved {
			t.Error("voter_removed = true was not mapped to ballot.voter_removed")
		}
	})

	t.Run("returns project UID and slug", func(t *testing.T) {
		got := ConvertVoteLinkBallotToResult(voteLinkBallotFixture(false, "responded"))
		if got.ProjectUID != "7cad5a8d-19d0-41a4-81a6-043453daf9ee" {
			t.Errorf("ProjectUID = %q, want the mapped v2 UID", got.ProjectUID)
		}
		if got.ProjectSlug == nil || *got.ProjectSlug != "cncf" {
			t.Errorf("ProjectSlug = %v, want ITX project_name (the slug)", got.ProjectSlug)
		}
	})

	t.Run("empty project slug is omitted", func(t *testing.T) {
		fixture := voteLinkBallotFixture(false, "responded")
		fixture.Poll.ProjectName = ""
		if got := ConvertVoteLinkBallotToResult(fixture).ProjectSlug; got != nil {
			t.Errorf("ProjectSlug = %q, want nil", *got)
		}
	})

	t.Run("no project SFID, committee, counts or PII in the JSON", func(t *testing.T) {
		raw, err := json.Marshal(ConvertVoteLinkBallotToResult(voteLinkBallotFixture(false, "responded")))
		if err != nil {
			t.Fatal(err)
		}
		for _, leak := range []string{"a0941000002wBz4AAE", "TSC\"", "voter@example.org", "ballot-1"} {
			if strings.Contains(string(raw), leak) {
				t.Errorf("result contains %q: %s", leak, raw)
			}
		}
	})
}

func TestConvertVoteResponseToResult_OptionalFieldsAndAnswers(t *testing.T) {
	t.Run("optional SES fields are only set when non-empty", func(t *testing.T) {
		empty := ConvertVoteResponseToResult(&itx.VoteResponse{})
		assert.Nil(t, empty.SesBounceType)
		assert.Nil(t, empty.SesBounceSubtype)
		assert.Nil(t, empty.SesComplaintType)
		assert.Nil(t, empty.SesComplaintDate)
		assert.Nil(t, empty.SesEmailOpenedLastTime)
		assert.Nil(t, empty.SesLinkClickedLastTime)

		full := ConvertVoteResponseToResult(&itx.VoteResponse{
			SESBounceType:          "Permanent",
			SESBounceSubtype:       "General",
			SESComplaintType:       "abuse",
			SESComplaintDate:       "2026-01-03T00:00:00Z",
			SESEmailOpenedLastTime: "2026-01-04T00:00:00Z",
			SESLinkClickedLastTime: "2026-01-05T00:00:00Z",
		})
		assert.Equal(t, "Permanent", *full.SesBounceType)
		assert.Equal(t, "General", *full.SesBounceSubtype)
		assert.Equal(t, "abuse", *full.SesComplaintType)
		assert.Equal(t, "2026-01-03T00:00:00Z", *full.SesComplaintDate)
		assert.Equal(t, "2026-01-04T00:00:00Z", *full.SesEmailOpenedLastTime)
		assert.Equal(t, "2026-01-05T00:00:00Z", *full.SesLinkClickedLastTime)
	})

	t.Run("converts regular and ranked poll answers", func(t *testing.T) {
		got := ConvertVoteResponseToResult(&itx.VoteResponse{
			PollAnswers: []itx.PollAnswer{
				{
					QuestionID: "q1", Prompt: "Approve?", Type: "single_choice",
					UserChoice: []itx.PollChoiceAnswer{{ChoiceID: "yes", ChoiceText: "Yes"}},
				},
				{
					QuestionID: "q2", Prompt: "Rank", Type: "ranked_choice",
					RankedUserChoice: []itx.RankedPollChoiceAnswer{
						{ChoiceID: "a", ChoiceText: "Alice Example", ChoiceRank: 1},
						{ChoiceID: "b", ChoiceText: "Bob Fixture", ChoiceRank: 2},
					},
				},
			},
		})

		require.Len(t, got.PollAnswers, 2)
		first := got.PollAnswers[0]
		assert.Equal(t, "q1", first.QuestionID)
		assert.Equal(t, "Approve?", first.Prompt)
		assert.Equal(t, "single_choice", first.Type)
		require.Len(t, first.UserChoice, 1)
		assert.Equal(t, "yes", first.UserChoice[0].ChoiceID)
		assert.Equal(t, "Yes", first.UserChoice[0].ChoiceText)
		assert.Nil(t, first.RankedUserChoice)

		second := got.PollAnswers[1]
		assert.Nil(t, second.UserChoice)
		require.Len(t, second.RankedUserChoice, 2)
		assert.Equal(t, "b", second.RankedUserChoice[1].ChoiceID)
		assert.Equal(t, "Bob Fixture", second.RankedUserChoice[1].ChoiceText)
		assert.Equal(t, 2, second.RankedUserChoice[1].ChoiceRank)
	})

	t.Run("nil poll answers stay nil while comment responses are always an array", func(t *testing.T) {
		got := ConvertVoteResponseToResult(&itx.VoteResponse{})

		assert.Nil(t, got.PollAnswers)
		require.NotNil(t, got.CommentResponses)
		assert.Empty(t, got.CommentResponses)
	})

	t.Run("converts comment responses", func(t *testing.T) {
		got := ConvertVoteResponseToResult(&itx.VoteResponse{
			CommentResponses: []itx.CommentResponse{{PromptID: "p1", CommentText: "Looks good"}},
		})

		require.Len(t, got.CommentResponses, 1)
		assert.Equal(t, "p1", got.CommentResponses[0].PromptID)
		assert.Equal(t, "Looks good", got.CommentResponses[0].CommentText)
	})
}

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
