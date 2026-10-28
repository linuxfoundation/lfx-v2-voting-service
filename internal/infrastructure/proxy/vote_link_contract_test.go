// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package proxy_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	converters "github.com/linuxfoundation/lfx-v2-voting-service/cmd/voting-api/service"
	voteserver "github.com/linuxfoundation/lfx-v2-voting-service/gen/http/vote/server"
	votesvc "github.com/linuxfoundation/lfx-v2-voting-service/gen/vote"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/infrastructure/proxy"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/infrastructure/votelink"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/service"
	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/constants"
	goahttp "goa.design/goa/v3/http"
)

const (
	linkVoteUID   = "8f3c2a1e-4b5d-4e6f-9a7b-1c2d3e4f5a6b"
	linkBallotUID = "cba14f40-1636-11ec-9621-0242ac130002"
)

type upstreamCall struct {
	method string
	path   string
	body   map[string]json.RawMessage
}

// TestVoteLinkContract drives personalized vote links through the generated transports,
// converters, service, real HMAC signer, and proxy HTTP serialization: bulk resend emits a
// recipient_links map whose token the link endpoints accept, and the link endpoints send
// ITX the ballot from the token.
func TestVoteLinkContract(t *testing.T) {
	signer, err := votelink.NewHMACSigner(map[string][]byte{"k1": []byte(strings.Repeat("s", votelink.MinKeyLength))}, "k1")
	if err != nil {
		t.Fatal(err)
	}
	wire := make(chan upstreamCall, 1)
	mux := voteLinkContractMux(t, wire, signer)

	serve := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	next := func(t *testing.T) upstreamCall {
		t.Helper()
		select {
		case call := <-wire:
			return call
		default:
			t.Fatal("expected an upstream HTTP request")
			return upstreamCall{}
		}
	}

	t.Run("bulk resend without personalized links is unchanged", func(t *testing.T) {
		rec := serve(http.MethodPost, "/votes/"+linkVoteUID+"/bulk_resend", `{"recipient_ids":["`+linkBallotUID+`"]}`)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status %d: %s", rec.Code, rec.Body)
		}
		call := next(t)
		if call.path != "/v2/voting/poll/"+linkVoteUID+"/bulk_resend" {
			t.Errorf("path = %s", call.path)
		}
		if _, present := call.body["recipient_links"]; present {
			t.Errorf("recipient_links must be absent, got %s", call.body["recipient_links"])
		}
	})

	var token string
	t.Run("bulk resend with personalized links", func(t *testing.T) {
		rec := serve(http.MethodPost, "/votes/"+linkVoteUID+"/bulk_resend",
			`{"recipient_ids":["`+linkBallotUID+`"],"personalized_links":true}`)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status %d: %s", rec.Code, rec.Body)
		}
		var links map[string]string
		if err := json.Unmarshal(next(t).body["recipient_links"], &links); err != nil {
			t.Fatalf("decode recipient_links: %v", err)
		}
		link, err := url.Parse(links[linkBallotUID])
		if err != nil {
			t.Fatalf("parse link %q: %v", links[linkBallotUID], err)
		}
		if link.Scheme != "https" || link.Host != "app.lfx.dev" || link.Path != "/votes/"+linkVoteUID {
			t.Errorf("link = %s", link)
		}
		if link.RawQuery != "" {
			t.Errorf("token must not be in the query string: %s", link)
		}
		fragment, err := url.ParseQuery(link.Fragment)
		if err != nil {
			t.Fatalf("parse fragment: %v", err)
		}
		token = fragment.Get("vote_token")
		claims, err := signer.Verify(token)
		if err != nil {
			t.Fatalf("emailed token does not verify: %v", err)
		}
		if claims.VoteUID != linkVoteUID || claims.VoteResponseUID != linkBallotUID {
			t.Errorf("claims = %+v", claims)
		}
	})

	t.Run("submit by link sends the token's ballot", func(t *testing.T) {
		body := `{"vote_token":"` + token + `","abstain":true,"vote_response_uid":"ignored"}`
		rec := serve(http.MethodPost, "/vote_links/responses", body)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status %d: %s", rec.Code, rec.Body)
		}
		call := next(t)
		if call.method != http.MethodPost || call.path != "/v2/voting/vote" {
			t.Errorf("upstream = %s %s", call.method, call.path)
		}
		if string(call.body["vote_id"]) != `"`+linkBallotUID+`"` {
			t.Errorf("vote_id = %s", call.body["vote_id"])
		}
	})

	t.Run("update by link sends the token's ballot", func(t *testing.T) {
		rec := serve(http.MethodPut, "/vote_links/responses", `{"vote_token":"`+token+`","abstain":true}`)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status %d: %s", rec.Code, rec.Body)
		}
		call := next(t)
		if call.method != http.MethodPut || call.path != "/v2/voting/vote/"+linkBallotUID {
			t.Errorf("upstream = %s %s", call.method, call.path)
		}
		if _, present := call.body["comment_responses"]; present {
			t.Error("omitted comment_responses must stay omitted so ITX preserves existing comments")
		}
	})

	t.Run("tampered token is rejected before ITX", func(t *testing.T) {
		for _, method := range []string{http.MethodPost, http.MethodPut} {
			rec := serve(method, "/vote_links/responses", `{"vote_token":"`+token+`x","abstain":true}`)
			if rec.Code != http.StatusNotFound {
				t.Errorf("%s status = %d, want 404: %s", method, rec.Code, rec.Body)
			}
		}
		select {
		case call := <-wire:
			t.Fatalf("rejected request reached ITX: %+v", call)
		default:
		}
	})

	t.Run("missing token fails validation", func(t *testing.T) {
		rec := serve(http.MethodPost, "/vote_links/responses", `{"abstain":true}`)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400: %s", rec.Code, rec.Body)
		}
	})
}

