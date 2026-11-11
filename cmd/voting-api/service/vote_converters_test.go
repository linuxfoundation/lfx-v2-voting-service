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

// TestConvertCreateVotePayloadToDomainAllowVoteChange guards the create hop: the pointer is
// copied as-is so an omitted setting stays nil (ITX defaults it to true) and an explicit
// value survives.
func TestConvertCreateVotePayloadToDomainAllowVoteChange(t *testing.T) {
	for name, value := range map[string]*bool{"omitted": nil, "true": utils.BoolPtr(true), "false": utils.BoolPtr(false)} {
		t.Run(name, func(t *testing.T) {
			req := ConvertCreateVotePayloadToDomain(&votesvc.CreateVotePayload{AllowVoteChange: value})

			assert.Equal(t, value, req.AllowVoteChange)
		})
	}
}

// TestConvertUpdateVotePayloadToDomainAllowVoteChange guards the update hop: an omitted
// setting must stay nil so ITX preserves the stored value instead of overwriting it.
func TestConvertUpdateVotePayloadToDomainAllowVoteChange(t *testing.T) {
	for name, value := range map[string]*bool{"omitted": nil, "true": utils.BoolPtr(true), "false": utils.BoolPtr(false)} {
		t.Run(name, func(t *testing.T) {
			req := ConvertUpdateVotePayloadToDomain(&votesvc.UpdateVotePayload{AllowVoteChange: value})

			assert.Equal(t, value, req.AllowVoteChange)
		})
	}
}

// TestConvertPollResponseToVoteResultAllowVoteChange guards the read hop: the result always
// carries a resolved boolean, and polls created before the setting existed read as allowed.
func TestConvertPollResponseToVoteResultAllowVoteChange(t *testing.T) {
	tests := []struct {
		name  string
		value *bool
		want  bool
	}{
		{name: "legacy absent reads as allowed", want: true},
		{name: "explicit true", value: utils.BoolPtr(true), want: true},
		{name: "explicit false", value: utils.BoolPtr(false), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ConvertPollResponseToVoteResult(&itx.PollResponse{AllowVoteChange: tt.value})

			require.NotNil(t, result.AllowVoteChange)
			assert.Equal(t, tt.want, *result.AllowVoteChange)
		})
	}
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

// TestConvertPollResponseToVoteResult_FieldRenames is the primary regression guard for
// the ITX-to-LFXv2 field rename:
//
//	itx.PollResponse.PollID      → VoteResult.UID
//	itx.PollResponse.ProjectID   → VoteResult.ProjectUID
//	itx.PollResponse.CommitteeID → VoteResult.CommitteeUID
//
// Any of these renames being silently dropped or swapped would expose raw v1 IDs to
// clients or produce empty UID fields, both of which break client-side assumptions.
func TestConvertPollResponseToVoteResult_FieldRenames(t *testing.T) {
	poll := &itx.PollResponse{
		PollID:      "poll-abc-123",
		Name:        "Test Vote",
		Status:      "enabled",
		ProjectID:   "proj-sfid-456",
		CommitteeID: "comm-sfid-789",
	}

	result := ConvertPollResponseToVoteResult(poll)

	if result.UID != poll.PollID {
		t.Errorf("UID: want %q (from PollID), got %q", poll.PollID, result.UID)
	}
	if result.ProjectUID != poll.ProjectID {
		t.Errorf("ProjectUID: want %q (from ProjectID), got %q", poll.ProjectID, result.ProjectUID)
	}
	if result.CommitteeUID != poll.CommitteeID {
		t.Errorf("CommitteeUID: want %q (from CommitteeID), got %q", poll.CommitteeID, result.CommitteeUID)
	}

	// Unchanged fields should pass through without transformation.
	if result.Name != poll.Name {
		t.Errorf("Name: want %q, got %q", poll.Name, result.Name)
	}
	if result.Status != poll.Status {
		t.Errorf("Status: want %q, got %q", poll.Status, result.Status)
	}
}

