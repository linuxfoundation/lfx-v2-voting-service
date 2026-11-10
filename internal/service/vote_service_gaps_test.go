// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/models/itx"
	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingPollClient records the IDs/arguments passed to the poll operations that
// have no dedicated capture client and returns canned values.
type recordingPollClient struct {
	domain.PollClient

	err error

	deleteID  string
	enableID  string
	resultsID string
	results   *itx.VoteResults
}

func (c *recordingPollClient) DeletePoll(_ context.Context, id string) error {
	c.deleteID = id
	return c.err
}

func (c *recordingPollClient) EnablePoll(_ context.Context, id string) error {
	c.enableID = id
	return c.err
}

func (c *recordingPollClient) GetPollResults(_ context.Context, id string) (*itx.VoteResults, error) {
	c.resultsID = id
	if c.err != nil {
		return nil, c.err
	}
	return c.results, nil
}

// directionIDMapper maps IDs by adding a direction-specific prefix so tests can tell
// exactly which mapper method was called for which field. Any ID listed in failOn
// returns an error instead.
type directionIDMapper struct {
	failOn map[string]error
	calls  []string
}

func (m *directionIDMapper) do(kind, id string) (string, error) {
	m.calls = append(m.calls, kind+":"+id)
	if err, ok := m.failOn[id]; ok {
		return "", err
	}
	return kind + "/" + id, nil
}

func (m *directionIDMapper) MapProjectV2ToV1(_ context.Context, id string) (string, error) {
	return m.do("project-v1", id)
}
func (m *directionIDMapper) MapProjectV1ToV2(_ context.Context, id string) (string, error) {
	return m.do("project-v2", id)
}
func (m *directionIDMapper) MapCommitteeV2ToV1(_ context.Context, id string) (string, error) {
	return m.do("committee-v1", id)
}
func (m *directionIDMapper) MapCommitteeV1ToV2(_ context.Context, id string) (string, error) {
	return m.do("committee-v2", id)
}

func TestVoteService_DeleteVote(t *testing.T) {
	t.Run("forwards the vote ID to ITX", func(t *testing.T) {
		client := &recordingPollClient{}
		svc := NewVoteService(nil, client, identityIDMapper{}, discardLogger())

		err := svc.DeleteVote(ctxWithPrincipal("alice"), "vote-1")

		require.NoError(t, err)
		assert.Equal(t, "vote-1", client.deleteID)
	})

	t.Run("propagates the proxy error unchanged", func(t *testing.T) {
		proxyErr := domain.NewNotFoundError("vote not found")
		svc := NewVoteService(nil, &recordingPollClient{err: proxyErr}, identityIDMapper{}, discardLogger())

		err := svc.DeleteVote(ctxWithPrincipal("alice"), "vote-1")

		require.Error(t, err)
		assert.Equal(t, domain.ErrorTypeNotFound, domain.GetErrorType(err))
	})

	t.Run("does not call ITX without a principal", func(t *testing.T) {
		client := &recordingPollClient{}
		svc := NewVoteService(nil, client, identityIDMapper{}, discardLogger())

		err := svc.DeleteVote(context.Background(), "vote-1")

		assertValidationError(t, err)
		assert.Empty(t, client.deleteID)
	})
}

func TestVoteService_EnableVote(t *testing.T) {
	t.Run("forwards the vote ID to ITX", func(t *testing.T) {
		client := &recordingPollClient{}
		svc := NewVoteService(nil, client, identityIDMapper{}, discardLogger())

		err := svc.EnableVote(ctxWithPrincipal("alice"), "vote-2")

		require.NoError(t, err)
		assert.Equal(t, "vote-2", client.enableID)
	})

	t.Run("propagates the proxy error unchanged", func(t *testing.T) {
		proxyErr := domain.NewConflictError("vote already enabled")
		svc := NewVoteService(nil, &recordingPollClient{err: proxyErr}, identityIDMapper{}, discardLogger())

		err := svc.EnableVote(ctxWithPrincipal("alice"), "vote-2")

		require.Error(t, err)
		assert.Equal(t, domain.ErrorTypeConflict, domain.GetErrorType(err))
	})

	t.Run("does not call ITX without a principal", func(t *testing.T) {
		client := &recordingPollClient{}
		svc := NewVoteService(nil, client, identityIDMapper{}, discardLogger())

		err := svc.EnableVote(context.Background(), "vote-2")

		assertValidationError(t, err)
		assert.Empty(t, client.enableID)
	})
}

func TestVoteService_GetVoteResults(t *testing.T) {
	t.Run("returns the ITX results for the vote", func(t *testing.T) {
		want := &itx.VoteResults{NumRecipients: 10, NumVotesCast: 7, NumAbstained: utils.IntPtr(1), PollEndTime: "2026-01-02T03:04:05Z"}
		client := &recordingPollClient{results: want}
		svc := NewVoteService(nil, client, identityIDMapper{}, discardLogger())

		got, err := svc.GetVoteResults(ctxWithPrincipal("alice"), "vote-3")

		require.NoError(t, err)
		assert.Same(t, want, got)
		assert.Equal(t, "vote-3", client.resultsID)
	})

	t.Run("propagates the proxy error and returns no results", func(t *testing.T) {
		proxyErr := domain.NewUnavailableError("itx down")
		svc := NewVoteService(nil, &recordingPollClient{err: proxyErr}, identityIDMapper{}, discardLogger())

		got, err := svc.GetVoteResults(ctxWithPrincipal("alice"), "vote-3")

		require.Error(t, err)
		assert.Nil(t, got)
		assert.Equal(t, domain.ErrorTypeUnavailable, domain.GetErrorType(err))
	})

	t.Run("does not call ITX without a principal", func(t *testing.T) {
		client := &recordingPollClient{}
		svc := NewVoteService(nil, client, identityIDMapper{}, discardLogger())

		got, err := svc.GetVoteResults(context.Background(), "vote-3")

		assertValidationError(t, err)
		assert.Nil(t, got)
		assert.Empty(t, client.resultsID)
	})
}

