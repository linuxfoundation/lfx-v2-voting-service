// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package domain

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/models/itx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestCoerceToInt(t *testing.T) {
	cases := []struct {
		name    string
		in      interface{}
		want    int
		wantErr string
	}{
		{name: "numeric string", in: "42", want: 42},
		{name: "negative string", in: "-7", want: -7},
		{name: "empty string is zero", in: "", want: 0},
		{name: "float64 from JSON", in: float64(12), want: 12},
		{name: "float64 truncates", in: 12.9, want: 12},
		{name: "int", in: 5, want: 5},
		{name: "nil is zero", in: nil, want: 0},
		{name: "non-numeric string", in: "abc", wantErr: "invalid string value for count"},
		{name: "decimal string is not an int", in: "1.5", wantErr: "invalid string value for count"},
		{name: "bool is rejected", in: true, wantErr: "invalid type for count: bool"},
		{name: "slice is rejected", in: []int{1}, wantErr: "invalid type for count"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := CoerceToInt(tc.in, "count")

			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				assert.Zero(t, got)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestCoerceToFloat64(t *testing.T) {
	cases := []struct {
		name    string
		in      interface{}
		want    float64
		wantErr string
	}{
		{name: "decimal string", in: "33.33", want: 33.33},
		{name: "integer string", in: "50", want: 50},
		{name: "empty string is zero", in: "", want: 0},
		{name: "float64", in: 12.5, want: 12.5},
		{name: "int", in: 3, want: 3},
		{name: "nil is zero", in: nil, want: 0},
		{name: "non-numeric string", in: "n/a", wantErr: "invalid string value for pct"},
		{name: "bool is rejected", in: false, wantErr: "invalid type for pct: bool"},
		{name: "map is rejected", in: map[string]int{}, wantErr: "invalid type for pct"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := CoerceToFloat64(tc.in, "pct")

			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				assert.Zero(t, got)
				return
			}
			require.NoError(t, err)
			assert.InDelta(t, tc.want, got, 1e-9)
		})
	}
}

func TestPollDBRaw_UnmarshalJSON(t *testing.T) {
	t.Run("maps every scalar field", func(t *testing.T) {
		in := `{
			"poll_id": "poll-1", "name": "Board Vote", "description": "desc",
			"creation_time": "2026-01-01T00:00:00Z", "last_modified_time": "2026-01-02T00:00:00Z",
			"end_time": "2026-02-01T00:00:00Z", "end_time_timezone": "America/New_York",
			"early_end_time": "2026-01-20T00:00:00Z", "status": "active",
			"project_id": "proj-1", "project_name": "Test Project",
			"committee_id": "comm-1", "committee_name": "Test Committee", "committee_type": "board",
			"committee_voting_status": true, "committee_filters": ["Voting Rep"],
			"total_voting_request_invitations": 10, "num_response_received": 4, "num_winners": 2,
			"poll_type": "ranked", "pseudo_anonymity": true, "allow_abstain": true
		}`

		var p PollDBRaw
		require.NoError(t, json.Unmarshal([]byte(in), &p))

		assert.Equal(t, "poll-1", p.PollID)
		assert.Equal(t, "Board Vote", p.Name)
		assert.Equal(t, "desc", p.Description)
		assert.Equal(t, "2026-01-01T00:00:00Z", p.CreationTime)
		assert.Equal(t, "2026-01-02T00:00:00Z", p.LastModifiedTime)
		assert.Equal(t, "2026-02-01T00:00:00Z", p.EndTime)
		assert.Equal(t, "America/New_York", p.EndTimeTimezone)
		assert.Equal(t, "2026-01-20T00:00:00Z", p.EarlyEndTime)
		assert.Equal(t, "active", p.Status)
		assert.Equal(t, "proj-1", p.ProjectID)
		assert.Equal(t, "Test Project", p.ProjectName)
		assert.Equal(t, "comm-1", p.CommitteeID)
		assert.Equal(t, "Test Committee", p.CommitteeName)
		assert.Equal(t, "board", p.CommitteeType)
		assert.True(t, p.CommitteeVotingStatus)
		assert.Equal(t, []string{"Voting Rep"}, p.CommitteeFilters)
		assert.Equal(t, 10, p.TotalVotingRequestInvitations)
		assert.Equal(t, 4, p.NumResponseReceived)
		assert.Equal(t, 2, p.NumWinners)
		assert.Equal(t, "ranked", p.PollType)
		assert.True(t, p.PseudoAnonymity)
		assert.True(t, p.AllowAbstain)
	})

	t.Run("numeric fields accept strings from Meltano", func(t *testing.T) {
		in := `{"total_voting_request_invitations": "25", "num_response_received": "12", "num_winners": "3"}`

		var p PollDBRaw
		require.NoError(t, json.Unmarshal([]byte(in), &p))

		assert.Equal(t, 25, p.TotalVotingRequestInvitations)
		assert.Equal(t, 12, p.NumResponseReceived)
		assert.Equal(t, 3, p.NumWinners)
	})

	t.Run("empty-string and null numerics become zero", func(t *testing.T) {
		in := `{"total_voting_request_invitations": "", "num_response_received": null}`

		var p PollDBRaw
		require.NoError(t, json.Unmarshal([]byte(in), &p))

		assert.Zero(t, p.TotalVotingRequestInvitations)
		assert.Zero(t, p.NumResponseReceived)
		assert.Zero(t, p.NumWinners, "absent field stays zero")
	})

	t.Run("invalid numerics are rejected", func(t *testing.T) {
		cases := map[string]string{
			"non-numeric invitations string": `{"total_voting_request_invitations": "lots"}`,
			"non-numeric responses string":   `{"num_response_received": "x"}`,
			"non-numeric winners string":     `{"num_winners": "two"}`,
			"bool invitations":               `{"total_voting_request_invitations": true}`,
			"object responses":               `{"num_response_received": {}}`,
			"array winners":                  `{"num_winners": [1]}`,
		}
		for name, in := range cases {
			t.Run(name, func(t *testing.T) {
				var p PollDBRaw
				assert.Error(t, json.Unmarshal([]byte(in), &p))
			})
		}
	})

	t.Run("malformed JSON is rejected", func(t *testing.T) {
		var p PollDBRaw
		assert.Error(t, p.UnmarshalJSON([]byte(`{"poll_id":`)))
	})
}

func TestRankedChoiceAnswerRaw_UnmarshalJSON(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		wantRank int
		wantErr  bool
	}{
		{name: "string rank", in: `{"choice_id":"c1","choice_text":"A","choice_rank":"2"}`, wantRank: 2},
		{name: "numeric rank", in: `{"choice_id":"c1","choice_text":"A","choice_rank":3}`, wantRank: 3},
		{name: "empty string rank", in: `{"choice_id":"c1","choice_text":"A","choice_rank":""}`, wantRank: 0},
		{name: "null rank", in: `{"choice_id":"c1","choice_text":"A","choice_rank":null}`, wantRank: 0},
		{name: "absent rank", in: `{"choice_id":"c1","choice_text":"A"}`, wantRank: 0},
		{name: "non-numeric string rank", in: `{"choice_rank":"first"}`, wantErr: true},
		{name: "bool rank", in: `{"choice_rank":true}`, wantErr: true},
		{name: "malformed JSON", in: `{"choice_id":`, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var r RankedChoiceAnswerRaw
			err := json.Unmarshal([]byte(tc.in), &r)

			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "c1", r.ChoiceID)
			assert.Equal(t, "A", r.ChoiceText)
			assert.Equal(t, tc.wantRank, r.ChoiceRank)
		})
	}

	t.Run("works when nested in a poll answer", func(t *testing.T) {
		in := `{"question_id":"q1","prompt":"Rank","type":"ranked_choice",
			"ranked_user_choice":[{"choice_id":"a","choice_rank":"1"},{"choice_id":"b","choice_rank":2}]}`

		var ans PollAnswerRaw
		require.NoError(t, json.Unmarshal([]byte(in), &ans))

		require.Len(t, ans.RankedUserChoice, 2)
		assert.Equal(t, 1, ans.RankedUserChoice[0].ChoiceRank)
		assert.Equal(t, 2, ans.RankedUserChoice[1].ChoiceRank)
	})
}

