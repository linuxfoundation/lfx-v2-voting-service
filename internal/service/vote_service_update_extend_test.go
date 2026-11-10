// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/models/itx"
	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/utils"
)

// failingPollClient returns a configured error (and optionally a response) from UpdatePoll
// and ExtendPoll.
type failingPollClient struct {
	domain.PollClient
	resp *itx.PollResponse
	err  error
}

func (c *failingPollClient) UpdatePoll(_ context.Context, _ string, _ *itx.UpdatePollRequest) (*itx.PollResponse, error) {
	return c.resp, c.err
}

func (c *failingPollClient) ExtendPoll(_ context.Context, _ string, _ *itx.ExtendPollRequest) (*itx.PollResponse, error) {
	return c.resp, c.err
}

func TestVoteService_UpdateVote_MapsRequestToITX(t *testing.T) {
	client := &capturePollClient{}
	mapper := &directionIDMapper{}
	svc := NewVoteService(nil, client, mapper, discardLogger())

	_, err := svc.UpdateVote(ctxWithPrincipal("alice"), "vote-1", &UpdateVoteRequest{
		Name:                       "Board Vote",
		Description:                "Annual vote",
		EndTime:                    "2026-03-01T17:00:00Z",
		EndTimeTimezone:            "America/New_York",
		ProjectUID:                 "proj-1",
		CommitteeUID:               "comm-1",
		CommitteeUIDs:              []string{"comm-2", "comm-3"},
		CommitteeFilters:           []string{"Voting Rep"},
		PseudoAnonymity:            true,
		PollType:                   "meek_stv",
		NumWinners:                 utils.IntPtr(3),
		AllowAbstain:               true,
		QuorumPercentage:           utils.IntPtr(50),
		WinningThresholdPercentage: utils.IntPtr(66),
		PollQuestions: []PollQuestionRequest{
			{Prompt: "Who?", Type: "ranked_choice", Choices: []PollChoiceRequest{{ChoiceText: "Alice Example"}, {ChoiceText: "Bob Fixture"}}},
		},
		PollCommentPrompts: []PollCommentPromptRequest{{Prompt: "Why?"}},
	})

	require.NoError(t, err)
	req := client.lastUpdate
	require.NotNil(t, req)
	assert.Equal(t, "Board Vote", req.Name)
	assert.Equal(t, "Annual vote", req.Description)
	assert.Equal(t, "2026-03-01T17:00:00Z", req.EndTime)
	assert.Equal(t, "America/New_York", req.EndTimeTimezone)
	assert.Equal(t, "project-v1/proj-1", req.ProjectID, "v2 UIDs must be mapped to v1 IDs before reaching ITX")
	assert.Equal(t, "committee-v1/comm-1", req.CommitteeID)
	assert.Equal(t, []string{"committee-v1/comm-2", "committee-v1/comm-3"}, req.CommitteeIDs)
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

	require.Len(t, req.PollQuestions, 1)
	assert.Equal(t, "Who?", req.PollQuestions[0].Prompt)
	assert.Equal(t, "ranked_choice", req.PollQuestions[0].Type)
	assert.Equal(t, []string{"Alice Example", "Bob Fixture"}, req.PollQuestions[0].Choices)
	require.Len(t, req.PollCommentPrompts, 1)
	assert.Equal(t, "Why?", req.PollCommentPrompts[0].Prompt)
}

func TestVoteService_UpdateVote_ResponseIDsAreMappedBack(t *testing.T) {
	client := &failingPollClient{resp: &itx.PollResponse{PollID: "poll-1", ProjectID: "p1", CommitteeID: "c1"}}
	svc := NewVoteService(nil, client, &directionIDMapper{}, discardLogger())

	got, err := svc.UpdateVote(ctxWithPrincipal("alice"), "vote-1", &UpdateVoteRequest{ProjectUID: "proj-1"})

	require.NoError(t, err)
	assert.Equal(t, "project-v2/p1", got.ProjectID)
	assert.Equal(t, "committee-v2/c1", got.CommitteeID)
}

