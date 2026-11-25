// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package middleware

import (
	"bufio"
	"encoding/json"
	"log/slog"
	"os"
	"testing"

	"github.com/linuxfoundation/lfx-v2-voting-service/internal/logging"
	"github.com/stretchr/testify/require"
)

// captureLogs installs the service's real structured logger (including the
// context-attribute handler) writing to a temp file, and returns a function that
// reads back every JSON log entry emitted so far. The previous default logger and
// stdout are restored when the test ends. Tests using it must not run in parallel.
func captureLogs(t *testing.T) func() []map[string]any {
	t.Helper()

	f, err := os.CreateTemp(t.TempDir(), "logs-*.jsonl")
	require.NoError(t, err)

	origStdout := os.Stdout
	origLogger := slog.Default()
	t.Setenv("LOG_LEVEL", "debug")
	os.Stdout = f
	logging.InitStructureLogConfig() // handler binds to the swapped os.Stdout
	os.Stdout = origStdout
	t.Cleanup(func() {
		slog.SetDefault(origLogger)
		_ = f.Close()
	})

	return func() []map[string]any {
		t.Helper()
		require.NoError(t, f.Sync())
		rf, err := os.Open(f.Name())
		require.NoError(t, err)
		defer func() { _ = rf.Close() }()

		var entries []map[string]any
		sc := bufio.NewScanner(rf)
		for sc.Scan() {
			var m map[string]any
			require.NoError(t, json.Unmarshal(sc.Bytes(), &m))
			entries = append(entries, m)
		}
		require.NoError(t, sc.Err())
		return entries
	}
}
