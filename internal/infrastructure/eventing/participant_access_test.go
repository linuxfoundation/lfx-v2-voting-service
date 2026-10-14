// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package eventing

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	fgaconstants "github.com/linuxfoundation/lfx-v2-fga-sync/pkg/constants"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fgaMessage is the subset of an fga-sync message these tests assert on.
type fgaMessage struct {
	Subject    string
	ObjectType string                 `json:"object_type"`
	Operation  string                 `json:"operation"`
	Data       map[string]interface{} `json:"data"`
}

// newCapturingPublisher starts an embedded NATS server and returns a publisher plus a
// function that drains the fga-sync messages published so far.
func newCapturingPublisher(t *testing.T) (*NATSPublisher, func() []fgaMessage) {
	t.Helper()

	srv, err := server.NewServer(&server.Options{Port: -1})
	require.NoError(t, err)
	go srv.Start()
	require.True(t, srv.ReadyForConnections(5*time.Second))
	t.Cleanup(srv.Shutdown)

	conn, err := nats.Connect(srv.ClientURL())
	require.NoError(t, err)
	t.Cleanup(conn.Close)

	msgs := make(chan fgaMessage, 16)
	handler := func(m *nats.Msg) {
		var msg fgaMessage
		require.NoError(t, json.Unmarshal(m.Data, &msg))
		msg.Subject = m.Subject
		msgs <- msg
	}
	for _, subject := range []string{
		fgaconstants.GenericUpdateAccessSubject,
		fgaconstants.GenericDeleteAccessSubject,
		fgaconstants.GenericMemberPutSubject,
		fgaconstants.GenericMemberRemoveSubject,
	} {
		_, err := conn.Subscribe(subject, handler)
		require.NoError(t, err)
	}
	require.NoError(t, conn.Flush())

	drain := func() []fgaMessage {
		require.NoError(t, conn.Flush())
		time.Sleep(100 * time.Millisecond)
		var out []fgaMessage
		for {
			select {
			case m := <-msgs:
				out = append(out, m)
			default:
				return out
			}
		}
	}
	return NewNATSPublisher(conn, slog.Default()), drain
}

func TestSendVoteParticipantMessage(t *testing.T) {
	tests := []struct {
		name          string
		data          domain.VoteResponseData
		wantOperation string
		wantSubject   string
		wantNone      bool
	}{
		{
			name:          "invitee with username becomes participant of the vote",
			data:          domain.VoteResponseData{UID: "resp-1", VoteUID: "vote-1", Username: "nickcoai"},
			wantOperation: "member_put",
			wantSubject:   fgaconstants.GenericMemberPutSubject,
		},
		{
			name:          "removed voter loses participant",
			data:          domain.VoteResponseData{UID: "resp-1", VoteUID: "vote-1", Username: "nickcoai", VoterRemoved: true},
			wantOperation: "member_remove",
			wantSubject:   fgaconstants.GenericMemberRemoveSubject,
		},
		{
			name:     "no username is skipped (email-only invitee, same as the owner tuple)",
			data:     domain.VoteResponseData{UID: "resp-1", VoteUID: "vote-1"},
			wantNone: true,
		},
		{
			name:     "no vote uid is skipped",
			data:     domain.VoteResponseData{UID: "resp-1", Username: "nickcoai"},
			wantNone: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			publisher, drain := newCapturingPublisher(t)

			require.NoError(t, publisher.sendVoteParticipantMessage(context.Background(), &tt.data, tt.data.VoterRemoved))

			got := drain()
			if tt.wantNone {
				assert.Empty(t, got)
				return
			}
			require.Len(t, got, 1)
			assert.Equal(t, tt.wantSubject, got[0].Subject)
			assert.Equal(t, "vote", got[0].ObjectType)
			assert.Equal(t, tt.wantOperation, got[0].Operation)
			assert.Equal(t, "vote-1", got[0].Data["uid"])
			assert.Equal(t, "nickcoai", got[0].Data["username"])
			assert.Equal(t, []interface{}{"participant"}, got[0].Data["relations"])
		})
	}
}

func TestPublishVoteResponseEvent_MaintainsParticipant(t *testing.T) {
	data := &domain.VoteResponseData{UID: "resp-1", VoteUID: "vote-1", Username: "nickcoai"}

	for _, action := range []string{"created", "updated"} {
		t.Run(action+" writes owner on the response and participant on the vote", func(t *testing.T) {
			publisher, drain := newCapturingPublisher(t)
			require.NoError(t, publisher.PublishVoteResponseEvent(context.Background(), action, data))

			var subjects []string
			for _, m := range drain() {
				subjects = append(subjects, m.Subject)
			}
			assert.ElementsMatch(t, []string{fgaconstants.GenericUpdateAccessSubject, fgaconstants.GenericMemberPutSubject}, subjects)
		})
	}

	t.Run("deleted removes the response and, when the voter is known, the participant", func(t *testing.T) {
		publisher, drain := newCapturingPublisher(t)
		require.NoError(t, publisher.PublishVoteResponseEvent(context.Background(), "deleted", data))

		var subjects []string
		var remove fgaMessage
		for _, m := range drain() {
			subjects = append(subjects, m.Subject)
			if m.Subject == fgaconstants.GenericMemberRemoveSubject {
				remove = m
			}
		}
		assert.ElementsMatch(t, []string{fgaconstants.GenericDeleteAccessSubject, fgaconstants.GenericMemberRemoveSubject}, subjects)
		assert.Equal(t, "vote", remove.ObjectType)
		assert.Equal(t, "vote-1", remove.Data["uid"])
		assert.Equal(t, "nickcoai", remove.Data["username"])
	})

	t.Run("deleted without a known voter only deletes the response", func(t *testing.T) {
		publisher, drain := newCapturingPublisher(t)
		require.NoError(t, publisher.PublishVoteResponseEvent(context.Background(), "deleted", &domain.VoteResponseData{UID: "resp-1"}))

		got := drain()
		require.Len(t, got, 1)
		assert.Equal(t, fgaconstants.GenericDeleteAccessSubject, got[0].Subject)
	})
}

func TestSendVoteAccessMessage_PreservesParticipantTuples(t *testing.T) {
	publisher, drain := newCapturingPublisher(t)

	require.NoError(t, publisher.sendVoteAccessMessage(context.Background(), &domain.VoteData{
		VoteUID: "vote-1", ProjectUID: "proj-1", CommitteeUID: "comm-1",
	}))

	got := drain()
	require.Len(t, got, 1)
	assert.Equal(t, fgaconstants.GenericUpdateAccessSubject, got[0].Subject)
	// update_access is a full sync: without this, every vote update would delete the participants.
	assert.Equal(t, []interface{}{"participant"}, got[0].Data["exclude_relations"])
}
