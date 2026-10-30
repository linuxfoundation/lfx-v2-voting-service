// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package itx

import (
	"encoding/json"
	"testing"
)

func TestPollResponseUnmarshal_PseudoAnonymityPresence(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		wantValue bool
		wantKnown bool
	}{
		{name: "true", body: `{"poll_id":"p1","pseudo_anonymity":true}`, wantValue: true, wantKnown: true},
		{name: "false", body: `{"poll_id":"p1","pseudo_anonymity":false}`, wantValue: false, wantKnown: true},
		{name: "missing", body: `{"poll_id":"p1"}`, wantValue: false, wantKnown: false},
		{name: "null", body: `{"poll_id":"p1","pseudo_anonymity":null}`, wantValue: false, wantKnown: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var p PollResponse
			if err := json.Unmarshal([]byte(tt.body), &p); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if p.PollID != "p1" {
				t.Errorf("other fields not decoded: %+v", p)
			}
			if p.PseudoAnonymity != tt.wantValue || p.PseudoAnonymityKnown != tt.wantKnown {
				t.Errorf("PseudoAnonymity=%v Known=%v, want %v/%v", p.PseudoAnonymity, p.PseudoAnonymityKnown, tt.wantValue, tt.wantKnown)
			}
		})
	}

	t.Run("a non-bool value is still a decode error", func(t *testing.T) {
		var p PollResponse
		if err := json.Unmarshal([]byte(`{"pseudo_anonymity":"yes"}`), &p); err == nil {
			t.Error("expected an error, as before")
		}
	})
}

func TestIsSubmittedVoteStatus(t *testing.T) {
	for status, want := range map[string]bool{
		"responded":                        true,
		"ended":                            true,
		"awaiting_response":                false,
		"awaiting_response_but_poll_ended": false,
		"":                                 false,
	} {
		if got := IsSubmittedVoteStatus(status); got != want {
			t.Errorf("IsSubmittedVoteStatus(%q) = %v, want %v", status, got, want)
		}
	}
}

func TestVoteResultsUnmarshal_Presence(t *testing.T) {
	boolPtr := func(b bool) *bool { return &b }
	intPtr := func(i int) *int { return &i }
	tests := []struct {
		name         string
		body         string
		wantHeld     *bool
		wantAbstain  *int
		wantVotes    int
		wantPolls    int
		wantComments int
	}{
		{name: "held true", body: `{"results_held":true,"num_recipients":10,"num_votes_cast":4,"poll_end_time":"2030-01-01T00:00:00Z"}`, wantHeld: boolPtr(true), wantVotes: 4},
		{name: "held false", body: `{"results_held":false,"num_recipients":10,"num_votes_cast":4,"num_abstained":1}`, wantHeld: boolPtr(false), wantAbstain: intPtr(1), wantVotes: 4},
		{name: "held missing", body: `{"num_recipients":10,"num_votes_cast":4,"num_abstained":1}`, wantAbstain: intPtr(1), wantVotes: 4},
		{name: "held null", body: `{"results_held":null,"num_recipients":10,"num_votes_cast":4}`, wantVotes: 4},
		{name: "abstained null", body: `{"num_abstained":null,"num_votes_cast":2}`, wantVotes: 2},
		{name: "abstained zero", body: `{"num_abstained":0,"num_votes_cast":2}`, wantAbstain: intPtr(0), wantVotes: 2},
		{name: "abstained positive", body: `{"num_abstained":3,"num_votes_cast":2}`, wantAbstain: intPtr(3), wantVotes: 2},
		{
			name:         "populated arrays",
			body:         `{"poll_results":[{"question":{"question_id":"q1"}}],"comment_results":[{"prompt":{"prompt_id":"c1"},"responses":[]}],"num_abstained":0}`,
			wantAbstain:  intPtr(0),
			wantPolls:    1,
			wantComments: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var r VoteResults
			if err := json.Unmarshal([]byte(tt.body), &r); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			assertPtr(t, "ResultsHeld", r.ResultsHeld, tt.wantHeld)
			assertPtr(t, "NumAbstained", r.NumAbstained, tt.wantAbstain)
			if r.NumVotesCast != tt.wantVotes {
				t.Errorf("NumVotesCast = %d, want %d", r.NumVotesCast, tt.wantVotes)
			}
			if len(r.PollResults) != tt.wantPolls || len(r.CommentResults) != tt.wantComments {
				t.Errorf("poll/comment results = %d/%d, want %d/%d", len(r.PollResults), len(r.CommentResults), tt.wantPolls, tt.wantComments)
			}
		})
	}

	t.Run("anonymous comments carry text only", func(t *testing.T) {
		var r VoteResults
		body := `{"comment_results":[{"prompt":{"prompt_id":"c1"},"responses":[{"comment_text":"b"},{"comment_text":"b"}]}]}`
		if err := json.Unmarshal([]byte(body), &r); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		for i, resp := range r.CommentResults[0].Responses {
			if resp.CommentText != "b" || resp.VoteID != nil || resp.VoteCreationTime != nil || resp.Abstained != nil {
				t.Errorf("response %d has unexpected metadata: %+v", i, resp)
			}
		}
	})

	t.Run("named comments keep ids, times and both abstained values", func(t *testing.T) {
		var r VoteResults
		body := `{"comment_results":[{"prompt":{"prompt_id":"c1"},"responses":[` +
			`{"vote_id":"v1","user_id":"u1","comment_text":"a","vote_creation_time":"2030-01-01T00:00:00Z","abstained":false},` +
			`{"vote_id":"v2","comment_text":"b","vote_creation_time":"2030-01-02T00:00:00Z","abstained":true}]}]}`
		if err := json.Unmarshal([]byte(body), &r); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		got := r.CommentResults[0].Responses
		if got[0].VoteID == nil || *got[0].VoteID != "v1" || got[0].VoteCreationTime == nil || *got[0].VoteCreationTime != "2030-01-01T00:00:00Z" {
			t.Errorf("first response lost metadata: %+v", got[0])
		}
		if got[0].Abstained == nil || *got[0].Abstained {
			t.Errorf("explicit abstained=false must stay present and false: %+v", got[0])
		}
		if got[1].Abstained == nil || !*got[1].Abstained {
			t.Errorf("explicit abstained=true must stay present and true: %+v", got[1])
		}
	})

	for name, body := range map[string]string{
		"string held":         `{"results_held":"yes"}`,
		"string abstained":    `{"num_abstained":"3"}`,
		"bool abstained":      `{"num_abstained":true}`,
		"int comment vote":    `{"comment_results":[{"responses":[{"vote_id":5,"comment_text":"x"}]}]}`,
		"string comment bool": `{"comment_results":[{"responses":[{"abstained":"no","comment_text":"x"}]}]}`,
	} {
		t.Run("wrong type is a decode error: "+name, func(t *testing.T) {
			var r VoteResults
			if err := json.Unmarshal([]byte(body), &r); err == nil {
				t.Error("expected a decode error")
			}
		})
	}
}

func assertPtr[T comparable](t *testing.T, field string, got, want *T) {
	t.Helper()
	switch {
	case got == nil && want == nil:
	case got == nil || want == nil:
		t.Errorf("%s = %v, want %v", field, got, want)
	case *got != *want:
		t.Errorf("%s = %v, want %v", field, *got, *want)
	}
}