// TestConvertPollResponseToVoteResult_PollQuestionsPassThrough verifies that nested
// question and choice structures are carried through correctly so an agent that adds
// a new question field doesn't accidentally drop existing ones.
func TestConvertPollResponseToVoteResult_PollQuestionsPassThrough(t *testing.T) {
	poll := &itx.PollResponse{
		PollID: "poll-1",
		PollQuestions: []itx.PollQuestionOutput{
			{
				QuestionID: "q-1",
				Prompt:     "Approve?",
				Type:       "single_choice",
				Choices: []itx.PollChoiceOutput{
					{ChoiceID: "c-1", ChoiceText: "Yes"},
					{ChoiceID: "c-2", ChoiceText: "No"},
				},
			},
		},
	}

	result := ConvertPollResponseToVoteResult(poll)

	if len(result.PollQuestions) != 1 {
		t.Fatalf("want 1 question, got %d", len(result.PollQuestions))
	}
	q := result.PollQuestions[0]
	if q.Prompt != "Approve?" {
		t.Errorf("question Prompt: want %q, got %q", "Approve?", q.Prompt)
	}
	if len(q.Choices) != 2 {
		t.Fatalf("want 2 choices, got %d", len(q.Choices))
	}
	if q.Choices[0].ChoiceText != "Yes" {
		t.Errorf("choice[0] ChoiceText: want %q, got %q", "Yes", q.Choices[0].ChoiceText)
	}
}

// TestConvertCreateVotePayloadToDomain_FieldMapping verifies that the Goa payload
// field names are correctly mapped to the service request. The highest-risk
// rename here is CommitteeUids→CommitteeUIDs (plural, casing); NumWinners is a
// *int passthrough — only explicitly provided values may reach ITX (#2922).
func TestConvertCreateVotePayloadToDomain_FieldMapping(t *testing.T) {
	payload := &votesvc.CreateVotePayload{
		Name:          "Board Election",
		ProjectUID:    "proj-uid-123",
		CommitteeUID:  "comm-uid-456",
		CommitteeUids: []string{"a", "b"},
		NumWinners:    utils.IntPtr(3),
		PollType:      "generic",
	}

	req := ConvertCreateVotePayloadToDomain(payload)

	if req.Name != payload.Name {
		t.Errorf("Name: want %q, got %q", payload.Name, req.Name)
	}
	if req.ProjectUID != payload.ProjectUID {
		t.Errorf("ProjectUID: want %q, got %q", payload.ProjectUID, req.ProjectUID)
	}
	if req.CommitteeUID != payload.CommitteeUID {
		t.Errorf("CommitteeUID: want %q, got %q", payload.CommitteeUID, req.CommitteeUID)
	}
	if len(req.CommitteeUIDs) != len(payload.CommitteeUids) {
		t.Errorf("CommitteeUIDs length: want %d, got %d", len(payload.CommitteeUids), len(req.CommitteeUIDs))
	}
	// NumWinners is a pointer passthrough: an explicitly provided value must
	// arrive non-nil and unchanged so it can be sent as-is to ITX.
	if req.NumWinners == nil {
		t.Fatal("NumWinners: expected non-nil pointer, got nil")
	}
	if *req.NumWinners != *payload.NumWinners {
		t.Errorf("NumWinners: want %d, got %d", *payload.NumWinners, *req.NumWinners)
	}
	if req.PollType != payload.PollType {
		t.Errorf("PollType: want %q, got %q", payload.PollType, req.PollType)
	}
}

