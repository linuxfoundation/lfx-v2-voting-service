// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package eventing

// indexing_contract_test.go — living documentation tests for IndexingConfig values.
//
// These tests assert that the access-control fields produced by the three
// buildXxxIndexingConfig helpers exactly match the authoritative values in
// docs/indexer-contract.md.  If the implementation drifts from the contract
// (e.g. "viewer" becomes "member", or the object-type prefix changes from
// "vote:" to "poll:") this file fails immediately and names the discrepancy.
//
// Update docs/indexer-contract.md in the same PR as any change to these tests.

import (
	"fmt"
	"reflect"
	"sort"
	"testing"

	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
)

// ---- Vote (Poll) ------------------------------------------------------------

func TestBuildVoteIndexingConfig_Contract(t *testing.T) {
	voteUID := "vote-abc-123"
	data := &domain.VoteData{
		VoteUID:      voteUID,
		Name:         "Test Vote",
		Description:  "A description",
		ProjectUID:   "proj-111",
		CommitteeUID: "comm-222",
	}

	cfg := buildVoteIndexingConfig(data)

	// docs/indexer-contract.md § Vote (Poll) — Access Control
	assertField(t, "AccessCheckObject", cfg.AccessCheckObject, fmt.Sprintf("vote:%s", voteUID))
	assertField(t, "AccessCheckRelation", cfg.AccessCheckRelation, "viewer")
	assertField(t, "HistoryCheckObject", cfg.HistoryCheckObject, fmt.Sprintf("vote:%s", voteUID))
	assertField(t, "HistoryCheckRelation", cfg.HistoryCheckRelation, "auditor")
	assertField(t, "ObjectID", cfg.ObjectID, voteUID)

	// Search behaviour
	assertField(t, "SortName", cfg.SortName, data.Name)
	assertField(t, "Fulltext", cfg.Fulltext, fmt.Sprintf("%s %s", data.Name, data.Description))

	// Tags — exactly these two entries, no more
	assertExactSlice(t, "Tags", cfg.Tags, []string{
		fmt.Sprintf("committee_uid:%s", data.CommitteeUID),
		fmt.Sprintf("project_uid:%s", data.ProjectUID),
	})

	// Parent refs — exactly these two entries, no more
	assertExactSlice(t, "ParentRefs", cfg.ParentRefs, []string{
		fmt.Sprintf("project:%s", data.ProjectUID),
		fmt.Sprintf("committee:%s", data.CommitteeUID),
	})
}

func TestBuildVoteIndexingConfig_EmptyOptionalFields(t *testing.T) {
	data := &domain.VoteData{VoteUID: "v1"}

	cfg := buildVoteIndexingConfig(data)

	if len(cfg.Tags) != 0 {
		t.Errorf("expected no tags when ProjectUID and CommitteeUID are empty, got %v", cfg.Tags)
	}
	if len(cfg.ParentRefs) != 0 {
		t.Errorf("expected no parent refs when UIDs are empty, got %v", cfg.ParentRefs)
	}
	if len(cfg.NameAndAliases) != 0 {
		t.Errorf("expected no name aliases when Name is empty, got %v", cfg.NameAndAliases)
	}
}

// ---- Vote Response ----------------------------------------------------------

func TestBuildVoteResponseIndexingConfig_Contract(t *testing.T) {
	uid := "vr-abc-123"
	voteUID := "vote-parent-456"
	data := &domain.VoteResponseData{
		UID:        uid,
		VoteUID:    voteUID,
		Username:   "alice",
		ProjectUID: "proj-111",
	}

	cfg := buildVoteResponseIndexingConfig(data)

	// docs/indexer-contract.md § Vote Response — Access Control
	// Note: access is scoped to the parent vote, not the response itself.
	assertField(t, "AccessCheckObject", cfg.AccessCheckObject, fmt.Sprintf("vote:%s", voteUID))
	assertField(t, "AccessCheckRelation", cfg.AccessCheckRelation, "viewer")
	assertField(t, "HistoryCheckObject", cfg.HistoryCheckObject, fmt.Sprintf("vote_response:%s", uid))
	assertField(t, "HistoryCheckRelation", cfg.HistoryCheckRelation, "auditor")
	assertField(t, "ObjectID", cfg.ObjectID, uid)

	// Search behaviour
	assertField(t, "SortName", cfg.SortName, data.Username)
	assertField(t, "Fulltext", cfg.Fulltext, data.Username)

	// Tags — exactly these two entries, no more
	assertExactSlice(t, "Tags", cfg.Tags, []string{
		fmt.Sprintf("vote_uid:%s", voteUID),
		fmt.Sprintf("project_uid:%s", data.ProjectUID),
	})

	// Parent refs — exactly these two entries, no more
	assertExactSlice(t, "ParentRefs", cfg.ParentRefs, []string{
		fmt.Sprintf("project:%s", data.ProjectUID),
		fmt.Sprintf("vote:%s", voteUID),
	})
}

