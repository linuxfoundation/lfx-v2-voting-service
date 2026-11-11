// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package proxy_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	converters "github.com/linuxfoundation/lfx-v2-voting-service/cmd/voting-api/service"
	voteclient "github.com/linuxfoundation/lfx-v2-voting-service/gen/http/vote/client"
	voteserver "github.com/linuxfoundation/lfx-v2-voting-service/gen/http/vote/server"
	votesvc "github.com/linuxfoundation/lfx-v2-voting-service/gen/vote"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/infrastructure/proxy"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/service"
	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/constants"
	goahttp "goa.design/goa/v3/http"
)

// TestVoteMethodContract protects omission and explicit intent across the generated
// transports, converters, service and actual proxy HTTP serialization. The sink
// captures requests only; it does not model ITX persistence or preservation.
func TestVoteMethodContract(t *testing.T) {
	const uid = "a02bdbaf-53b1-4d47-bc04-dd7e459dd308"
	const bodyPrefix = `{"name":"Synthetic method contract","description":"Updated description","end_time":"2030-02-15T23:59:59Z","end_time_timezone":"UTC","project_uid":"synthetic-project","committee_uid":"` + uid + `","poll_questions":[{"prompt":"Rank candidates","type":"multiple_choice","choices":[{"choice_text":"Candidate A"},{"choice_text":"Candidate B"},{"choice_text":"Candidate C"},{"choice_text":"Candidate D"},{"choice_text":"Candidate E"}]}]`
	tests := []struct {
		name        string
		method      string
		extra       string
		wantType    string
		wantWinners string
		wantChange  string
		invalid     string
	}{
		{name: "update omitted", method: http.MethodPut},
		{name: "update null", method: http.MethodPut, extra: `,"poll_type":null,"num_winners":null`},
		{name: "update generic", method: http.MethodPut, extra: `,"poll_type":"generic"`, wantType: `"generic"`},
		{name: "update condorcet", method: http.MethodPut, extra: `,"poll_type":"condorcet_irv"`, wantType: `"condorcet_irv"`},
		{name: "update instant runoff", method: http.MethodPut, extra: `,"poll_type":"instant_runoff_vote"`, wantType: `"instant_runoff_vote"`},
		{name: "update STV four", method: http.MethodPut, extra: `,"poll_type":"meek_stv","num_winners":4`, wantType: `"meek_stv"`, wantWinners: "4"},
		{name: "update STV omitted count", method: http.MethodPut, extra: `,"poll_type":"meek_stv"`, wantType: `"meek_stv"`},
		{name: "update count without type", method: http.MethodPut, extra: `,"num_winners":3`, wantWinners: "3"},
		{name: "update empty type", method: http.MethodPut, extra: `,"poll_type":""`, invalid: "poll_type"},
		{name: "update unknown type", method: http.MethodPut, extra: `,"poll_type":"unknown"`, invalid: "poll_type"},
		{name: "update zero count", method: http.MethodPut, extra: `,"poll_type":"meek_stv","num_winners":0`, invalid: "num_winners"},
		{name: "update one count", method: http.MethodPut, extra: `,"poll_type":"meek_stv","num_winners":1`, invalid: "num_winners"},
		{name: "update count above maximum", method: http.MethodPut, extra: `,"num_winners":11`, invalid: "num_winners"},
		{name: "update allow_vote_change null", method: http.MethodPut, extra: `,"allow_vote_change":null`},
		{name: "update allow_vote_change false", method: http.MethodPut, extra: `,"allow_vote_change":false`, wantChange: "false"},
		{name: "update allow_vote_change true", method: http.MethodPut, extra: `,"allow_vote_change":true`, wantChange: "true"},
		{name: "create omitted", method: http.MethodPost, wantType: `"generic"`},
		{name: "create generic omitted count", method: http.MethodPost, extra: `,"poll_type":"generic"`, wantType: `"generic"`},
		{name: "create condorcet omitted count", method: http.MethodPost, extra: `,"poll_type":"condorcet_irv"`, wantType: `"condorcet_irv"`},
		{name: "create instant runoff omitted count", method: http.MethodPost, extra: `,"poll_type":"instant_runoff_vote"`, wantType: `"instant_runoff_vote"`},
		{name: "create generic explicit count", method: http.MethodPost, extra: `,"poll_type":"generic","num_winners":4`, wantType: `"generic"`, wantWinners: "4"},
		{name: "create STV omitted count", method: http.MethodPost, extra: `,"poll_type":"meek_stv"`, wantType: `"meek_stv"`, wantWinners: "2"},
		{name: "create STV explicit count", method: http.MethodPost, extra: `,"poll_type":"meek_stv","num_winners":4`, wantType: `"meek_stv"`, wantWinners: "4"},
		{name: "create allow_vote_change false", method: http.MethodPost, extra: `,"allow_vote_change":false`, wantType: `"generic"`, wantChange: "false"},
		{name: "create allow_vote_change true", method: http.MethodPost, extra: `,"allow_vote_change":true`, wantType: `"generic"`, wantChange: "true"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wire := make(chan map[string]json.RawMessage, 1)
			mux := voteMethodContractMux(t, wire)
			body := bodyPrefix + tt.extra + "}"
			path := "/votes"
			wantStatus := http.StatusCreated
			if tt.method == http.MethodPut {
				path += "/" + uid
				wantStatus = http.StatusOK
			}
			if tt.invalid != "" {
				wantStatus = http.StatusBadRequest
			}
			request := httptest.NewRequest(tt.method, path, strings.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, request)
			if response.Code != wantStatus {
				t.Fatalf("HTTP status: want %d, got %d: %s", wantStatus, response.Code, response.Body)
			}
			if tt.invalid != "" {
				if !strings.Contains(response.Body.String(), tt.invalid) {
					t.Fatalf("expected %s validation error: %s", tt.invalid, response.Body)
				}
				select {
				case got := <-wire:
					t.Fatalf("invalid request reached ITX: %s", got)
				default:
				}
				return
			}
			assertWire := func(t *testing.T) {
				t.Helper()
				select {
				case got := <-wire:
					for key, want := range map[string]string{"poll_type": tt.wantType, "num_winners": tt.wantWinners, "allow_vote_change": tt.wantChange} {
						value, present := got[key]
						if want == "" && present {
							t.Errorf("%s must be absent, got %s", key, value)
						} else if want != "" && (!present || string(value) != want) {
							t.Errorf("%s: want %s, got %s", key, want, value)
						}
					}
				default:
					t.Fatal("expected an upstream HTTP request")
				}
			}
			assertWire(t)
			// The fake ITX response carries no allow_vote_change, like a legacy poll: the
			// result must still report the resolved setting.
			var result map[string]json.RawMessage
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if string(result["allow_vote_change"]) != "true" {
				t.Errorf("allow_vote_change: want true for a legacy poll, got %s", result["allow_vote_change"])
			}
			if tt.method == http.MethodPut {
				t.Run("generated CLI and client body", func(t *testing.T) {
					payload, err := voteclient.BuildUpdateVotePayload(body, uid, "")
					if err != nil {
						t.Fatal(err)
					}
					encoded, err := json.Marshal(voteclient.NewUpdateVoteRequestBody(payload))
					if err != nil {
						t.Fatal(err)
					}
					request := httptest.NewRequest(http.MethodPut, path, bytes.NewReader(encoded))
					request.Header.Set("Content-Type", "application/json")
					response := httptest.NewRecorder()
					mux.ServeHTTP(response, request)
					if response.Code != http.StatusOK {
						t.Fatalf("generated client HTTP status: %d: %s", response.Code, response.Body)
					}
					assertWire(t)
				})
			}
		})
	}
}

