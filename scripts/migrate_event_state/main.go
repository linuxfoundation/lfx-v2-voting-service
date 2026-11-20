// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"flag"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/infrastructure/eventstate"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"log/slog"
	"os"
	"time"
)

func run() error {
	apply := flag.Bool("apply", false, "apply schema, import and verify; all legacy mappings writers must already be stopped")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()
	s, err := eventstate.Open(ctx, os.Getenv("VOTING_STATE_DATABASE_URL"))
	if err != nil {
		return err
	}
	defer s.Close()
	nc, err := nats.Connect(os.Getenv("NATS_URL"), nats.Timeout(10*time.Second))
	if err != nil {
		return err
	}
	defer nc.Close()
	js, err := jetstream.New(nc)
	if err != nil {
		return err
	}
	n, err := s.Import(ctx, js, *apply)
	if err == nil {
		slog.Info("voting state migration verified", "entries", n, "applied", *apply)
	}
	return err
}
func main() {
	if err := run(); err != nil {
		slog.Error("voting state migration failed; verify cutover state before retrying", "failure_category", eventstate.MigrationFailureCategory(err))
		os.Exit(1)
	}
}
