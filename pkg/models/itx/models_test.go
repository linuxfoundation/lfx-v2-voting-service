// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package itx

import (
	"encoding/json"
	"testing"
)

func TestPollResponseUnmarshal_PseudoAnonymityPresence(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		wantValue bool
		wantKnown bool
	}{
		{name: "true", body: `{"poll_id":"p1","pseudo_anonymity":true}`, wantValue: true, wantKnown: true},
		{name: "false", body: `{"poll_id":"p1","pseudo_anonymity":false}`, wantValue: false, wantKnown: true},
		{name: "missing", body: `{"poll_id":"p1"}`, wantValue: false, wantKnown: false},
		{name: "null", body: `{"poll_id":"p1","pseudo_anonymity":null}`, wantValue: false, wantKnown: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var p PollResponse
			if err := json.Unmarshal([]byte(tt.body), &p); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if p.PollID != "p1" {
				t.Errorf("other fields not decoded: %+v", p)
			}
			if p.PseudoAnonymity != tt.wantValue || p.PseudoAnonymityKnown != tt.wantKnown {
				t.Errorf("PseudoAnonymity=%v Known=%v, want %v/%v", p.PseudoAnonymity, p.PseudoAnonymityKnown, tt.wantValue, tt.wantKnown)
			}
		})
	}

	t.Run("a non-bool value is still a decode error", func(t *testing.T) {
		var p PollResponse
		if err := json.Unmarshal([]byte(`{"pseudo_anonymity":"yes"}`), &p); err == nil {
			t.Error("expected an error, as before")
		}
	})
}
