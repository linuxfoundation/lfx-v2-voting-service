// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package eventstate

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

var errInvalidSourceResponse = errors.New("invalid source stream response")

// ReadSourceMessage uses leader STREAM.MSG.GET, never follower direct reads.
// nextSubject scans sparse sequences; lastSubject reads one current source key.
func ReadSourceMessage(ctx context.Context, js jetstream.JetStream, stream string, seq uint64, nextSubject, lastSubject string) (*jetstream.RawStreamMsg, error) {
	request, err := json.Marshal(struct {
		Sequence    uint64 `json:"seq,omitempty"`
		NextSubject string `json:"next_by_subj,omitempty"`
		LastSubject string `json:"last_by_subj,omitempty"`
	}{seq, nextSubject, lastSubject})
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	prefix := js.Options().APIPrefix
	if prefix == "" {
		prefix = jetstream.DefaultAPIPrefix
		if js.Options().Domain != "" {
			prefix = "$JS." + js.Options().Domain + ".API."
		}
	}
	response, err := js.Conn().RequestWithContext(ctx, strings.TrimSuffix(prefix, ".")+".STREAM.MSG.GET."+stream, request)
	if err != nil {
		return nil, err
	}
	var result struct {
		Error   *jetstream.APIError `json:"error"`
		Message *struct {
			Subject  string    `json:"subject"`
			Sequence uint64    `json:"seq"`
			Data     []byte    `json:"data"`
			Headers  []byte    `json:"hdrs"`
			Time     time.Time `json:"time"`
		} `json:"message"`
	}
	if err := json.Unmarshal(response.Data, &result); err != nil {
		return nil, errInvalidSourceResponse
	}
	if result.Error != nil {
		if result.Error.ErrorCode == jetstream.JSErrCodeMessageNotFound {
			return nil, jetstream.ErrMsgNotFound
		}
		return nil, result.Error
	}
	if result.Message == nil {
		return nil, errInvalidSourceResponse
	}
	var headers nats.Header
	if len(result.Message.Headers) > 0 {
		headers, err = nats.DecodeHeadersMsg(result.Message.Headers)
		if err != nil {
			return nil, errInvalidSourceResponse
		}
	}
	return &jetstream.RawStreamMsg{Subject: result.Message.Subject, Sequence: result.Message.Sequence, Data: result.Message.Data, Header: headers, Time: result.Message.Time}, nil
}