func voteLinkContractMux(t *testing.T, wire chan<- upstreamCall, signer *votelink.HMACSigner) http.Handler {
	t.Helper()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode upstream request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		wire <- upstreamCall{method: r.Method, path: r.URL.Path, body: body}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(upstream.Close)

	client := proxy.NewContractTestClient(upstream.Client(), upstream.URL+"/")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	voting := service.NewVoteService(nil, client, methodContractIDMapper{}, logger,
		service.WithVoteLinks(signer, "https://app.lfx.dev"))
	responses := service.NewVoteResponseService(nil, client, methodContractIDMapper{}, logger,
		service.WithVoteLinkVerifier(signer))

	bulkResend := func(ctx context.Context, payload any) (any, error) {
		ctx = context.WithValue(ctx, constants.PrincipalContextID, "synthetic-contract-user")
		p := payload.(*votesvc.BulkResendVotePayload)
		return nil, voting.BulkResendVote(ctx, p.UID, p.RecipientIds, p.PersonalizedLinks)
	}
	createByLink := func(ctx context.Context, payload any) (any, error) {
		p := payload.(*votesvc.CreateVoteResponseByLinkPayload)
		req, err := converters.ConvertCreateVoteResponseByLinkPayloadToDomain(p)
		if err != nil {
			return nil, err
		}
		return nil, responses.CreateVoteResponseByLink(ctx, p.VoteToken, req)
	}
	updateByLink := func(ctx context.Context, payload any) (any, error) {
		p := payload.(*votesvc.UpdateVoteResponseByLinkPayload)
		req, err := converters.ConvertUpdateVoteResponseByLinkPayloadToDomain(p)
		if err != nil {
			return nil, err
		}
		return nil, responses.UpdateVoteResponseByLink(ctx, p.VoteToken, req)
	}

	// Map domain errors to statuses the way cmd/voting-api handleError does; leave Goa's
	// own decode/validation errors to the default formatter.
	formatter := func(ctx context.Context, err error) goahttp.Statuser {
		var dErr *domain.DomainError
		if errors.As(err, &dErr) {
			return domainStatus{err: dErr}
		}
		return goahttp.NewErrorResponse(ctx, err)
	}

	mux := goahttp.NewMuxer()
	voteserver.MountBulkResendVoteHandler(mux, voteserver.NewBulkResendVoteHandler(bulkResend, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, formatter))
	voteserver.MountCreateVoteResponseByLinkHandler(mux, voteserver.NewCreateVoteResponseByLinkHandler(createByLink, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, formatter))
	voteserver.MountUpdateVoteResponseByLinkHandler(mux, voteserver.NewUpdateVoteResponseByLinkHandler(updateByLink, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, formatter))
	return mux
}

// domainStatus renders a domain error with the HTTP status handleError would use.
type domainStatus struct {
	err *domain.DomainError
}