func TestPollResultDBRaw_UnmarshalJSON(t *testing.T) {
	t.Run("maps scalar fields and coerces string counts", func(t *testing.T) {
		in := `{
			"poll_id": "poll-1", "committee_id": "comm-1", "project_id": "proj-1", "status": "ended",
			"num_recipients": "20", "num_votes_cast": 15.0, "num_abstained": "2",
			"poll_end_time": "2026-02-01T00:00:00Z", "created_time": "2026-01-01T00:00:00Z",
			"last_modified_time": "2026-02-02T00:00:00Z",
			"poll_questions_result": [{"question_id":"q1","prompt":"Pick","choice_results":[
				{"choice_id":"c1","choice_text":"A","vote_count":"9","percentage":"60.0"},
				{"choice_id":"c2","choice_text":"B","vote_count":6,"percentage":40}
			]}]
		}`

		var p PollResultDBRaw
		require.NoError(t, json.Unmarshal([]byte(in), &p))

		assert.Equal(t, "poll-1", p.PollID)
		assert.Equal(t, "comm-1", p.CommitteeID)
		assert.Equal(t, "proj-1", p.ProjectID)
		assert.Equal(t, "ended", p.Status)
		assert.Equal(t, 20, p.NumRecipients)
		assert.Equal(t, 15, p.NumVotesCast)
		assert.Equal(t, 2, p.NumAbstained)
		assert.Equal(t, "2026-02-01T00:00:00Z", p.PollEndTime)
		assert.Equal(t, "2026-01-01T00:00:00Z", p.CreatedTime)
		assert.Equal(t, "2026-02-02T00:00:00Z", p.LastModifiedTime)

		require.Len(t, p.PollQuestionsResult, 1)
		q := p.PollQuestionsResult[0]
		assert.Equal(t, "q1", q.QuestionID)
		assert.Equal(t, "Pick", q.Prompt)
		require.Len(t, q.ChoiceResults, 2)
		assert.Equal(t, 9, q.ChoiceResults[0].VoteCount)
		assert.InDelta(t, 60.0, q.ChoiceResults[0].Percentage, 1e-9)
		assert.Equal(t, 6, q.ChoiceResults[1].VoteCount)
		assert.InDelta(t, 40.0, q.ChoiceResults[1].Percentage, 1e-9)
	})

	t.Run("invalid count fields are rejected", func(t *testing.T) {
		cases := map[string]string{
			"num_recipients": `{"num_recipients": "many"}`,
			"num_votes_cast": `{"num_votes_cast": true}`,
			"num_abstained":  `{"num_abstained": "none"}`,
		}
		for name, in := range cases {
			t.Run(name, func(t *testing.T) {
				var p PollResultDBRaw
				err := json.Unmarshal([]byte(in), &p)
				require.Error(t, err)
				assert.Contains(t, err.Error(), name)
			})
		}
	})

	t.Run("malformed JSON is rejected", func(t *testing.T) {
		var p PollResultDBRaw
		assert.Error(t, p.UnmarshalJSON([]byte(`not json`)))
	})
}

