// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

// Regression tests for the converter layer (cmd/voting-api/service/).
//
// The converters rename ITX (v1) field names to LFXv2 API field names.
// An accidental rename in either the ITX model or the Goa type would break
// the API contract silently — the response would compile but return the wrong
// field to clients. These tests pin the renames so a refactor that changes one
// side without updating the converter fails immediately.

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	votesvc "github.com/linuxfoundation/lfx-v2-voting-service/gen/vote"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/service"
	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/models/itx"
	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/utils"
)

// ---- ConvertPollResponseToVoteResult ----------------------------------------

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

// ---- ConvertCreateVotePayloadToDomain ---------------------------------------

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

// ---- ConvertVoteResponseToResult --------------------------------------------

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

// ---- Null comment_responses validation --------------------------------------

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
			EndTime: "2026-12-01T00:00:00Z", ProjectID: "a0941000002wBz4AAE", CommitteeID: "c1",
			CommitteeName: "TSC", TotalVotingRequestInvitations: 9, NumResponseReceived: 4,
			PseudoAnonymity: pseudo, PollType: "generic", AllowAbstain: true,
			PollQuestions: []itx.PollQuestionOutput{{QuestionID: "q1", Prompt: "Who?", Type: "single_choice",
				Choices: []itx.PollChoiceOutput{{ChoiceID: "c-a", ChoiceText: "Alice"}}}},
		},
		Ballot: &itx.VoteResponse{
			VoteID: "ballot-1", PollID: "vote-1", VoteStatus: status, Abstained: false, UserEmail: "voter@example.org",
			PollAnswers:      []itx.PollAnswer{{QuestionID: "q1", Prompt: "Who?", Type: "single_choice", UserChoice: []itx.PollChoiceAnswer{{ChoiceID: "c-a", ChoiceText: "Alice"}}}},
			CommentResponses: []itx.CommentResponse{{PromptID: "p1", CommentText: "Great"}},
		},
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
		if b.AlreadyVoted || b.PollAnswers != nil || b.Abstained != nil {
			t.Errorf("unexpected ballot contents before voting: %+v", b)
		}
	})

	t.Run("no project, committee, counts or PII in the JSON", func(t *testing.T) {
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