func TestVoteService_UpdateVote_Errors(t *testing.T) {
	t.Run("an ID mapping failure stops before ITX is called", func(t *testing.T) {
		client := &capturePollClient{}
		svc := NewVoteService(nil, client, &directionIDMapper{failOn: map[string]error{"proj-1": domain.NewNotFoundError("no such project")}}, discardLogger())

		got, err := svc.UpdateVote(ctxWithPrincipal("alice"), "vote-1", &UpdateVoteRequest{ProjectUID: "proj-1"})

		require.Error(t, err)
		assert.Nil(t, got)
		assert.Equal(t, domain.ErrorTypeNotFound, domain.GetErrorType(err))
		assert.Nil(t, client.lastUpdate)
	})

	t.Run("an ITX error is returned unchanged", func(t *testing.T) {
		client := &failingPollClient{err: domain.NewConflictError("vote is active")}
		svc := NewVoteService(nil, client, &directionIDMapper{}, discardLogger())

		got, err := svc.UpdateVote(ctxWithPrincipal("alice"), "vote-1", &UpdateVoteRequest{ProjectUID: "proj-1"})

		require.Error(t, err)
		assert.Nil(t, got)
		assert.Equal(t, domain.ErrorTypeConflict, domain.GetErrorType(err))
	})

	t.Run("a response ID mapping failure is returned", func(t *testing.T) {
		client := &failingPollClient{resp: &itx.PollResponse{PollID: "poll-1", ProjectID: "p1"}}
		svc := NewVoteService(nil, client, &directionIDMapper{failOn: map[string]error{"p1": domain.NewInternalError("mapper down")}}, discardLogger())

		got, err := svc.UpdateVote(ctxWithPrincipal("alice"), "vote-1", &UpdateVoteRequest{})

		require.Error(t, err)
		assert.Nil(t, got)
		assert.Equal(t, domain.ErrorTypeInternal, domain.GetErrorType(err))
	})

	t.Run("a name over 200 characters is rejected before any lookup", func(t *testing.T) {
		client := &capturePollClient{}
		mapper := &directionIDMapper{}
		svc := NewVoteService(nil, client, mapper, discardLogger())

		_, err := svc.UpdateVote(ctxWithPrincipal("alice"), "vote-1", &UpdateVoteRequest{Name: strings.Repeat("x", 201), ProjectUID: "proj-1"})

		assertValidationError(t, err)
		assert.Empty(t, mapper.calls)
		assert.Nil(t, client.lastUpdate)
	})
}

func TestVoteService_ExtendVote_Errors(t *testing.T) {
	t.Run("requires a principal", func(t *testing.T) {
		client := &capturePollClient{}
		svc := NewVoteService(nil, client, identityIDMapper{}, discardLogger())

		got, err := svc.ExtendVote(context.Background(), "vote-1", "2026-03-01T17:00:00Z", "UTC")

		assertValidationError(t, err)
		assert.Nil(t, got)
		assert.Nil(t, client.lastExtend)
	})

	t.Run("an ITX error is returned unchanged", func(t *testing.T) {
		svc := NewVoteService(nil, &failingPollClient{err: domain.NewValidationError("vote already ended")}, identityIDMapper{}, discardLogger())

		got, err := svc.ExtendVote(ctxWithPrincipal("alice"), "vote-1", "2026-03-01T17:00:00Z", "UTC")

		require.Error(t, err)
		assert.Nil(t, got)
		assert.Equal(t, "vote already ended", err.Error())
	})

	t.Run("response IDs are mapped back to v2 UIDs", func(t *testing.T) {
		client := &failingPollClient{resp: &itx.PollResponse{PollID: "poll-1", ProjectID: "p1", CommitteeID: "c1"}}
		svc := NewVoteService(nil, client, &directionIDMapper{}, discardLogger())

		got, err := svc.ExtendVote(ctxWithPrincipal("alice"), "vote-1", "2026-03-01T17:00:00Z", "UTC")

		require.NoError(t, err)
		assert.Equal(t, "project-v2/p1", got.ProjectID)
		assert.Equal(t, "committee-v2/c1", got.CommitteeID)
	})

	t.Run("a response ID mapping failure is returned", func(t *testing.T) {
		client := &failingPollClient{resp: &itx.PollResponse{PollID: "poll-1", CommitteeID: "c1"}}
		svc := NewVoteService(nil, client, &directionIDMapper{failOn: map[string]error{"c1": domain.NewNotFoundError("no such committee")}}, discardLogger())

		got, err := svc.ExtendVote(ctxWithPrincipal("alice"), "vote-1", "2026-03-01T17:00:00Z", "UTC")

		require.Error(t, err)
		assert.Nil(t, got)
		assert.Equal(t, domain.ErrorTypeNotFound, domain.GetErrorType(err))
	})
}