func TestChoiceResultRaw_UnmarshalJSON(t *testing.T) {
	t.Run("coerces string and numeric values", func(t *testing.T) {
		var c ChoiceResultRaw
		require.NoError(t, json.Unmarshal([]byte(`{"choice_id":"c1","choice_text":"A","vote_count":"4","percentage":"25.5"}`), &c))

		assert.Equal(t, "c1", c.ChoiceID)
		assert.Equal(t, "A", c.ChoiceText)
		assert.Equal(t, 4, c.VoteCount)
		assert.InDelta(t, 25.5, c.Percentage, 1e-9)
	})

	t.Run("missing counts default to zero", func(t *testing.T) {
		var c ChoiceResultRaw
		require.NoError(t, json.Unmarshal([]byte(`{"choice_id":"c1"}`), &c))

		assert.Zero(t, c.VoteCount)
		assert.Zero(t, c.Percentage)
	})

	t.Run("invalid vote_count is rejected", func(t *testing.T) {
		var c ChoiceResultRaw
		err := json.Unmarshal([]byte(`{"vote_count":"x"}`), &c)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "vote_count")
	})

	t.Run("invalid percentage is rejected", func(t *testing.T) {
		var c ChoiceResultRaw
		err := json.Unmarshal([]byte(`{"vote_count":1,"percentage":"x"}`), &c)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "percentage")
	})
}
