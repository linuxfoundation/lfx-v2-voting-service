// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package domain

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/models/itx"
)

func TestPollRecordIsPseudoAnonymous(t *testing.T) {
	tests := []struct {
		name   string
		record map[string]any
		want   bool
	}{
		{name: "nil map", record: nil, want: true},
		{name: "missing key", record: map[string]any{"poll_id": "p1"}, want: true},
		{name: "null value", record: map[string]any{"pseudo_anonymity": nil}, want: true},
		{name: "bool true", record: map[string]any{"pseudo_anonymity": true}, want: true},
		{name: "bool false", record: map[string]any{"pseudo_anonymity": false}, want: false},
		{name: "string true", record: map[string]any{"pseudo_anonymity": "true"}, want: true},
		{name: "string false padded upper case", record: map[string]any{"pseudo_anonymity": " FALSE "}, want: false},
		{name: "unrecognised string", record: map[string]any{"pseudo_anonymity": "yes"}, want: true},
		{name: "number zero", record: map[string]any{"pseudo_anonymity": 0}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PollRecordIsPseudoAnonymous(tt.record); got != tt.want {
				t.Errorf("PollRecordIsPseudoAnonymous() = %v, want %v", got, tt.want)
			}
		})
	}
}

func sampleVoteResponseData() VoteResponseData {
	return VoteResponseData{
		UID:              "vr-1",
		VoteID:           "vr-1",
		VoteUID:          "poll-1",
		PollID:           "poll-1",
		ProjectID:        "sfid-1",
		ProjectUID:       "proj-uid-1",
		VoteCreationTime: "2026-01-01T00:00:00Z",
		LastModifiedTime: "2026-01-02T00:00:00Z",
		UserID:           "user-1",
		UserEmail:        "voter@example.com",
		UserName:         "Voter One",
		Username:         "voter1",
		UserVotingStatus: "voting_rep",
		PollAnswers: []itx.PollAnswer{{
			QuestionID: "q1",
			Prompt:     "Pick one",
			Type:       "single_choice",
			UserChoice: []itx.PollChoiceAnswer{{ChoiceID: "c1"}},
		}},
		CommentResponses: []itx.CommentResponse{{PromptID: "cp1", CommentText: "secret"}},
		VoteStatus:       "responded",
		Abstained:        true,
	}
}

func TestVoteResponseData_RedactBallot(t *testing.T) {
	d := sampleVoteResponseData()
	d.RedactBallot()

	if d.PollAnswers != nil || d.CommentResponses != nil || d.Abstained {
		t.Errorf("RedactBallot() left ballot fields: answers=%v comments=%v abstained=%v", d.PollAnswers, d.CommentResponses, d.Abstained)
	}
	if !d.BallotRedacted {
		t.Error("RedactBallot() did not set BallotRedacted")
	}
	if d.Username != "voter1" || d.VoteStatus != "responded" {
		t.Errorf("RedactBallot() changed participation fields: username=%q vote_status=%q", d.Username, d.VoteStatus)
	}
}

func TestVoteResponseData_MarshalJSON_Redacted(t *testing.T) {
	d := sampleVoteResponseData()
	d.RedactBallot()

	// Mirror the publisher: a *VoteResponseData held in an `any` envelope field.
	envelope := struct {
		Data any `json:"data"`
	}{Data: &d}
	raw, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var decoded struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	for _, key := range []string{"poll_answers", "comment_responses", "abstained", "BallotRedacted"} {
		if _, ok := decoded.Data[key]; ok {
			t.Errorf("redacted payload contains %q: %s", key, raw)
		}
	}
	for _, key := range []string{"vote_status", "vote_creation_time", "last_modified_time", "username", "user_email", "user_id", "user_voting_status"} {
		if _, ok := decoded.Data[key]; !ok {
			t.Errorf("redacted payload missing %q: %s", key, raw)
		}
	}
	if strings.Contains(string(raw), "secret") {
		t.Errorf("redacted payload leaks comment text: %s", raw)
	}
}

// namedVoteResponseReference freezes the vote_response wire tags as they were
// before redaction existed. It has no MarshalJSON, so it encodes with the
// default struct encoder.
type namedVoteResponseReference struct {
	UID                     string                `json:"uid"`
	VoteID                  string                `json:"vote_id"`
	VoteUID                 string                `json:"vote_uid"`
	PollID                  string                `json:"poll_id"`
	ProjectID               string                `json:"project_id"`
	ProjectUID              string                `json:"project_uid"`
	VoteCreationTime        string                `json:"vote_creation_time"`
	LastModifiedTime        string                `json:"last_modified_time"`
	UserID                  string                `json:"user_id"`
	UserEmail               string                `json:"user_email"`
	UserRole                string                `json:"user_role"`
	UserName                string                `json:"user_name"`
	Username                string                `json:"username"`
	ProfilePicture          string                `json:"profile_picture"`
	UserVotingStatus        string                `json:"user_voting_status"`
	UserOrgID               string                `json:"user_org_id"`
	UserOrgName             string                `json:"user_org_name"`
	PollAnswers             []itx.PollAnswer      `json:"poll_answers"`
	CommentResponses        []itx.CommentResponse `json:"comment_responses"`
	VoteStatus              string                `json:"vote_status"`
	Abstained               bool                  `json:"abstained"`
	VoterRemoved            bool                  `json:"voter_removed"`
	SESMessageID            string                `json:"ses_message_id"`
	SESMessageLastSentTime  string                `json:"ses_message_last_sent_time"`
	SESBounceType           string                `json:"ses_bounce_type"`
	SESBounceSubtype        string                `json:"ses_bounce_subtype"`
	SESDeliverySuccessful   bool                  `json:"ses_delivery_successful"`
	SESComplaintExists      bool                  `json:"ses_complaint_exists"`
	SESComplaintType        string                `json:"ses_complaint_type"`
	SESComplaintDate        string                `json:"ses_complaint_date"`
	SESEmailOpened          bool                  `json:"ses_email_opened"`
	SESEmailOpenedFirstTime string                `json:"ses_email_opened_first_time"`
	SESEmailOpenedLastTime  string                `json:"ses_email_opened_last_time"`
	SESLinkClicked          bool                  `json:"ses_link_clicked"`
	SESLinkClickedFirstTime string                `json:"ses_link_clicked_first_time"`
	SESLinkClickedLastTime  string                `json:"ses_link_clicked_last_time"`
	BallotRedacted          bool                  `json:"-"`
}

func TestVoteResponseData_MarshalJSON_NamedUnchanged(t *testing.T) {
	full := sampleVoteResponseData()
	empty := sampleVoteResponseData()
	empty.PollAnswers = []itx.PollAnswer{}
	empty.CommentResponses = []itx.CommentResponse{}
	empty.Abstained = false

	for name, d := range map[string]VoteResponseData{"with answers": full, "empty answers": empty} {
		t.Run(name, func(t *testing.T) {
			got, err := json.Marshal(&d)
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}
			want, err := json.Marshal(namedVoteResponseReference(d))
			if err != nil {
				t.Fatalf("json.Marshal(reference) error = %v", err)
			}
			if string(got) != string(want) {
				t.Errorf("named encoding changed\n got: %s\nwant: %s", got, want)
			}
		})
	}

	got, err := json.Marshal(empty)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	for _, fragment := range []string{`"abstained":false`, `"poll_answers":[]`, `"comment_responses":[]`} {
		if !strings.Contains(string(got), fragment) {
			t.Errorf("named encoding missing %s: %s", fragment, got)
		}
	}
}