// TestEndVoteMethodContract drives POST /votes/{uid}/end through the generated server
// transport, converters and service to the proxy's actual HTTP serialization. The sink
// answers GetPoll and the bodiless end request; it does not model ITX persistence.
func TestEndVoteMethodContract(t *testing.T) {
	const uid = "a02bdbaf-53b1-4d47-bc04-dd7e459dd308"
	const endedBody = `{"poll_id":"` + uid + `","name":"Synthetic end contract","status":"ended","end_time":"2030-03-01T23:59:59Z","early_end_time":"2030-02-20T09:00:00Z","project_id":"synthetic-project","committee_id":"` + uid + `"}`

	type upstreamCall struct{ method, path, body string }
	newMux := func(t *testing.T, currentStatus string) (http.Handler, *[]upstreamCall) {
		t.Helper()
		var calls []upstreamCall
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			calls = append(calls, upstreamCall{r.Method, r.URL.Path, string(body)})
			w.Header().Set("Content-Type", "application/json")
			if r.Method == http.MethodGet {
				_, _ = io.WriteString(w, `{"poll_id":"`+uid+`","status":"`+currentStatus+`"}`)
				return
			}
			_, _ = io.WriteString(w, endedBody)
		}))
		t.Cleanup(upstream.Close)
		client := proxy.NewContractTestClient(upstream.Client(), upstream.URL+"/")
		voting := service.NewVoteService(nil, client, methodContractIDMapper{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
		mux := goahttp.NewMuxer()
		end := func(ctx context.Context, payload any) (any, error) {
			ctx = context.WithValue(ctx, constants.PrincipalContextID, "synthetic-contract-user")
			poll, err := voting.EndVote(ctx, payload.(*votesvc.EndVotePayload).UID)
			if err != nil {
				// Mirror cmd/voting-api handleError: a domain conflict becomes the Goa ConflictError (409).
				if domain.GetErrorType(err) == domain.ErrorTypeConflict {
					return nil, &votesvc.ConflictError{Code: "409", Message: err.Error()}
				}
				return nil, err
			}
			return converters.ConvertPollResponseToVoteResult(poll), nil
		}
		voteserver.MountEndVoteHandler(mux, voteserver.NewEndVoteHandler(end, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil))
		return mux, &calls
	}

	t.Run("active vote returns the ended vote", func(t *testing.T) {
		mux, calls := newMux(t, "active")
		request := httptest.NewRequest(http.MethodPost, "/votes/"+uid+"/end", nil)
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("HTTP status: want 200, got %d: %s", response.Code, response.Body)
		}
		var result map[string]any
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if result["status"] != "ended" || result["early_end_time"] != "2030-02-20T09:00:00Z" || result["end_time"] != "2030-03-01T23:59:59Z" {
			t.Fatalf("unexpected response shape: %s", response.Body)
		}
		want := []upstreamCall{
			{http.MethodGet, "/v2/voting/poll/" + uid, ""},
			{http.MethodPost, "/v2/voting/poll/" + uid + "/end", ""},
		}
		if len(*calls) != len(want) {
			t.Fatalf("upstream calls: want %v, got %v", want, *calls)
		}
		for i := range want {
			if (*calls)[i] != want[i] {
				t.Errorf("upstream call %d: want %v, got %v", i, want[i], (*calls)[i])
			}
		}
	})

	t.Run("disabled vote returns 409 without calling ITX end", func(t *testing.T) {
		mux, calls := newMux(t, "disabled")
		request := httptest.NewRequest(http.MethodPost, "/votes/"+uid+"/end", nil)
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		if response.Code != http.StatusConflict {
			t.Fatalf("HTTP status: want 409, got %d: %s", response.Code, response.Body)
		}
		for _, call := range *calls {
			if call.method == http.MethodPost {
				t.Fatalf("disabled vote reached ITX end: %v", call)
			}
		}
	})
}

