// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package idmapper

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
)

func TestNoOpMapper(t *testing.T) {
	ctx := context.Background()
	m := NewNoOpMapper()
	require.NotNil(t, m)

	var _ domain.IDMapper = m

	cases := map[string]func(string) (string, error){
		"MapProjectV2ToV1":   func(id string) (string, error) { return m.MapProjectV2ToV1(ctx, id) },
		"MapProjectV1ToV2":   func(id string) (string, error) { return m.MapProjectV1ToV2(ctx, id) },
		"MapCommitteeV2ToV1": func(id string) (string, error) { return m.MapCommitteeV2ToV1(ctx, id) },
		"MapCommitteeV1ToV2": func(id string) (string, error) { return m.MapCommitteeV1ToV2(ctx, id) },
	}
	for name, call := range cases {
		t.Run(name+" echoes the ID", func(t *testing.T) {
			got, err := call("00000000-0000-0000-0000-000000000001")

			require.NoError(t, err)
			assert.Equal(t, "00000000-0000-0000-0000-000000000001", got)
		})

		t.Run(name+" accepts an empty ID", func(t *testing.T) {
			got, err := call("")

			require.NoError(t, err)
			assert.Empty(t, got)
		})
	}

	t.Run("Close is a no-op", func(t *testing.T) {
		assert.NotPanics(t, m.Close)
	})
}
