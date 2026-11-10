// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/constants"
	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/models/itx"
	goaauth "goa.design/goa/v3/security"
)

// createCaptureClient records the CreateVote request and returns a configurable error.
type createCaptureClient struct {
	domain.VoteResponseClient
	last *itx.CreateVoteRequest
	err  error
}

func (c *createCaptureClient) CreateVote(_ context.Context, req *itx.CreateVoteRequest) error {
	c.last = req
	return c.err
}

func TestVoteResponseService_CreateVoteResponse_MapsBallotToITX(t *testing.T) {
	client := &createCaptureClient{}
	svc := newResponseSvc(client, identityIDMapper{})

	err := svc.CreateVoteResponse(ctxWithPrincipal("alice"), &CreateVoteResponseRequest{
		VoteID: "vote-response-1",
		UserVoteContent: []VoteAnswerRequest{
			{QuestionID: "q1", ChoiceIDs: []string{"yes"}},
			{QuestionID: "q2", RankedChoices: []RankedChoiceRequest{{ChoiceID: "a", ChoiceRank: 1}, {ChoiceID: "b", ChoiceRank: 2}}},
		},
		Abstain: true,
		CommentResponses: []CommentResponseRequest{
			{PromptID: "p1", CommentText: "Looks good"},
		},
	})

	require.NoError(t, err)
	require.NotNil(t, client.last)
	assert.Equal(t, "vote-response-1", client.last.VoteID)
	assert.True(t, client.last.Abstain)

	require.Len(t, client.last.UserVoteContent, 2)
	assert.Equal(t, "q1", client.last.UserVoteContent[0].QuestionID)
	assert.Equal(t, []string{"yes"}, client.last.UserVoteContent[0].ChoiceIDs)
	assert.Empty(t, client.last.UserVoteContent[0].RankedChoices)
	assert.Equal(t, "q2", client.last.UserVoteContent[1].QuestionID)
	require.Len(t, client.last.UserVoteContent[1].RankedChoices, 2)
	assert.Equal(t, itx.RankedChoiceInput{ChoiceID: "a", ChoiceRank: 1}, client.last.UserVoteContent[1].RankedChoices[0])
	assert.Equal(t, itx.RankedChoiceInput{ChoiceID: "b", ChoiceRank: 2}, client.last.UserVoteContent[1].RankedChoices[1])

	require.Len(t, client.last.CommentResponses, 1)
	assert.Equal(t, itx.CommentResponse{PromptID: "p1", CommentText: "Looks good"}, client.last.CommentResponses[0])
}

func TestVoteResponseService_CreateVoteResponse_EmptyBallot(t *testing.T) {
	client := &createCaptureClient{}
	svc := newResponseSvc(client, identityIDMapper{})

	require.NoError(t, svc.CreateVoteResponse(ctxWithPrincipal("alice"), &CreateVoteResponseRequest{VoteID: "r1", Abstain: true}))

	require.NotNil(t, client.last)
	assert.NotNil(t, client.last.UserVoteContent, "always a list on the wire, never null")
	assert.Empty(t, client.last.UserVoteContent)
	assert.NotNil(t, client.last.CommentResponses)
	assert.Empty(t, client.last.CommentResponses)
	assert.True(t, client.last.Abstain)
}

func TestVoteResponseService_CreateVoteResponse_ITXErrorIsReturnedUnchanged(t *testing.T) {
	client := &createCaptureClient{err: domain.NewConflictError("ballot already submitted")}
	svc := newResponseSvc(client, identityIDMapper{})

	err := svc.CreateVoteResponse(ctxWithPrincipal("alice"), &CreateVoteResponseRequest{VoteID: "r1"})

	require.Error(t, err)
	assert.Equal(t, domain.ErrorTypeConflict, domain.GetErrorType(err))
	assert.Equal(t, "ballot already submitted", err.Error())
}

func TestVoteResponseService_JWTAuth(t *testing.T) {
	scheme := &goaauth.JWTScheme{}

	t.Run("a valid token stores the principal in the context", func(t *testing.T) {
		svc := NewVoteResponseService(&mockAuthenticator{principal: "alice"}, nil, identityIDMapper{}, discardLogger())

		ctx, err := svc.JWTAuth(context.Background(), "token", scheme)

		require.NoError(t, err)
		assert.Equal(t, "alice", ctx.Value(constants.PrincipalContextID))
	})

	t.Run("an invalid token is a validation error and leaves no principal", func(t *testing.T) {
		cause := errors.New("signature invalid")
		svc := NewVoteResponseService(&mockAuthenticator{err: cause}, nil, identityIDMapper{}, discardLogger())

		ctx, err := svc.JWTAuth(context.Background(), "token", scheme)

		require.Error(t, err)
		assert.Equal(t, domain.ErrorTypeValidation, domain.GetErrorType(err))
		assert.ErrorIs(t, err, cause)
		assert.Nil(t, ctx.Value(constants.PrincipalContextID))
	})
}