// lockedBallotMessage is the ITX message for a change to a submitted ballot on a poll whose
// allow_vote_change is false.
const lockedBallotMessage = "this poll does not allow changing a submitted vote"

// lockedBallotUpstream fakes ITX rejecting every ballot update with its 409 body and records
// the request paths it received.
func lockedBallotUpstream(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var paths []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, `{"code":"409","message":"`+lockedBallotMessage+`"}`)
	}))
	t.Cleanup(upstream.Close)
	return upstream, &paths
}

// mirrorHandleError mirrors cmd/voting-api handleError for the conflict path: a domain
// conflict becomes the Goa ConflictError, which the generated encoder renders only when the
// method declares a Conflict response.
func mirrorHandleError(err error) error {
	if domain.GetErrorType(err) == domain.ErrorTypeConflict {
		return &votesvc.ConflictError{Code: "409", Message: err.Error()}
	}
	return err
}

// assertLockedBallotConflict checks that a ballot update rejected by ITX reached ITX once and
// was returned to the client as 409 with ITX's message.
func assertLockedBallotConflict(t *testing.T, response *httptest.ResponseRecorder, paths []string, wantPath string) {
	t.Helper()
	if response.Code != http.StatusConflict {
		t.Fatalf("HTTP status: want 409, got %d: %s", response.Code, response.Body)
	}
	var body struct{ Code, Message string }
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v: %s", err, response.Body)
	}
	if body.Code != "409" || body.Message != lockedBallotMessage {
		t.Errorf("error body: want code 409 and ITX's message, got %s", response.Body)
	}
	if len(paths) != 1 || paths[0] != wantPath {
		t.Errorf("upstream calls: want [%s], got %v", wantPath, paths)
	}
}

