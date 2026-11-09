// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package eventsync

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// logRecords decodes the JSON log lines written to buf.
func logRecords(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var records []map[string]any
	for _, line := range bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var rec map[string]any
		require.NoError(t, json.Unmarshal(line, &rec))
		records = append(records, rec)
	}
	return records
}

// TestSync_VoteResponseUpdate_LogsCarryParentVote pins that every log line of a vote
// response update after the UID check carries the parent vote's poll_id, so operators
// can search logs by parent vote.
func TestSync_VoteResponseUpdate_LogsCarryParentVote(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	h := newHarness(t, func(d *Deps) { d.Logger = logger })
	withParentVote(h, "poll-456")
	h.pub.publishVoteResponseErr = errors.New("indexer rejected the message")

	assert.Equal(t, Ack, h.sync(putJSON(t, "itx-poll-vote.vote-123", responsePayload("vote-123", "poll-456"))))

	var sawPublishFailure bool
	for _, rec := range logRecords(t, &buf) {
		if rec["vote_response_id"] == nil {
			continue // logged before the UID was known
		}
		assert.Equal(t, "poll-456", rec["poll_id"], "log line %q lost poll_id", rec["msg"])
		if rec["msg"] == "failed to publish vote response event" {
			sawPublishFailure = true
		}
	}
	assert.True(t, sawPublishFailure, "expected the publish failure to be logged")
}
