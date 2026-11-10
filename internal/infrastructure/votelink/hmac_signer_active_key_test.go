// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package votelink

import (
	"strings"
	"testing"
)

func TestActiveKeyID(t *testing.T) {
	keys := map[string][]byte{"k1": testKey('a'), "k2": testKey('b')}

	for _, active := range []string{"k1", "k2"} {
		s := newTestSigner(t, keys, active)

		if got := s.ActiveKeyID(); got != active {
			t.Errorf("ActiveKeyID() = %q, want %q", got, active)
		}

		token, err := s.Sign(testVoteUID, testResponseUID)
		if err != nil {
			t.Fatalf("Sign: %v", err)
		}
		if !strings.HasPrefix(token, "v1."+active+".") {
			t.Errorf("token %q should be signed with the active key %q", token, active)
		}
	}
}