// TestUpdateVoteResponseConflictContract drives PUT /vote_responses/{uid} through the generated
// server transport (with the production nil formatter), converters and service to the proxy's
// HTTP serialization. ITX's 409 for a locked ballot must reach the client as 409 with ITX's
// message rather than falling through to the default 500.
func TestUpdateVoteResponseConflictContract(t *testing.T) {
	const ballotUID = "b03cdbaf-53b1-4d47-bc04-dd7e459dd309"
	upstream, paths := lockedBallotUpstream(t)
	client := proxy.NewContractTestClient(upstream.Client(), upstream.URL+"/")
	responses := service.NewVoteResponseService(nil, client, methodContractIDMapper{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	update := func(ctx context.Context, payload any) (any, error) {
		ctx = context.WithValue(ctx, constants.PrincipalContextID, "synthetic-contract-user")
		p := payload.(*votesvc.UpdateVoteResponsePayload)
		req, err := converters.ConvertUpdateVoteResponsePayloadToDomain(p)
		if err != nil {
			return nil, mirrorHandleError(err)
		}
		return nil, mirrorHandleError(responses.UpdateVoteResponse(ctx, p.VoteResponseUID, req))
	}
	mux := goahttp.NewMuxer()
	voteserver.MountUpdateVoteResponseHandler(mux, voteserver.NewUpdateVoteResponseHandler(update, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil))

	request := httptest.NewRequest(http.MethodPut, "/vote_responses/"+ballotUID, strings.NewReader(`{"abstain":true}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)

	assertLockedBallotConflict(t, response, *paths, http.MethodPut+" /v2/voting/vote/"+ballotUID)
}

func voteMethodContractMux(t *testing.T, wire chan<- map[string]json.RawMessage) http.Handler {
	t.Helper()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode upstream request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		wire <- body
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"poll_id":"a02bdbaf-53b1-4d47-bc04-dd7e459dd308","name":"Synthetic method contract","description":"Wire response only","status":"disabled","project_id":"synthetic-project","committee_id":"a02bdbaf-53b1-4d47-bc04-dd7e459dd308"}`)
	}))
	t.Cleanup(upstream.Close)
	client := proxy.NewContractTestClient(upstream.Client(), upstream.URL+"/")
	voting := service.NewVoteService(nil, client, methodContractIDMapper{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	mux := goahttp.NewMuxer()
	create := func(ctx context.Context, payload any) (any, error) {
		ctx = context.WithValue(ctx, constants.PrincipalContextID, "synthetic-contract-user")
		poll, err := voting.CreateVote(ctx, converters.ConvertCreateVotePayloadToDomain(payload.(*votesvc.CreateVotePayload)))
		if err != nil {
			return nil, err
		}
		return converters.ConvertPollResponseToVoteResult(poll), nil
	}
	update := func(ctx context.Context, payload any) (any, error) {
		ctx = context.WithValue(ctx, constants.PrincipalContextID, "synthetic-contract-user")
		p := payload.(*votesvc.UpdateVotePayload)
		poll, err := voting.UpdateVote(ctx, p.UID, converters.ConvertUpdateVotePayloadToDomain(p))
		if err != nil {
			return nil, err
		}
		return converters.ConvertPollResponseToVoteResult(poll), nil
	}
	voteserver.MountCreateVoteHandler(mux, voteserver.NewCreateVoteHandler(create, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil))
	voteserver.MountUpdateVoteHandler(mux, voteserver.NewUpdateVoteHandler(update, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil))
	return mux
}

type methodContractIDMapper struct{}

func (methodContractIDMapper) MapProjectV2ToV1(_ context.Context, id string) (string, error) {
	return id, nil
}
func (methodContractIDMapper) MapProjectV1ToV2(_ context.Context, id string) (string, error) {
	return id, nil
}
func (methodContractIDMapper) MapCommitteeV2ToV1(_ context.Context, id string) (string, error) {
	return id, nil
}
func (methodContractIDMapper) MapCommitteeV1ToV2(_ context.Context, id string) (string, error) {
	return id, nil
}