func TestVoteService_mapPollResponseV1ToV2(t *testing.T) {
	ctx := context.Background()

	t.Run("maps both project and committee IDs", func(t *testing.T) {
		mapper := &directionIDMapper{}
		svc := NewVoteService(nil, nil, mapper, discardLogger())
		resp := &itx.PollResponse{ProjectID: "p1", CommitteeID: "c1"}

		err := svc.mapPollResponseV1ToV2(ctx, resp)

		require.NoError(t, err)
		assert.Equal(t, "project-v2/p1", resp.ProjectID)
		assert.Equal(t, "committee-v2/c1", resp.CommitteeID)
	})

	t.Run("skips the mapper for empty IDs", func(t *testing.T) {
		mapper := &directionIDMapper{}
		svc := NewVoteService(nil, nil, mapper, discardLogger())
		resp := &itx.PollResponse{}

		err := svc.mapPollResponseV1ToV2(ctx, resp)

		require.NoError(t, err)
		assert.Empty(t, mapper.calls)
		assert.Empty(t, resp.ProjectID)
		assert.Empty(t, resp.CommitteeID)
	})

	t.Run("maps only the committee when there is no project", func(t *testing.T) {
		mapper := &directionIDMapper{}
		svc := NewVoteService(nil, nil, mapper, discardLogger())
		resp := &itx.PollResponse{CommitteeID: "c1"}

		require.NoError(t, svc.mapPollResponseV1ToV2(ctx, resp))

		assert.Equal(t, []string{"committee-v2:c1"}, mapper.calls)
		assert.Empty(t, resp.ProjectID)
		assert.Equal(t, "committee-v2/c1", resp.CommitteeID)
	})

	t.Run("project mapping failure is returned and leaves the response untouched", func(t *testing.T) {
		boom := domain.NewInternalError("mapper down")
		mapper := &directionIDMapper{failOn: map[string]error{"p1": boom}}
		svc := NewVoteService(nil, nil, mapper, discardLogger())
		resp := &itx.PollResponse{ProjectID: "p1", CommitteeID: "c1"}

		err := svc.mapPollResponseV1ToV2(ctx, resp)

		require.ErrorIs(t, err, boom)
		assert.Equal(t, "p1", resp.ProjectID)
		assert.Equal(t, "c1", resp.CommitteeID)
		assert.Equal(t, []string{"project-v2:p1"}, mapper.calls, "committee must not be mapped after a project failure")
	})

	t.Run("committee mapping failure is returned", func(t *testing.T) {
		boom := domain.NewNotFoundError("no such committee")
		mapper := &directionIDMapper{failOn: map[string]error{"c1": boom}}
		svc := NewVoteService(nil, nil, mapper, discardLogger())
		resp := &itx.PollResponse{ProjectID: "p1", CommitteeID: "c1"}

		err := svc.mapPollResponseV1ToV2(ctx, resp)

		require.ErrorIs(t, err, boom)
		assert.Equal(t, "c1", resp.CommitteeID)
	})
}

func TestVoteService_mapRequestIDsV2ToV1(t *testing.T) {
	ctx := context.Background()

	t.Run("maps project, committee and committee list", func(t *testing.T) {
		mapper := &directionIDMapper{}
		svc := NewVoteService(nil, nil, mapper, discardLogger())

		project, committee, committees, err := svc.mapRequestIDsV2ToV1(ctx, "p1", "c1", []string{"c2", "c3"})

		require.NoError(t, err)
		assert.Equal(t, "project-v1/p1", project)
		assert.Equal(t, "committee-v1/c1", committee)
		assert.Equal(t, []string{"committee-v1/c2", "committee-v1/c3"}, committees, "order must be preserved")
	})

	t.Run("empty committee and nil list are not mapped", func(t *testing.T) {
		mapper := &directionIDMapper{}
		svc := NewVoteService(nil, nil, mapper, discardLogger())

		project, committee, committees, err := svc.mapRequestIDsV2ToV1(ctx, "p1", "", nil)

		require.NoError(t, err)
		assert.Equal(t, "project-v1/p1", project)
		assert.Empty(t, committee)
		assert.Nil(t, committees)
		assert.Equal(t, []string{"project-v1:p1"}, mapper.calls)
	})

	t.Run("empty list (non-nil) yields nil committee IDs", func(t *testing.T) {
		svc := NewVoteService(nil, nil, &directionIDMapper{}, discardLogger())

		_, _, committees, err := svc.mapRequestIDsV2ToV1(ctx, "p1", "", []string{})

		require.NoError(t, err)
		assert.Nil(t, committees)
	})

	failures := []struct {
		name   string
		failOn string
	}{
		{"project mapping failure", "p1"},
		{"single committee mapping failure", "c1"},
		{"committee list mapping failure", "c3"},
	}
	for _, tc := range failures {
		t.Run(tc.name+" returns zero values and the error", func(t *testing.T) {
			boom := fmt.Errorf("cannot map %s", tc.failOn)
			mapper := &directionIDMapper{failOn: map[string]error{tc.failOn: boom}}
			svc := NewVoteService(nil, nil, mapper, discardLogger())

			project, committee, committees, err := svc.mapRequestIDsV2ToV1(ctx, "p1", "c1", []string{"c2", "c3"})

			require.ErrorIs(t, err, boom)
			assert.Empty(t, project)
			assert.Empty(t, committee)
			assert.Nil(t, committees)
		})
	}
}