func (d domainStatus) StatusCode() int {
	switch d.err.Type {
	case domain.ErrorTypeValidation:
		return http.StatusBadRequest
	case domain.ErrorTypeNotFound:
		return http.StatusNotFound
	case domain.ErrorTypeConflict:
		return http.StatusConflict
	case domain.ErrorTypeUnavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

// TestVoteLinkBallotContract drives POST /vote_links/ballot through the generated transport,
// service, real HMAC signer and proxy client against a fake ITX: the token's ids select the
// ballot and the vote, the response carries only ballot fields, and pseudo-anonymous votes
// never return ballot contents.
func TestVoteLinkBallotContract(t *testing.T) {
	signer, err := votelink.NewHMACSigner(map[string][]byte{"k1": []byte(strings.Repeat("s", votelink.MinKeyLength))}, "k1")
	if err != nil {
		t.Fatal(err)
	}
	token, err := signer.Sign(linkVoteUID, linkBallotUID)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name        string
		pseudoField string // JSON fragment for pseudo_anonymity, "" = omitted
		wantAnswers bool
	}{
		{name: "regular vote", pseudoField: `"pseudo_anonymity":false,`, wantAnswers: true},
		{name: "pseudo-anonymous vote", pseudoField: `"pseudo_anonymity":true,`},
		{name: "pseudo_anonymity missing fails closed", pseudoField: ``},
		{name: "pseudo_anonymity null fails closed", pseudoField: `"pseudo_anonymity":null,`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var paths []string
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				paths = append(paths, r.Method+" "+r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/v2/voting/vote/" + linkBallotUID:
					_, _ = io.WriteString(w, `{"vote_id":"`+linkBallotUID+`","poll_id":"`+linkVoteUID+`","vote_status":"responded","abstained":false,"user_email":"voter@example.org","voter_removed":false,`+
						`"poll_answers":[{"question_id":"q1","prompt":"Who?","type":"single_choice","user_choice":[{"choice_id":"c-a","choice_text":"Alice"}]}],"comment_responses":[]}`)
				case "/v2/voting/poll/" + linkVoteUID:
					_, _ = io.WriteString(w, `{"poll_id":"`+linkVoteUID+`","name":"TSC election","description":"Pick one","status":"active","end_time":"2030-01-01T00:00:00Z",`+
						`"project_id":"a0941000002wBz4AAE","committee_id":"c1","total_voting_request_invitations":9,"num_response_received":4,`+tc.pseudoField+
						`"allow_abstain":true,"poll_type":"generic","poll_questions":[{"question_id":"q1","prompt":"Who?","type":"single_choice","choices":[{"choice_id":"c-a","choice_text":"Alice"}]}]}`)
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer upstream.Close()

			client := proxy.NewContractTestClient(upstream.Client(), upstream.URL+"/")
			responses := service.NewVoteResponseService(nil, client, methodContractIDMapper{}, slog.New(slog.NewTextHandler(io.Discard, nil)),
				service.WithVoteLinkVerifier(signer), service.WithVoteLinkPolls(client))
			getBallot := func(ctx context.Context, payload any) (any, error) {
				b, err := responses.GetBallotByLink(ctx, payload.(*votesvc.GetVoteBallotByLinkPayload).VoteToken)
				if err != nil {
					return nil, err
				}
				return converters.ConvertVoteLinkBallotToResult(b), nil
			}
			formatter := func(ctx context.Context, err error) goahttp.Statuser {
				var dErr *domain.DomainError
				if errors.As(err, &dErr) {
					return domainStatus{err: dErr}
				}
				return goahttp.NewErrorResponse(ctx, err)
			}
			mux := goahttp.NewMuxer()
			voteserver.MountGetVoteBallotByLinkHandler(mux, voteserver.NewGetVoteBallotByLinkHandler(getBallot, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, formatter))

			req := httptest.NewRequest(http.MethodPost, "/vote_links/ballot", strings.NewReader(`{"vote_token":"`+token+`"}`))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status %d: %s", rec.Code, rec.Body)
			}
			if len(paths) != 2 || paths[0] != "GET /v2/voting/vote/"+linkBallotUID || paths[1] != "GET /v2/voting/poll/"+linkVoteUID {
				t.Errorf("upstream calls = %v", paths)
			}

			var body map[string]json.RawMessage
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"vote_uid", "name", "status", "poll_questions", "ballot"} {
				if _, ok := body[key]; !ok {
					t.Errorf("missing %s: %s", key, rec.Body)
				}
			}
			for _, leak := range []string{"project", "committee", "num_response_received", "total_voting_request_invitations", "voter@example.org", linkBallotUID} {
				if strings.Contains(rec.Body.String(), leak) {
					t.Errorf("response leaks %q: %s", leak, rec.Body)
				}
			}
			var ballot map[string]json.RawMessage
			if err := json.Unmarshal(body["ballot"], &ballot); err != nil {
				t.Fatal(err)
			}
			if string(ballot["already_voted"]) != "true" {
				t.Errorf("already_voted = %s", ballot["already_voted"])
			}
			_, hasAnswers := ballot["poll_answers"]
			if hasAnswers != tc.wantAnswers {
				t.Errorf("poll_answers present = %v, want %v: %s", hasAnswers, tc.wantAnswers, body["ballot"])
			}
			if wantPseudo := strconv.FormatBool(!tc.wantAnswers); string(body["pseudo_anonymity"]) != wantPseudo {
				t.Errorf("pseudo_anonymity = %s, want %s", body["pseudo_anonymity"], wantPseudo)
			}
		})
	}

	t.Run("tampered token is rejected before ITX", func(t *testing.T) {
		called := false
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
		defer upstream.Close()
		client := proxy.NewContractTestClient(upstream.Client(), upstream.URL+"/")
		responses := service.NewVoteResponseService(nil, client, methodContractIDMapper{}, slog.New(slog.NewTextHandler(io.Discard, nil)),
			service.WithVoteLinkVerifier(signer), service.WithVoteLinkPolls(client))
		_, err := responses.GetBallotByLink(context.Background(), token+"x")
		var dErr *domain.DomainError
		if !errors.As(err, &dErr) || dErr.Type != domain.ErrorTypeNotFound || called {
			t.Errorf("err = %v, upstream called = %v", err, called)
		}
	})
}
