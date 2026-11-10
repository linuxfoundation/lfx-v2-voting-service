// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAppendCtx(t *testing.T) {
	t.Run("nil parent is treated as a background context", func(t *testing.T) {
		//nolint:staticcheck // deliberately exercising the nil-parent guard
		ctx := AppendCtx(nil, slog.String("a", "1"))
		require.NotNil(t, ctx)
		attrs, ok := ctx.Value(slogFields).([]slog.Attr)
		require.True(t, ok)
		require.Len(t, attrs, 1)
		assert.Equal(t, "a", attrs[0].Key)
	})

	t.Run("attributes accumulate in order", func(t *testing.T) {
		ctx := AppendCtx(context.Background(), slog.String("a", "1"))
		ctx = AppendCtx(ctx, slog.String("b", "2"))

		attrs := ctx.Value(slogFields).([]slog.Attr)
		require.Len(t, attrs, 2)
		assert.Equal(t, "a", attrs[0].Key)
		assert.Equal(t, "b", attrs[1].Key)
	})

	t.Run("appending to a shared parent does not mutate siblings", func(t *testing.T) {
		parent := AppendCtx(context.Background(), slog.String("base", "x"))
		childA := AppendCtx(parent, slog.String("a", "1"))
		childB := AppendCtx(parent, slog.String("b", "2"))

		assert.Len(t, parent.Value(slogFields).([]slog.Attr), 1)
		a := childA.Value(slogFields).([]slog.Attr)
		b := childB.Value(slogFields).([]slog.Attr)
		assert.Equal(t, "a", a[1].Key)
		assert.Equal(t, "b", b[1].Key)
	})
}

func TestContextHandler_Handle(t *testing.T) {
	newLogger := func(buf *bytes.Buffer) *slog.Logger {
		return slog.New(contextHandler{slog.NewJSONHandler(buf, nil)})
	}

	t.Run("emits attributes stored in the context", func(t *testing.T) {
		var buf bytes.Buffer
		ctx := AppendCtx(context.Background(), slog.String("request_id", "req-1"))
		ctx = AppendCtx(ctx, slog.Int("attempt", 3))

		newLogger(&buf).InfoContext(ctx, "hello")

		var entry map[string]any
		require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))
		assert.Equal(t, "hello", entry["msg"])
		assert.Equal(t, "req-1", entry["request_id"])
		assert.EqualValues(t, 3, entry["attempt"])
	})

	t.Run("plain context logs without extra attributes", func(t *testing.T) {
		var buf bytes.Buffer

		newLogger(&buf).InfoContext(context.Background(), "plain")

		var entry map[string]any
		require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))
		assert.Equal(t, "plain", entry["msg"])
		assert.NotContains(t, entry, "request_id")
	})
}

func TestInitStructureLogConfig_LogLevel(t *testing.T) {
	orig := slog.Default()
	t.Cleanup(func() { slog.SetDefault(orig) })

	cases := map[string]slog.Level{
		"debug":   slog.LevelDebug,
		"info":    slog.LevelInfo,
		"warn":    slog.LevelWarn,
		"error":   slog.LevelError,
		"":        logLevelDefault,
		"unknown": logLevelDefault,
	}
	for env, want := range cases {
		t.Run("LOG_LEVEL="+env, func(t *testing.T) {
			t.Setenv("LOG_LEVEL", env)
			h := InitStructureLogConfig()
			require.NotNil(t, h)
			for _, lvl := range []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError} {
				assert.Equal(t, lvl >= want, h.Enabled(context.Background(), lvl), "level %v with min %v", lvl, want)
			}
		})
	}
}