// TestConvertCreateVotePayloadToDomain_PollQuestionsAndChoices verifies that nested
// question structures (including choice text) survive conversion. Dropping choices
// would silently submit polls with no answer options to ITX.
func TestConvertCreateVotePayloadToDomain_PollQuestionsAndChoices(t *testing.T) {
	payload := &votesvc.CreateVotePayload{
		Name: "poll",
		PollQuestions: []*votesvc.PollQuestion{
			{
				Prompt: "Who should be elected?",
				Type:   "single_choice",
				Choices: []*votesvc.PollChoice{
					{ChoiceText: "Alice"},
					{ChoiceText: "Bob"},
				},
			},
		},
	}

	req := ConvertCreateVotePayloadToDomain(payload)

	if len(req.PollQuestions) != 1 {
		t.Fatalf("want 1 question, got %d", len(req.PollQuestions))
	}
	q := req.PollQuestions[0]
	if q.Prompt != "Who should be elected?" {
		t.Errorf("Prompt: want %q, got %q", "Who should be elected?", q.Prompt)
	}
	if len(q.Choices) != 2 {
		t.Fatalf("want 2 choices, got %d", len(q.Choices))
	}
	if q.Choices[0].ChoiceText != "Alice" || q.Choices[1].ChoiceText != "Bob" {
		t.Errorf("choices: want [Alice, Bob], got %v", q.Choices)
	}
}

func TestConvertUpdateVotePayloadToDomain_FieldMapping(t *testing.T) {
	basePayload := func() *votesvc.UpdateVotePayload {
		return &votesvc.UpdateVotePayload{
			Name:                       "Board Election",
			Description:                "Annual board election",
			EndTime:                    "2026-03-01T17:00:00Z",
			EndTimeTimezone:            "America/New_York",
			CommitteeUids:              []string{"comm-2", "comm-3"},
			CommitteeFilters:           []string{"Voting Rep"},
			PseudoAnonymity:            true,
			PollType:                   utils.StringPtr("meek_stv"),
			NumWinners:                 utils.IntPtr(3),
			AllowAbstain:               true,
			QuorumPercentage:           utils.IntPtr(50),
			WinningThresholdPercentage: utils.IntPtr(66),
		}
	}

	t.Run("maps every scalar field", func(t *testing.T) {
		req := ConvertUpdateVotePayloadToDomain(basePayload())

		assert.Equal(t, "Board Election", req.Name)
		assert.Equal(t, "Annual board election", req.Description)
		assert.Equal(t, "2026-03-01T17:00:00Z", req.EndTime)
		assert.Equal(t, "America/New_York", req.EndTimeTimezone)
		assert.Equal(t, []string{"comm-2", "comm-3"}, req.CommitteeUIDs)
		assert.Equal(t, []string{"Voting Rep"}, req.CommitteeFilters)
		assert.True(t, req.PseudoAnonymity)
		assert.Equal(t, "meek_stv", req.PollType)
		require.NotNil(t, req.NumWinners)
		assert.Equal(t, 3, *req.NumWinners)
		assert.True(t, req.AllowAbstain)
		require.NotNil(t, req.QuorumPercentage)
		assert.Equal(t, 50, *req.QuorumPercentage)
		require.NotNil(t, req.WinningThresholdPercentage)
		assert.Equal(t, 66, *req.WinningThresholdPercentage)
	})

	t.Run("unset optional pointers stay nil/empty", func(t *testing.T) {
		p := basePayload()
		p.PollType = nil
		p.NumWinners = nil
		p.QuorumPercentage = nil
		p.WinningThresholdPercentage = nil

		req := ConvertUpdateVotePayloadToDomain(p)

		assert.Nil(t, req.QuorumPercentage)
		assert.Nil(t, req.WinningThresholdPercentage)
		assert.Nil(t, req.NumWinners, "omitted num_winners must not become 0")
		assert.Empty(t, req.PollType)
		assert.Empty(t, req.ProjectUID)
		assert.Empty(t, req.CommitteeUID)
		assert.Empty(t, req.PollQuestions)
		assert.Empty(t, req.PollCommentPrompts)
	})

	t.Run("dereferences project and committee UIDs when present", func(t *testing.T) {
		p := basePayload()
		p.ProjectUID = utils.StringPtr("proj-1")
		p.CommitteeUID = utils.StringPtr("comm-1")

		req := ConvertUpdateVotePayloadToDomain(p)

		assert.Equal(t, "proj-1", req.ProjectUID)
		assert.Equal(t, "comm-1", req.CommitteeUID)
	})

	t.Run("converts poll questions with choices", func(t *testing.T) {
		p := basePayload()
		p.PollQuestions = []*votesvc.PollQuestion{
			{
				Prompt: "Who should serve?",
				Type:   "ranked_choice",
				Choices: []*votesvc.PollChoice{
					{ChoiceText: "Alice Example"},
					{ChoiceText: "Bob Fixture"},
				},
			},
			{Prompt: "Approve budget?", Type: "single_choice"},
		}

		req := ConvertUpdateVotePayloadToDomain(p)

		require.Len(t, req.PollQuestions, 2)
		assert.Equal(t, "Who should serve?", req.PollQuestions[0].Prompt)
		assert.Equal(t, "ranked_choice", req.PollQuestions[0].Type)
		require.Len(t, req.PollQuestions[0].Choices, 2)
		assert.Equal(t, "Alice Example", req.PollQuestions[0].Choices[0].ChoiceText)
		assert.Equal(t, "Bob Fixture", req.PollQuestions[0].Choices[1].ChoiceText)
		assert.Equal(t, "Approve budget?", req.PollQuestions[1].Prompt)
		assert.Empty(t, req.PollQuestions[1].Choices)
	})

	t.Run("converts comment prompts", func(t *testing.T) {
		p := basePayload()
		p.PollCommentPrompts = []*votesvc.PollCommentPromptInput{{Prompt: "Why?"}, {Prompt: "Anything else?"}}

		req := ConvertUpdateVotePayloadToDomain(p)

		require.Len(t, req.PollCommentPrompts, 2)
		assert.Equal(t, "Why?", req.PollCommentPrompts[0].Prompt)
		assert.Equal(t, "Anything else?", req.PollCommentPrompts[1].Prompt)
	})
}

