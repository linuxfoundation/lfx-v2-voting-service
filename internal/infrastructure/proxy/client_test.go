// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/service"
	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/constants"
	goahttp "goa.design/goa/v3/http"
)

// TestMapHTTPError protects the HTTP status → domain error type mapping in mapHTTPError.
//
// This mapping is security-relevant: 401/403 intentionally map to ErrorTypeInternal
// (not ErrorTypeValidation) because the service uses M2M credentials that should
// never be rejected by ITX, so an auth failure implies an infrastructure problem.
// If this mapping is accidentally changed, clients will receive incorrect HTTP
// status codes and downstream error handling will break.
func TestMapHTTPError(t *testing.T) {
	c := &Client{} // mapHTTPError uses only json and domain — no httpClient needed

	tests := []struct {
		name        string
		statusCode  int
		body        []byte
		wantType    domain.ErrorType
		wantMessage string
	}{
		{
			name:       "400 maps to Validation",
			statusCode: 400,
			body:       []byte(`{"message":"invalid request"}`),
			wantType:   domain.ErrorTypeValidation,
		},
		{
			name:       "400 falls back to error field when message absent",
			statusCode: 400,
			body:       []byte(`{"error":"bad input"}`),
			wantType:   domain.ErrorTypeValidation,
		},
		{
			name:        "400 with empty body uses default message",
			statusCode:  400,
			body:        []byte(`{}`),
			wantType:    domain.ErrorTypeValidation,
			wantMessage: "ITX API error: HTTP 400",
		},
		{
			// 401 from ITX indicates broken M2M credentials, not a user auth failure.
			// Mapping it to Internal instead of Validation is deliberate — do not change.
			name:       "401 maps to Internal (not Validation)",
			statusCode: 401,
			body:       []byte(`{"message":"unauthorized"}`),
			wantType:   domain.ErrorTypeInternal,
		},
		{
			// Same reasoning as 401 — bad M2M setup, not a caller permission problem.
			name:       "403 maps to Internal (not Validation)",
			statusCode: 403,
			body:       []byte(`{"message":"forbidden"}`),
			wantType:   domain.ErrorTypeInternal,
		},
		{
			name:       "404 maps to NotFound",
			statusCode: 404,
			body:       []byte(`{"message":"poll not found"}`),
			wantType:   domain.ErrorTypeNotFound,
		},
		{
			name:       "409 maps to Conflict",
			statusCode: 409,
			body:       []byte(`{"message":"already exists"}`),
			wantType:   domain.ErrorTypeConflict,
		},
		{
			name:       "429 maps to Unavailable",
			statusCode: 429,
			body:       []byte(`{"message":"rate limited"}`),
			wantType:   domain.ErrorTypeUnavailable,
		},
		{
			name:       "503 maps to Unavailable",
			statusCode: 503,
			body:       []byte(`{"message":"service unavailable"}`),
			wantType:   domain.ErrorTypeUnavailable,
		},
		{
			name:       "500 maps to Internal",
			statusCode: 500,
			body:       []byte(`{"message":"internal error"}`),
			wantType:   domain.ErrorTypeInternal,
		},
		{
			name:       "unrecognised 5xx maps to Internal",
			statusCode: 502,
			body:       []byte(`{"message":"bad gateway"}`),
			wantType:   domain.ErrorTypeInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := c.mapHTTPError(tt.statusCode, tt.body)
			if err == nil {
				t.Fatal("expected non-nil error")
			}

			var domainErr *domain.DomainError
			if !errors.As(err, &domainErr) {
				t.Fatalf("expected *domain.DomainError, got %T: %v", err, err)
			}

			if domainErr.Type != tt.wantType {
				t.Errorf("status %d: want ErrorType %d, got %d (message: %q)",
					tt.statusCode, tt.wantType, domainErr.Type, domainErr.Message)
			}

			if tt.wantMessage != "" && domainErr.Message != tt.wantMessage {
				t.Errorf("want message %q, got %q", tt.wantMessage, domainErr.Message)
			}
		})
	}
}

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
		{name: "create omitted", method: http.MethodPost, wantType: `"generic"`, wantWinners: "2"},
		{name: "create STV omitted count", method: http.MethodPost, extra: `,"poll_type":"meek_stv"`, wantType: `"meek_stv"`, wantWinners: "2"},
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
					for key, want := range map[string]string{"poll_type": tt.wantType, "num_winners": tt.wantWinners} {
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
	client := &Client{httpClient: upstream.Client(), config: Config{BaseURL: upstream.URL + "/"}}
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
