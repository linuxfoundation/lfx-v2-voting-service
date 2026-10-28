// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package votelink

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

const (
	testVoteUID     = "8f3c2a1e-4b5d-4e6f-9a7b-1c2d3e4f5a6b"
	testResponseUID = "cba14f40-1636-11ec-9621-0242ac130002"
)

func testKey(fill byte) []byte {
	key := make([]byte, MinKeyLength)
	for i := range key {
		key[i] = fill
	}
	return key
}

func newTestSigner(t *testing.T, keys map[string][]byte, active string) *HMACSigner {
	t.Helper()
	s, err := NewHMACSigner(keys, active)
	if err != nil {
		t.Fatalf("NewHMACSigner: %v", err)
	}
	s.now = func() time.Time { return time.Unix(1793232000, 0) }
	return s
}

func TestSignVerify_RoundTrip(t *testing.T) {
	s := newTestSigner(t, map[string][]byte{"k1": testKey('a')}, "k1")

	token, err := s.Sign(testVoteUID, testResponseUID)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if !strings.HasPrefix(token, "v1.k1.") {
		t.Errorf("token %q does not start with v1.k1.", token)
	}

	claims, err := s.Verify(token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.VoteUID != testVoteUID || claims.VoteResponseUID != testResponseUID || claims.KeyID != "k1" {
		t.Errorf("claims = %+v", claims)
	}
}

func TestVerify_RejectsTampering(t *testing.T) {
	s := newTestSigner(t, map[string][]byte{"k1": testKey('a')}, "k1")
	token, err := s.Sign(testVoteUID, testResponseUID)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	parts := strings.Split(token, ".")

	otherPayload := base64.RawURLEncoding.EncodeToString(
		[]byte(`{"v":"` + testVoteUID + `","r":"00000000-0000-0000-0000-000000000000","iat":1}`))

	cases := map[string]string{
		"empty":             "",
		"too long":          strings.Repeat("a", MaxTokenLength+1),
		"three segments":    strings.Join(parts[:3], "."),
		"five segments":     token + ".x",
		"wrong version":     "v2." + strings.Join(parts[1:], "."),
		"unknown kid":       parts[0] + ".k9." + parts[2] + "." + parts[3],
		"swapped payload":   parts[0] + "." + parts[1] + "." + otherPayload + "." + parts[3],
		"bad signature b64": parts[0] + "." + parts[1] + "." + parts[2] + ".!!!",
		"truncated sig":     token[:len(token)-2],
	}
	for name, tok := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := s.Verify(tok); err != ErrInvalidToken {
				t.Errorf("Verify(%q) err = %v, want ErrInvalidToken", tok, err)
			}
		})
	}
}

func TestVerify_RejectsTokenFromDifferentKey(t *testing.T) {
	a := newTestSigner(t, map[string][]byte{"k1": testKey('a')}, "k1")
	b := newTestSigner(t, map[string][]byte{"k1": testKey('b')}, "k1")

	token, err := a.Sign(testVoteUID, testResponseUID)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if _, err := b.Verify(token); err != ErrInvalidToken {
		t.Errorf("err = %v, want ErrInvalidToken", err)
	}
}

func TestVerify_RejectsValidMACOverBadPayload(t *testing.T) {
	s := newTestSigner(t, map[string][]byte{"k1": testKey('a')}, "k1")
	for name, body := range map[string]string{
		"not json":     "nope",
		"missing vote": `{"r":"x","iat":1}`,
		"missing resp": `{"v":"x","iat":1}`,
	} {
		t.Run(name, func(t *testing.T) {
			signed := "v1.k1." + base64.RawURLEncoding.EncodeToString([]byte(body))
			token := signed + "." + base64.RawURLEncoding.EncodeToString(s.mac(s.keys["k1"], signed))
			if _, err := s.Verify(token); err != ErrInvalidToken {
				t.Errorf("err = %v, want ErrInvalidToken", err)
			}
		})
	}
}

func TestKeyRotation_OldKeyStillVerifies(t *testing.T) {
	old := newTestSigner(t, map[string][]byte{"k1": testKey('a')}, "k1")
	oldToken, err := old.Sign(testVoteUID, testResponseUID)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	rotated := newTestSigner(t, map[string][]byte{"k1": testKey('a'), "k2": testKey('b')}, "k2")
	if _, err := rotated.Verify(oldToken); err != nil {
		t.Errorf("old token should verify after rotation: %v", err)
	}
	newToken, err := rotated.Sign(testVoteUID, testResponseUID)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if !strings.HasPrefix(newToken, "v1.k2.") {
		t.Errorf("rotated signer should sign with k2, got %q", newToken)
	}

	// Removing k1 from the set revokes every link signed with it.
	retired := newTestSigner(t, map[string][]byte{"k2": testKey('b')}, "k2")
	if _, err := retired.Verify(oldToken); err != ErrInvalidToken {
		t.Errorf("token signed with removed key should fail, got %v", err)
	}
}

func TestSign_RequiresIDs(t *testing.T) {
	s := newTestSigner(t, map[string][]byte{"k1": testKey('a')}, "k1")
	if _, err := s.Sign("", testResponseUID); err == nil {
		t.Error("expected error for empty vote uid")
	}
	if _, err := s.Sign(testVoteUID, ""); err == nil {
		t.Error("expected error for empty vote response uid")
	}
}

func TestNewHMACSigner_Validation(t *testing.T) {
	cases := map[string]struct {
		keys   map[string][]byte
		active string
	}{
		"no keys":        {map[string][]byte{}, "k1"},
		"short key":      {map[string][]byte{"k1": []byte("short")}, "k1"},
		"bad kid":        {map[string][]byte{"k.1": testKey('a')}, "k.1"},
		"missing active": {map[string][]byte{"k1": testKey('a')}, "k2"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := NewHMACSigner(tc.keys, tc.active); err == nil {
				t.Error("expected error")
			}
		})
	}
}

func TestParseKeys(t *testing.T) {
	std := base64.StdEncoding.EncodeToString(testKey('a'))
	url := base64.RawURLEncoding.EncodeToString(testKey(0xfb))

	keys, err := ParseKeys(" k1:" + std + " , k2:" + url + ",")
	if err != nil {
		t.Fatalf("ParseKeys: %v", err)
	}
	if string(keys["k1"]) != string(testKey('a')) || string(keys["k2"]) != string(testKey(0xfb)) {
		t.Errorf("decoded keys mismatch: %v", keys)
	}

	for name, raw := range map[string]string{
		"empty":       "",
		"no colon":    "k1" + std,
		"empty kid":   ":" + std,
		"duplicate":   "k1:" + std + ",k1:" + std,
		"invalid b64": "k1:%%%",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseKeys(raw); err == nil {
				t.Error("expected error")
			}
		})
	}
}