func TestConvertPollResponseToVoteResult_OptionalFieldsAndPrompts(t *testing.T) {
	base := func() *itx.PollResponse {
		return &itx.PollResponse{
			PollID:                        "poll-1",
			Name:                          "Vote",
			Description:                   "desc",
			CreationTime:                  "2026-01-01T00:00:00Z",
			LastModifiedTime:              "2026-01-02T00:00:00Z",
			EndTime:                       "2026-02-01T00:00:00Z",
			Status:                        "active",
			ProjectID:                     "proj-1",
			ProjectName:                   "Test Project",
			CommitteeID:                   "comm-1",
			CommitteeName:                 "Test Committee",
			CommitteeType:                 "board",
			CommitteeVotingStatus:         true,
			CommitteeFilters:              []string{"Voting Rep"},
			PseudoAnonymity:               true,
			TotalVotingRequestInvitations: 10,
			NumResponseReceived:           4,
			PollType:                      "generic",
			NumWinners:                    1,
			AllowAbstain:                  true,
		}
	}

	t.Run("maps scalar fields to pointer fields", func(t *testing.T) {
		got := ConvertPollResponseToVoteResult(base())

		assert.Equal(t, "poll-1", got.UID)
		assert.Equal(t, "Vote", got.Name)
		assert.Equal(t, "desc", got.Description)
		require.NotNil(t, got.CreationTime)
		assert.Equal(t, "2026-01-01T00:00:00Z", *got.CreationTime)
		require.NotNil(t, got.LastModifiedTime)
		assert.Equal(t, "2026-01-02T00:00:00Z", *got.LastModifiedTime)
		require.NotNil(t, got.EndTime)
		assert.Equal(t, "2026-02-01T00:00:00Z", *got.EndTime)
		assert.Equal(t, "active", got.Status)
		assert.Equal(t, "proj-1", got.ProjectUID)
		assert.Equal(t, "comm-1", got.CommitteeUID)
		assert.Equal(t, "Test Project", *got.ProjectName)
		assert.Equal(t, "Test Committee", *got.CommitteeName)
		assert.Equal(t, "board", *got.CommitteeType)
		assert.True(t, *got.CommitteeVotingStatus)
		assert.Equal(t, []string{"Voting Rep"}, got.CommitteeFilters)
		assert.True(t, *got.PseudoAnonymity)
		assert.Equal(t, 10, *got.TotalVotingRequestInvitations)
		assert.Equal(t, 4, *got.NumResponseReceived)
		assert.Equal(t, "generic", got.PollType)
		assert.Equal(t, 1, *got.NumWinners)
		assert.True(t, *got.AllowAbstain)
	})

	t.Run("early_end_time is emitted only when it is a valid timestamp", func(t *testing.T) {
		valid := base()
		valid.EarlyEndTime = "2026-01-20T00:00:00Z"
		got := ConvertPollResponseToVoteResult(valid)
		require.NotNil(t, got.EarlyEndTime)
		assert.Equal(t, "2026-01-20T00:00:00Z", *got.EarlyEndTime)

		for _, invalid := range []string{"", "not-a-time", "0001-01-01T00:00:00Z"} {
			p := base()
			p.EarlyEndTime = invalid
			assert.Nil(t, ConvertPollResponseToVoteResult(p).EarlyEndTime, "early_end_time %q", invalid)
		}
	})

	t.Run("end_time_timezone is omitted when empty", func(t *testing.T) {
		assert.Nil(t, ConvertPollResponseToVoteResult(base()).EndTimeTimezone)
	})

	t.Run("num_winners is omitted when zero", func(t *testing.T) {
		p := base()
		p.NumWinners = 0
		assert.Nil(t, ConvertPollResponseToVoteResult(p).NumWinners)
	})

	t.Run("converts comment prompts including server-assigned IDs", func(t *testing.T) {
		p := base()
		p.PollCommentPrompts = []itx.PollCommentPromptOutput{
			{PromptID: "prompt-1", Prompt: "Why?"},
			{PromptID: "prompt-2", Prompt: "Anything else?"},
		}

		got := ConvertPollResponseToVoteResult(p)

		require.Len(t, got.PollCommentPrompts, 2)
		assert.Equal(t, "prompt-1", got.PollCommentPrompts[0].PromptID)
		assert.Equal(t, "Why?", got.PollCommentPrompts[0].Prompt)
		assert.Equal(t, "prompt-2", got.PollCommentPrompts[1].PromptID)
		assert.Equal(t, "Anything else?", got.PollCommentPrompts[1].Prompt)
	})

	t.Run("no comment prompts leaves the field nil", func(t *testing.T) {
		assert.Nil(t, ConvertPollResponseToVoteResult(base()).PollCommentPrompts)
	})
}

