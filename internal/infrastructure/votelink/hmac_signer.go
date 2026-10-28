// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

// Package votelink implements HMAC-signed tokens for personalized vote links.
//
// Token format: v1.<kid>.<base64url(payload)>.<base64url(HMAC-SHA256(key[kid], "v1.<kid>.<payload>"))>
// where payload is the JSON object {"v":<vote_uid>,"r":<vote_response_uid>,"iat":<unix seconds>}.
// Tokens carry no expiry: ITX rejects submissions while the vote is ended or the voter is
// removed. Extending an ended vote reactivates its links; only removing the voter or the
// signing key revokes them.
package votelink

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
)

const (
	tokenVersion = "v1"
	// MinKeyLength is the minimum decoded length, in bytes, of a signing key.
	MinKeyLength = 32
	// MaxTokenLength bounds the work done on untrusted input before verification.
	MaxTokenLength = 1024
)

var (
	kidPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)
	b64        = base64.RawURLEncoding

	// ErrInvalidToken is returned by Verify for any token that fails verification.
	// A single error avoids revealing which check failed.
	ErrInvalidToken = errors.New("invalid vote link token")
)

type payload struct {
	VoteUID         string `json:"v"`
	VoteResponseUID string `json:"r"`
	IssuedAt        int64  `json:"iat"`
}

// HMACSigner implements domain.VoteLinkSigner using HMAC-SHA256 with a rotatable key set.
type HMACSigner struct {
	keys      map[string][]byte
	activeKID string
	now       func() time.Time
}

var _ domain.VoteLinkSigner = (*HMACSigner)(nil)

// NewHMACSigner builds a signer from a key set and the id of the key used for signing.
// Every key in the set verifies, so retired keys can stay listed until links signed
// with them are no longer needed.
func NewHMACSigner(keys map[string][]byte, activeKID string) (*HMACSigner, error) {
	if len(keys) == 0 {
		return nil, errors.New("at least one signing key is required")
	}
	copied := make(map[string][]byte, len(keys))
	for kid, key := range keys {
		if !kidPattern.MatchString(kid) {
			return nil, fmt.Errorf("invalid key id %q: must match %s", kid, kidPattern.String())
		}
		if len(key) < MinKeyLength {
			return nil, fmt.Errorf("key %q is %d bytes; at least %d are required", kid, len(key), MinKeyLength)
		}
		copied[kid] = append([]byte(nil), key...)
	}
	if _, ok := copied[activeKID]; !ok {
		return nil, fmt.Errorf("active key id %q is not in the key set", activeKID)
	}
	return &HMACSigner{keys: copied, activeKID: activeKID, now: time.Now}, nil
}

// ParseKeys parses a key set in the form "kid1:base64key,kid2:base64key" and also returns the
// key ids in the order listed, so callers can treat the first as the default signing key. Keys
// may use standard or URL-safe base64, with or without padding.
func ParseKeys(raw string) (map[string][]byte, []string, error) {
	keys := make(map[string][]byte)
	var order []string
	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		kid, encoded, ok := strings.Cut(entry, ":")
		if !ok || kid == "" || encoded == "" {
			return nil, nil, errors.New("each key entry must be in the form kid:base64key")
		}
		if _, dup := keys[kid]; dup {
			return nil, nil, fmt.Errorf("duplicate key id %q", kid)
		}
		key, err := decodeKey(encoded)
		if err != nil {
			return nil, nil, fmt.Errorf("key %q is not valid base64: %w", kid, err)
		}
		keys[kid] = key
		order = append(order, kid)
	}
	if len(keys) == 0 {
		return nil, nil, errors.New("no signing keys found")
	}
	return keys, order, nil
}

func decodeKey(encoded string) ([]byte, error) {
	encoded = strings.TrimRight(encoded, "=")
	if key, err := base64.RawStdEncoding.DecodeString(encoded); err == nil {
		return key, nil
	}
	return base64.RawURLEncoding.DecodeString(encoded)
}

// ActiveKeyID returns the id of the key used for signing.
func (s *HMACSigner) ActiveKeyID() string {
	return s.activeKID
}

// Sign returns a token binding the vote and ballot, signed with the active key.
func (s *HMACSigner) Sign(voteUID, voteResponseUID string) (string, error) {
	if voteUID == "" || voteResponseUID == "" {
		return "", errors.New("vote uid and vote response uid are required")
	}
	body, err := json.Marshal(payload{
		VoteUID:         voteUID,
		VoteResponseUID: voteResponseUID,
		IssuedAt:        s.now().Unix(),
	})
	if err != nil {
		return "", fmt.Errorf("marshal vote link payload: %w", err)
	}
	signed := tokenVersion + "." + s.activeKID + "." + b64.EncodeToString(body)
	return signed + "." + b64.EncodeToString(s.mac(s.keys[s.activeKID], signed)), nil
}

// Verify checks the token's version, key id, and signature, then returns its claims.
func (s *HMACSigner) Verify(token string) (*domain.VoteLinkClaims, error) {
	if token == "" || len(token) > MaxTokenLength {
		return nil, ErrInvalidToken
	}
	parts := strings.Split(token, ".")
	if len(parts) != 4 || parts[0] != tokenVersion {
		return nil, ErrInvalidToken
	}
	key, ok := s.keys[parts[1]]
	if !ok {
		return nil, ErrInvalidToken
	}
	sig, err := b64.DecodeString(parts[3])
	if err != nil {
		return nil, ErrInvalidToken
	}
	signed := parts[0] + "." + parts[1] + "." + parts[2]
	if !hmac.Equal(sig, s.mac(key, signed)) {
		return nil, ErrInvalidToken
	}
	body, err := b64.DecodeString(parts[2])
	if err != nil {
		return nil, ErrInvalidToken
	}
	var p payload
	if err := json.Unmarshal(body, &p); err != nil || p.VoteUID == "" || p.VoteResponseUID == "" {
		return nil, ErrInvalidToken
	}
	return &domain.VoteLinkClaims{
		VoteUID:         p.VoteUID,
		VoteResponseUID: p.VoteResponseUID,
		KeyID:           parts[1],
	}, nil
}

func (s *HMACSigner) mac(key []byte, signed string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(signed))
	return h.Sum(nil)
}
