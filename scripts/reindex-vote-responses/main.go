// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

// Command reindex-vote-responses reindexes vote responses. It asks OpenSearch which vote_response
// documents are indexed, then re-puts each one's itx-poll-vote record in the v1-objects KV bucket,
// so the voting consumer reprocesses it through the live handler (indexer message, vote_response
// owner tuple, vote participant tuple). Same approach as reindex_meetings in lfx-v2-meeting-service.
//
// It is a dry run unless -apply is passed:
//
//	NATS_URL=nats://localhost:4222 OPENSEARCH_URL=http://localhost:9200 reindex-vote-responses                # report only
//	NATS_URL=nats://localhost:4222 OPENSEARCH_URL=http://localhost:9200 reindex-vote-responses -vote-uid <u>  # one vote, report only
//	NATS_URL=nats://localhost:4222 OPENSEARCH_URL=http://localhost:9200 reindex-vote-responses -apply         # replay everything
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/linuxfoundation/lfx-v2-voting-service/cmd/voting-api/eventing"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	natsURL := flag.String("nats-url", getEnv("NATS_URL", "nats://127.0.0.1:4222"), "NATS server URL (env NATS_URL)")
	osURL := flag.String("opensearch-url", getEnv("OPENSEARCH_URL", "http://localhost:9200"), "OpenSearch base URL (env OPENSEARCH_URL)")
	apply := flag.Bool("apply", false, "re-put the records; without it nothing is written")
	voteUID := flag.String("vote-uid", "", "limit the replay to one vote's responses (v2 UID)")
	rate := flag.Int("rate", eventing.DefaultReindexRatePerSecond, "max records re-put per second")
	list := flag.Bool("list", false, "print the key of every record that would be replayed")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	conn, err := nats.Connect(*natsURL)
	if err != nil {
		return fmt.Errorf("failed to connect to NATS: %w", err)
	}
	defer conn.Close()

	js, err := jetstream.New(conn)
	if err != nil {
		return fmt.Errorf("failed to create JetStream context: %w", err)
	}

	result, err := eventing.ReplayVoteResponses(ctx, js, eventing.ReplayVoteResponsesOptions{
		OpenSearchURL: *osURL,
		Apply:         *apply,
		VoteUID:       *voteUID,
		RatePerSecond: *rate,
	}, logger)

	if result != nil {
		if *list {
			for _, k := range result.Keys {
				fmt.Println(k)
			}
		}
		mode := "DRY RUN (pass -apply to write)"
		if *apply {
			mode = "APPLIED"
		}
		fmt.Printf("\n%s\n", mode)
		fmt.Printf("  indexed responses scanned:     %d\n", result.Scanned)
		fmt.Printf("  duplicate object_id:           %d\n", result.Duplicates)
		fmt.Printf("  skipped, no object_id:         %d\n", result.SkippedNoID)
		fmt.Printf("  indexed but not in v1-objects: %d\n", result.NotFound)
		fmt.Printf("  skipped, soft-deleted:         %d\n", result.SkippedDeleted)
		fmt.Printf("  skipped, other vote:           %d\n", result.SkippedOther)
		fmt.Printf("  skipped, never processed:      %d\n", result.SkippedNoMap)
		fmt.Printf("  skipped, response tombstoned:  %d\n", result.SkippedTomb)
		fmt.Printf("  skipped, parent not mapped:    %d\n", result.SkippedNoParent)
		fmt.Printf("  records to replay:             %d\n", result.Eligible)
		if *apply {
			fmt.Printf("  re-put:                        %d\n", result.Replayed)
			fmt.Printf("  conflicts (changed upstream):  %d\n", result.Conflicts)
			fmt.Printf("  failed:                        %d\n", result.Failed)
		}
	}
	return err
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