func TestConvertVoteResultsToResult(t *testing.T) {
	t.Run("maps summary counts and optional timezone", func(t *testing.T) {
		got := ConvertVoteResultsToResult(&itx.VoteResults{
			NumRecipients:       20,
			NumVotesCast:        15,
			NumAbstained:        utils.IntPtr(2),
			PollEndTime:         "2026-02-01T00:00:00Z",
			PollEndTimeTimezone: "Europe/Berlin",
		})

		assert.Equal(t, 20, got.NumRecipients)
		assert.Equal(t, 15, got.NumVotesCast)
		require.NotNil(t, got.NumAbstained)
		assert.Equal(t, 2, *got.NumAbstained)
		require.NotNil(t, got.PollEndTime)
		assert.Equal(t, "2026-02-01T00:00:00Z", *got.PollEndTime)
		require.NotNil(t, got.PollEndTimeTimezone)
		assert.Equal(t, "Europe/Berlin", *got.PollEndTimeTimezone)
		assert.Nil(t, got.PollResults)
		assert.Nil(t, got.CommentResults)
	})

	t.Run("passes results_held through", func(t *testing.T) {
		assert.Nil(t, ConvertVoteResultsToResult(&itx.VoteResults{}).ResultsHeld)

		got := ConvertVoteResultsToResult(&itx.VoteResults{ResultsHeld: utils.BoolPtr(true)})
		require.NotNil(t, got.ResultsHeld)
		assert.True(t, *got.ResultsHeld)
	})

	t.Run("empty timezone is omitted", func(t *testing.T) {
		got := ConvertVoteResultsToResult(&itx.VoteResults{})
		assert.Nil(t, got.PollEndTimeTimezone)
	})

	t.Run("converts generic choice results", func(t *testing.T) {
		got := ConvertVoteResultsToResult(&itx.VoteResults{
			PollResults: []itx.PollResult{{
				Question: itx.PollQuestionDetail{
					QuestionID: "q1", Prompt: "Approve?", Type: "single_choice",
					Choices: []itx.PollChoiceOutput{{ChoiceID: "yes", ChoiceText: "Yes"}, {ChoiceID: "no", ChoiceText: "No"}},
				},
				GenericChoiceVotes: []itx.GenericChoiceVote{
					{ChoiceID: "yes", VoteCount: 9, Percentage: 60},
					{ChoiceID: "no", VoteCount: 6, Percentage: 40},
				},
			}},
		})

		require.Len(t, got.PollResults, 1)
		item := got.PollResults[0]
		assert.Equal(t, "q1", item.Question.QuestionID)
		assert.Equal(t, "Approve?", item.Question.Prompt)
		assert.Equal(t, "single_choice", item.Question.Type)
		require.Len(t, item.Question.Choices, 2)
		assert.Equal(t, "yes", *item.Question.Choices[0].ChoiceID)
		assert.Equal(t, "Yes", item.Question.Choices[0].ChoiceText)
		require.Len(t, item.GenericChoiceVotes, 2)
		assert.Equal(t, "yes", item.GenericChoiceVotes[0].ChoiceID)
		assert.Equal(t, 9, item.GenericChoiceVotes[0].VoteCount)
		assert.InDelta(t, 60, item.GenericChoiceVotes[0].Percentage, 1e-9)
		assert.Nil(t, item.RankedChoiceVotes)
		assert.Nil(t, item.RankedChoiceWinnerInfo)
		assert.Nil(t, item.IrvRoundSummary)
		assert.Nil(t, item.MeekStvRoundSummary)
	})

	t.Run("converts ranked choice votes, winner info and IRV rounds", func(t *testing.T) {
		got := ConvertVoteResultsToResult(&itx.VoteResults{
			PollResults: []itx.PollResult{{
				RankedChoiceVotes: []itx.RankedChoiceVote{{
					ChoiceID:   "a",
					RankCounts: []itx.RankCount{{Rank: 1, Count: 7}, {Rank: 2, Count: 3}},
					CondorcetMatrix: []itx.CondorcetMatrixEntry{
						{ChoiceID: "a", OtherChoiceID: "b", ChoiceIDWins: 8, OtherChoiceIDWins: 2, Result: "win"},
					},
				}},
				RankedChoiceWinnerInfo: &itx.RankedChoiceWinnerInfo{
					PollChoices:                     []itx.PollChoiceOutput{{ChoiceID: "a", ChoiceText: "Alice Example"}},
					CondorcetIRVUsedForEliminations: true,
				},
				IRVRoundSummary: []itx.IRVRoundSummary{{
					RoundNumber: 1, TotalVotes: 10, MinVotes: 2, ExhaustedVotes: 1, Threshold: 6,
					EliminatedChoiceID: "c", Message: "c eliminated",
					Votes:            []itx.VoteCount{{ChoiceID: "a", VoteCount: 6, Percentage: 60}},
					TransferredVotes: []itx.VoteCount{{ChoiceID: "b", VoteCount: 1, Percentage: 10}},
				}},
			}},
		})

		require.Len(t, got.PollResults, 1)
		item := got.PollResults[0]

		require.Len(t, item.RankedChoiceVotes, 1)
		rcv := item.RankedChoiceVotes[0]
		assert.Equal(t, "a", rcv.ChoiceID)
		require.Len(t, rcv.RankCounts, 2)
		assert.Equal(t, 1, rcv.RankCounts[0].Rank)
		assert.Equal(t, 7, rcv.RankCounts[0].Count)
		require.Len(t, rcv.CondorcetMatrix, 1)
		cm := rcv.CondorcetMatrix[0]
		assert.Equal(t, "a", cm.ChoiceID)
		assert.Equal(t, "b", cm.OtherChoiceID)
		assert.Equal(t, 8, cm.ChoiceIDWins)
		assert.Equal(t, 2, cm.OtherChoiceIDWins)
		assert.Equal(t, "win", cm.Result)

		require.NotNil(t, item.RankedChoiceWinnerInfo)
		require.NotNil(t, item.RankedChoiceWinnerInfo.CondorcetIrvUsedForEliminations)
		assert.True(t, *item.RankedChoiceWinnerInfo.CondorcetIrvUsedForEliminations)
		require.Len(t, item.RankedChoiceWinnerInfo.PollChoices, 1)
		assert.Equal(t, "Alice Example", item.RankedChoiceWinnerInfo.PollChoices[0].ChoiceText)

		require.Len(t, item.IrvRoundSummary, 1)
		irv := item.IrvRoundSummary[0]
		assert.Equal(t, 1, irv.RoundNumber)
		assert.Equal(t, 10, irv.TotalVotes)
		require.NotNil(t, irv.MinVotes)
		assert.Equal(t, 2, *irv.MinVotes)
		assert.Equal(t, 1, irv.ExhaustedVotes)
		assert.Equal(t, 6, irv.Threshold)
		require.NotNil(t, irv.EliminatedChoiceID)
		assert.Equal(t, "c", *irv.EliminatedChoiceID)
		assert.Equal(t, "c eliminated", irv.Message)
		require.Len(t, irv.Votes, 1)
		assert.Equal(t, "a", irv.Votes[0].ChoiceID)
		assert.Equal(t, 6, irv.Votes[0].VoteCount)
		assert.InDelta(t, 60, *irv.Votes[0].Percentage, 1e-9)
		require.Len(t, irv.TransferredVotes, 1)
		assert.Equal(t, "b", irv.TransferredVotes[0].ChoiceID)
	})

	t.Run("each IRV round keeps its own pointer fields", func(t *testing.T) {
		got := ConvertVoteResultsToResult(&itx.VoteResults{
			PollResults: []itx.PollResult{{
				IRVRoundSummary: []itx.IRVRoundSummary{
					{RoundNumber: 1, MinVotes: 1, EliminatedChoiceID: "x", Votes: []itx.VoteCount{{ChoiceID: "a", Percentage: 10}, {ChoiceID: "b", Percentage: 20}}},
					{RoundNumber: 2, MinVotes: 5, EliminatedChoiceID: "y"},
				},
			}},
		})

		rounds := got.PollResults[0].IrvRoundSummary
		require.Len(t, rounds, 2)
		assert.Equal(t, 1, *rounds[0].MinVotes)
		assert.Equal(t, "x", *rounds[0].EliminatedChoiceID)
		assert.Equal(t, 5, *rounds[1].MinVotes)
		assert.Equal(t, "y", *rounds[1].EliminatedChoiceID)
		assert.InDelta(t, 10, *rounds[0].Votes[0].Percentage, 1e-9)
		assert.InDelta(t, 20, *rounds[0].Votes[1].Percentage, 1e-9)
	})

	t.Run("converts Meek STV rounds", func(t *testing.T) {
		got := ConvertVoteResultsToResult(&itx.VoteResults{
			PollResults: []itx.PollResult{{
				MeekSTVRoundSummary: []itx.MeekSTVRoundSummary{{
					RoundNumber: 2, TotalVotes: 30, ExhaustedVotes: 3, Threshold: 11, Message: "round 2",
					Votes:             []itx.MeekSTVVoteCount{{ChoiceID: "a", VoteCount: 12}},
					ElectedChoices:    []itx.MeekSTVElectedChoice{{ChoiceID: "a", VoteCount: 12, SurplusVotes: 1}},
					EliminatedChoices: []itx.MeekSTVVoteCount{{ChoiceID: "c", VoteCount: 2}},
					TransferredVotes:  []itx.MeekSTVVoteCount{{ChoiceID: "b", VoteCount: 1}},
					SurplusVotes:      []itx.MeekSTVVoteCount{{ChoiceID: "a", VoteCount: 1}},
				}},
			}},
		})

		require.Len(t, got.PollResults[0].MeekStvRoundSummary, 1)
		m := got.PollResults[0].MeekStvRoundSummary[0]
		assert.Equal(t, 2, m.RoundNumber)
		assert.Equal(t, 30, m.TotalVotes)
		assert.Equal(t, 3, m.ExhaustedVotes)
		assert.Equal(t, 11, m.Threshold)
		assert.Equal(t, "round 2", m.Message)
		require.Len(t, m.Votes, 1)
		assert.Equal(t, "a", m.Votes[0].ChoiceID)
		assert.Equal(t, 12, m.Votes[0].VoteCount)
		require.Len(t, m.ElectedChoices, 1)
		assert.Equal(t, 1, m.ElectedChoices[0].SurplusVotes)
		require.Len(t, m.EliminatedChoices, 1)
		assert.Equal(t, "c", m.EliminatedChoices[0].ChoiceID)
		require.Len(t, m.TransferredVotes, 1)
		assert.Equal(t, "b", m.TransferredVotes[0].ChoiceID)
		require.Len(t, m.SurplusVotes, 1)
		assert.Equal(t, "a", m.SurplusVotes[0].ChoiceID)
	})

	t.Run("comment results hide identity fields under pseudo-anonymity", func(t *testing.T) {
		got := ConvertVoteResultsToResult(&itx.VoteResults{
			CommentResults: []itx.CommentResult{{
				Prompt: itx.PollCommentPromptOutput{PromptID: "prompt-1", Prompt: "Why?"},
				Responses: []itx.CommentResponseWithUser{
					{VoteID: utils.StringPtr("v1"), CommentText: "named", UserID: "alice", UserName: "Alice Example", ProfilePicture: "https://example.com/a.png", VoteCreationTime: utils.StringPtr("2026-01-05T00:00:00Z")},
					{VoteID: utils.StringPtr("v2"), CommentText: "anonymous", VoteCreationTime: utils.StringPtr("2026-01-06T00:00:00Z"), Abstained: utils.BoolPtr(true)},
				},
			}, {
				Prompt: itx.PollCommentPromptOutput{PromptID: "prompt-2", Prompt: "No responses"},
			}},
		})

		require.Len(t, got.CommentResults, 2)
		first := got.CommentResults[0]
		assert.Equal(t, "prompt-1", first.Prompt.PromptID)
		assert.Equal(t, "Why?", first.Prompt.Prompt)
		require.Len(t, first.Responses, 2)

		named := first.Responses[0]
		assert.Equal(t, utils.StringPtr("v1"), named.VoteID)
		assert.Equal(t, "named", named.CommentText)
		assert.Equal(t, utils.StringPtr("2026-01-05T00:00:00Z"), named.VoteCreationTime)
		assert.Nil(t, named.Abstained, "unset abstained stays absent")
		require.NotNil(t, named.UserID)
		assert.Equal(t, "alice", *named.UserID)
		require.NotNil(t, named.UserName)
		assert.Equal(t, "Alice Example", *named.UserName)
		require.NotNil(t, named.ProfilePicture)
		assert.Equal(t, "https://example.com/a.png", *named.ProfilePicture)

		anon := first.Responses[1]
		assert.Equal(t, utils.StringPtr("v2"), anon.VoteID)
		assert.Equal(t, utils.BoolPtr(true), anon.Abstained)
		assert.Nil(t, anon.UserID, "absent, not empty string")
		assert.Nil(t, anon.UserName)
		assert.Nil(t, anon.ProfilePicture)

		assert.Nil(t, got.CommentResults[1].Responses)
	})
}
