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
		token = link.Query().Get("vote_token")
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
