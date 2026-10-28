// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLimitVoteLinkBody(t *testing.T) {
	var readErr error
	handler := limitVoteLinkBody(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, readErr = io.ReadAll(r.Body)
	}))
	oversized := strings.Repeat("a", maxVoteLinkBodyBytes+1)

	tests := []struct {
		path    string
		wantErr bool
	}{
		{path: "/vote_links/responses", wantErr: true},
		{path: "/vote_responses", wantErr: false},
		{path: "/votes/x/bulk_resend", wantErr: false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			readErr = nil
			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader(oversized)))
			if (readErr != nil) != tt.wantErr {
				t.Errorf("read error = %v, wantErr %v", readErr, tt.wantErr)
			}
		})
	}
}

func TestNewVoteLinkSigner(t *testing.T) {
	key := func(fill string) string { return base64.StdEncoding.EncodeToString([]byte(strings.Repeat(fill, 32))) }

	tests := []struct {
		name       string
		cfg        config
		wantNil    bool
		wantErr    bool
		wantActive string
	}{
		{name: "unset disables the feature", cfg: config{}, wantNil: true},
		{name: "single key is active", cfg: config{VoteLinkSigningKeys: "k1:" + key("a")}, wantActive: "k1"},
		{name: "first listed key is active by default", cfg: config{VoteLinkSigningKeys: "k2:" + key("b") + ",k1:" + key("a")}, wantActive: "k2"},
		{name: "explicit active kid wins", cfg: config{VoteLinkSigningKeys: "k2:" + key("b") + ",k1:" + key("a"), VoteLinkActiveKID: "k1"}, wantActive: "k1"},
		{name: "unknown active kid fails", cfg: config{VoteLinkSigningKeys: "k1:" + key("a"), VoteLinkActiveKID: "k9"}, wantErr: true},
		{name: "malformed keys fail", cfg: config{VoteLinkSigningKeys: "nocolon"}, wantErr: true},
		{name: "short key fails", cfg: config{VoteLinkSigningKeys: "k1:" + base64.StdEncoding.EncodeToString([]byte("short"))}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			signer, err := newVoteLinkSigner(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if (signer == nil) != tt.wantNil {
				t.Fatalf("signer nil = %v, want %v", signer == nil, tt.wantNil)
			}
			if signer != nil && signer.ActiveKeyID() != tt.wantActive {
				t.Errorf("active key = %q, want %q", signer.ActiveKeyID(), tt.wantActive)
			}
		})
	}
}