// ---- Vote Result ------------------------------------------------------------

func TestBuildVoteResultIndexingConfig_Contract(t *testing.T) {
	voteUID := "vote-result-123"
	data := &domain.PollResultData{
		VoteUID:      voteUID,
		ProjectUID:   "proj-111",
		CommitteeUID: "comm-222",
	}

	cfg := buildVoteResultIndexingConfig(data)

	// docs/indexer-contract.md § Vote Result — Access Control
	assertField(t, "AccessCheckObject", cfg.AccessCheckObject, fmt.Sprintf("vote:%s", voteUID))
	assertField(t, "AccessCheckRelation", cfg.AccessCheckRelation, "results_viewer")
	assertField(t, "HistoryCheckObject", cfg.HistoryCheckObject, fmt.Sprintf("vote:%s", voteUID))
	assertField(t, "HistoryCheckRelation", cfg.HistoryCheckRelation, "auditor")
	assertField(t, "ObjectID", cfg.ObjectID, voteUID)

	// Tags — exactly these three entries, no more
	assertExactSlice(t, "Tags", cfg.Tags, []string{
		fmt.Sprintf("vote_uid:%s", voteUID),
		fmt.Sprintf("committee_uid:%s", data.CommitteeUID),
		fmt.Sprintf("project_uid:%s", data.ProjectUID),
	})

	// Parent refs — exactly these three entries, no more
	assertExactSlice(t, "ParentRefs", cfg.ParentRefs, []string{
		fmt.Sprintf("vote:%s", voteUID),
		fmt.Sprintf("project:%s", data.ProjectUID),
		fmt.Sprintf("committee:%s", data.CommitteeUID),
	})
}

func TestBuildVoteResultIndexingConfig_AlwaysEmitsVoteUID(t *testing.T) {
	voteUID := "v1"
	data := &domain.PollResultData{VoteUID: voteUID}

	cfg := buildVoteResultIndexingConfig(data)

	// vote_uid tag and vote parent ref must always be emitted regardless of
	// other optional fields — the indexer requires them to relate results back
	// to the parent vote.
	assertContains(t, "Tags", cfg.Tags, fmt.Sprintf("vote_uid:%s", voteUID))
	assertContains(t, "ParentRefs", cfg.ParentRefs, fmt.Sprintf("vote:%s", voteUID))
}

// ---- helpers ----------------------------------------------------------------

func assertField(t *testing.T, field, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("IndexingConfig.%s: got %q, want %q (check docs/indexer-contract.md)", field, got, want)
	}
}

func assertContains(t *testing.T, field string, slice []string, want string) {
	t.Helper()
	for _, v := range slice {
		if v == want {
			return
		}
	}
	t.Errorf("IndexingConfig.%s does not contain %q (got %v); check docs/indexer-contract.md", field, want, slice)
}

// assertExactSlice verifies that got and want contain exactly the same elements
// (order-independent). An extra or missing element in got fails the test, which
// is stronger than assertContains (subset check).
func assertExactSlice(t *testing.T, field string, got, want []string) {
	t.Helper()
	gotCopy := append([]string(nil), got...)
	wantCopy := append([]string(nil), want...)
	sort.Strings(gotCopy)
	sort.Strings(wantCopy)
	if !reflect.DeepEqual(gotCopy, wantCopy) {
		t.Errorf("IndexingConfig.%s mismatch (check docs/indexer-contract.md):\n  got:  %v\n  want: %v", field, got, want)
	}
}
