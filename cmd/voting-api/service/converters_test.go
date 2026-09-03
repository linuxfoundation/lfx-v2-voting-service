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
	"errors"
	"testing"

	votesvc "github.com/linuxfoundation/lfx-v2-voting-service/gen/vote"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/models/itx"
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
// field names are correctly mapped to the service request. The two highest-risk
// renames here are CommitteeUids→CommitteeUIDs (plural, casing) and the
// NumWinners int→*int wrapping via utils.IntPtr.
func TestConvertCreateVotePayloadToDomain_FieldMapping(t *testing.T) {
	payload := &votesvc.CreateVotePayload{
		Name:          "Board Election",
		ProjectUID:    "proj-uid-123",
		CommitteeUID:  "comm-uid-456",
		CommitteeUids: []string{"a", "b"},
		NumWinners:    3,
		PollType:      "single_choice",
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
	// NumWinners must be wrapped as a non-nil pointer so it can be sent as-is to ITX.
	if req.NumWinners == nil {
		t.Fatal("NumWinners: expected non-nil pointer, got nil")
	}
	if *req.NumWinners != payload.NumWinners {
		t.Errorf("NumWinners: want %d, got %d", payload.NumWinners, *req.NumWinners)
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
