// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package domain

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
