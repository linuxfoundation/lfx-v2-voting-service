// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package proxy_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"testing"

	converters "github.com/linuxfoundation/lfx-v2-voting-service/cmd/voting-api/service"
	voteclient "github.com/linuxfoundation/lfx-v2-voting-service/gen/http/vote/client"
	voteserver "github.com/linuxfoundation/lfx-v2-voting-service/gen/http/vote/server"
	votesvc "github.com/linuxfoundation/lfx-v2-voting-service/gen/vote"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/infrastructure/proxy"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/service"
	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/constants"
	goahttp "goa.design/goa/v3/http"
)

// TestVoteResultsMethodContract drives stub ITX results JSON through the real proxy,
// VoteService.GetVoteResults, the results converter and the generated GET handler with the
// standard Goa encoder, then decodes the body with the generated client. The stub only
// answers the results request; it does not model ITX hold/unlock decisions.
func TestVoteResultsMethodContract(t *testing.T) {
	const uid = "a02bdbaf-53b1-4d47-bc04-dd7e459dd308"
	const voteID = "b02bdbaf-53b1-4d47-bc04-dd7e459dd308"
	const (
		prompt1   = "c02bdbaf-53b1-4d47-bc04-dd7e459dd308"
		question1 = "d02bdbaf-53b1-4d47-bc04-dd7e459dd308"
		question2 = "e02bdbaf-53b1-4d47-bc04-dd7e459dd308"
		choiceA   = "f02bdbaf-53b1-4d47-bc04-dd7e459dd308"
		choiceB   = "f12bdbaf-53b1-4d47-bc04-dd7e459dd308"
		choiceX   = "f22bdbaf-53b1-4d47-bc04-dd7e459dd308"
	)

	serve := func(t *testing.T, upstreamBody string) *http.Response {
		t.Helper()
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet || r.URL.Path != "/v2/voting/poll/"+uid+"/results" {
				t.Errorf("unexpected upstream request: %s %s", r.Method, r.URL.Path)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, upstreamBody)
		}))
		t.Cleanup(upstream.Close)
		client := proxy.NewContractTestClient(upstream.Client(), upstream.URL+"/")
		voting := service.NewVoteService(nil, client, methodContractIDMapper{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
		mux := goahttp.NewMuxer()
		get := func(ctx context.Context, payload any) (any, error) {
			ctx = context.WithValue(ctx, constants.PrincipalContextID, "synthetic-contract-user")
			results, err := voting.GetVoteResults(ctx, payload.(*votesvc.GetVoteResultsPayload).UID)
			if err != nil {
				return nil, err
			}
			return converters.ConvertVoteResultsToResult(results), nil
		}
		voteserver.MountGetVoteResultsHandler(mux, voteserver.NewGetVoteResultsHandler(get, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil))
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/votes/"+uid+"/results", nil))
		if response.Code != http.StatusOK {
			t.Fatalf("HTTP status: want 200, got %d: %s", response.Code, response.Body)
		}
		return response.Result()
	}
	decodeMap := func(t *testing.T, response *http.Response) map[string]json.RawMessage {
		t.Helper()
		var body map[string]json.RawMessage
		if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		return body
	}
	keys := func(m map[string]json.RawMessage) []string {
		out := make([]string, 0, len(m))
		for k := range m {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	}
	decodeClient := func(t *testing.T, upstreamBody string) *votesvc.VoteResultsResult {
		t.Helper()
		response := serve(t, upstreamBody)
		got, err := voteclient.DecodeGetVoteResultsResponse(goahttp.ResponseDecoder, false)(response)
		if err != nil {
			t.Fatalf("generated client rejected the response: %v", err)
		}
		return got.(*votesvc.VoteResultsResult)
	}

	t.Run("held response returns only the flag, participation and deadline", func(t *testing.T) {
		body := decodeMap(t, serve(t, `{"results_held":true,"num_recipients":10,"num_votes_cast":4,"poll_end_time":"2030-01-01T00:00:00Z","poll_end_time_timezone":"America/New_York"}`))
		want := []string{"num_recipients", "num_votes_cast", "poll_end_time", "poll_end_time_timezone", "results_held"}
		if got := keys(body); !reflect.DeepEqual(got, want) {
			t.Fatalf("held keys: want %v, got %v", want, got)
		}
		if string(body["results_held"]) != "true" {
			t.Errorf("results_held: %s", body["results_held"])
		}
		got := decodeClient(t, `{"results_held":true,"num_recipients":10,"num_votes_cast":4,"poll_end_time":"2030-01-01T00:00:00Z"}`)
		if got.ResultsHeld == nil || !*got.ResultsHeld || got.NumAbstained != nil || got.PollResults != nil || got.CommentResults != nil {
			t.Errorf("generated client lost the held shape: %+v", got)
		}
	})

	t.Run("held response without a timezone omits the timezone key", func(t *testing.T) {
		body := decodeMap(t, serve(t, `{"results_held":true,"num_recipients":3,"num_votes_cast":1,"poll_end_time":"2030-01-01T00:00:00Z"}`))
		if _, present := body["poll_end_time_timezone"]; present {
			t.Errorf("timezone must be absent: %s", body["poll_end_time_timezone"])
		}
	})

	t.Run("final anonymous comments are text-only, duplicated and ordered", func(t *testing.T) {
		upstream := `{"num_recipients":5,"num_votes_cast":3,"num_abstained":1,"poll_end_time":"2030-01-01T00:00:00Z","poll_results":[],"comment_results":[{"prompt":{"prompt_id":"` + prompt1 + `","prompt":"Any comments?"},"responses":[{"comment_text":"zeta"},{"comment_text":"alpha"},{"comment_text":"alpha"}]}]}`
		body := decodeMap(t, serve(t, upstream))
		if _, present := body["results_held"]; present {
			t.Errorf("missing results_held must stay absent: %s", body["results_held"])
		}
		if _, present := body["poll_results"]; present {
			t.Errorf("empty optional poll_results is omitted under existing Goa behavior: %s", body["poll_results"])
		}
		var comments []struct {
			Responses []map[string]json.RawMessage `json:"responses"`
		}
		if err := json.Unmarshal(body["comment_results"], &comments); err != nil {
			t.Fatalf("decode comment_results: %v", err)
		}
		responses := comments[0].Responses
		if len(responses) != 3 {
			t.Fatalf("want 3 responses, got %d", len(responses))
		}
		for i, text := range []string{`"zeta"`, `"alpha"`, `"alpha"`} {
			if got := keys(responses[i]); !reflect.DeepEqual(got, []string{"comment_text"}) {
				t.Errorf("response %d keys: want [comment_text], got %v", i, got)
			}
			if string(responses[i]["comment_text"]) != text {
				t.Errorf("response %d text: want %s, got %s", i, text, responses[i]["comment_text"])
			}
		}
		got := decodeClient(t, upstream)
		if got.NumAbstained == nil || *got.NumAbstained != 1 {
			t.Errorf("num_abstained lost: %+v", got.NumAbstained)
		}
		for i, r := range got.CommentResults[0].Responses {
			if r.VoteID != nil || r.VoteCreationTime != nil || r.Abstained != nil {
				t.Errorf("generated client accepted but invented metadata for response %d: %+v", i, r)
			}
		}
	})

	t.Run("explicit false and zero survive the encoder", func(t *testing.T) {
		body := decodeMap(t, serve(t, `{"results_held":false,"num_recipients":2,"num_votes_cast":2,"num_abstained":0,"poll_end_time":"2030-01-01T00:00:00Z"}`))
		if string(body["results_held"]) != "false" || string(body["num_abstained"]) != "0" {
			t.Errorf("results_held=%s num_abstained=%s", body["results_held"], body["num_abstained"])
		}
	})

	t.Run("named results keep totals, ids, times, zero counts and both abstained values", func(t *testing.T) {
		upstream := `{"num_recipients":4,"num_votes_cast":2,"num_abstained":0,"poll_end_time":"2030-01-01T00:00:00Z",` +
			`"poll_results":[` +
			`{"question":{"question_id":"` + question1 + `","prompt":"Pick","type":"multiple_choice"},"generic_choice_votes":[{"choice_id":"` + choiceB + `","vote_count":0,"percentage":0},{"choice_id":"` + choiceA + `","vote_count":2,"percentage":100}]},` +
			`{"question":{"question_id":"` + question2 + `","prompt":"Rank","type":"multiple_choice"},"ranked_choice_votes":[{"choice_id":"` + choiceX + `","rank_counts":[{"rank":1,"count":2}]}]}],` +
			`"comment_results":[{"prompt":{"prompt_id":"` + prompt1 + `","prompt":"Why?"},"responses":[` +
			`{"vote_id":"` + voteID + `","user_id":"u1","comment_text":"a","vote_creation_time":"2030-01-01T00:00:00Z","abstained":false},` +
			`{"vote_id":"` + uid + `","comment_text":"b","vote_creation_time":"2030-01-02T00:00:00Z","abstained":true}]}]}`
		got := decodeClient(t, upstream)
		if got.NumAbstained == nil || *got.NumAbstained != 0 {
			t.Errorf("num_abstained: %+v", got.NumAbstained)
		}
		if len(got.PollResults) != 2 {
			t.Fatalf("poll_results: %+v", got.PollResults)
		}
		generic := got.PollResults[0].GenericChoiceVotes
		if len(generic) != 2 || generic[0].ChoiceID != choiceB || generic[0].VoteCount != 0 || generic[1].ChoiceID != choiceA || generic[1].VoteCount != 2 {
			t.Errorf("generic totals or order changed: %+v", generic)
		}
		if ranked := got.PollResults[1].RankedChoiceVotes; len(ranked) != 1 || ranked[0].RankCounts[0].Count != 2 {
			t.Errorf("ranked totals changed: %+v", ranked)
		}
		responses := got.CommentResults[0].Responses
		if len(responses) != 2 || responses[0].VoteID == nil || *responses[0].VoteID != voteID || responses[0].VoteCreationTime == nil {
			t.Fatalf("named metadata lost: %+v", responses)
		}
		if responses[0].Abstained == nil || *responses[0].Abstained || responses[1].Abstained == nil || !*responses[1].Abstained {
			t.Errorf("abstained values changed: %+v %+v", responses[0].Abstained, responses[1].Abstained)
		}
	})

	t.Run("a prompt with no responses keeps a required empty array", func(t *testing.T) {
		body := decodeMap(t, serve(t, `{"num_recipients":1,"num_votes_cast":0,"num_abstained":0,"poll_end_time":"2030-01-01T00:00:00Z","comment_results":[{"prompt":{"prompt_id":"c1","prompt":"Why?"},"responses":[]}]}`))
		var comments []map[string]json.RawMessage
		if err := json.Unmarshal(body["comment_results"], &comments); err != nil {
			t.Fatalf("decode comment_results: %v", err)
		}
		if string(comments[0]["responses"]) != "[]" {
			t.Errorf("responses: want [], got %s", comments[0]["responses"])
		}
	})

	t.Run("supplied values are still validated by the generated client", func(t *testing.T) {
		response := serve(t, `{"num_recipients":1,"num_votes_cast":1,"poll_end_time":"2030-01-01T00:00:00Z","comment_results":[{"prompt":{"prompt_id":"c1","prompt":"Why?"},"responses":[{"vote_id":"not-a-uuid","comment_text":"x"}]}]}`)
		if _, err := voteclient.DecodeGetVoteResultsResponse(goahttp.ResponseDecoder, false)(response); err == nil {
			t.Error("a supplied non-UUID vote_id must still fail client validation")
		}
	})
}
