#!/bin/bash
# Copyright The Linux Foundation and each contributor to LFX.
# SPDX-License-Identifier: MIT

# Script to delete all vote and vote_response documents from OpenSearch
# This is a temporary utility script for cleaning up test/migration data

set -e

# Configuration
OPENSEARCH_URL="${OPENSEARCH_URL:-http://opensearch-cluster-master.lfx.svc.cluster.local:9200}"
INDEX_NAME="${INDEX_NAME:-resources}"

echo "================================================"
echo "Vote & Vote Response Cleanup Script"
echo "================================================"
echo "OpenSearch URL: $OPENSEARCH_URL"
echo "OpenSearch Index: $INDEX_NAME"
echo ""
echo "This will delete ALL OpenSearch documents with type:"
echo "  - vote"
echo "  - vote_response"
echo ""
read -p "Are you sure you want to proceed? (yes/no): " CONFIRM

if [ "$CONFIRM" != "yes" ]; then
    echo "Aborted."
    exit 0
fi

echo ""
echo "Step 1: Counting vote documents..."
VOTE_COUNT=$(curl -s -X GET "${OPENSEARCH_URL}/${INDEX_NAME}/_count" \
    -H 'Content-Type: application/json' \
    -d '{
        "query": {
            "term": {
                "object_type": "vote"
            }
        }
    }' | jq -r '.count')

echo "Found $VOTE_COUNT vote documents"

echo ""
echo "Step 2: Counting vote_response documents..."
RESPONSE_COUNT=$(curl -s -X GET "${OPENSEARCH_URL}/${INDEX_NAME}/_count" \
    -H 'Content-Type: application/json' \
    -d '{
        "query": {
            "term": {
                "object_type": "vote_response"
            }
        }
    }' | jq -r '.count')

echo "Found $RESPONSE_COUNT vote_response documents"

TOTAL_COUNT=$((VOTE_COUNT + RESPONSE_COUNT))
echo ""
echo "Total documents to delete: $TOTAL_COUNT"

if [ "$TOTAL_COUNT" -eq 0 ]; then
    echo "No documents to delete. Exiting."
    exit 0
fi

echo ""
read -p "Proceed with deletion? (yes/no): " CONFIRM_DELETE

if [ "$CONFIRM_DELETE" != "yes" ]; then
    echo "Aborted."
    exit 0
fi

# Delete OpenSearch documents for votes
echo ""
echo "Step 3: Deleting OpenSearch vote documents..."
VOTE_RESULT=$(curl -s -X POST "${OPENSEARCH_URL}/${INDEX_NAME}/_delete_by_query?conflicts=proceed" \
    -H 'Content-Type: application/json' \
    -d '{
        "query": {
            "term": {
                "object_type": "vote"
            }
        }
    }')

VOTE_DELETED=$(echo "$VOTE_RESULT" | jq -r '.deleted')
echo "Deleted $VOTE_DELETED vote documents from OpenSearch"

# Delete OpenSearch documents for vote responses
echo ""
echo "Step 4: Deleting OpenSearch vote_response documents..."
RESPONSE_RESULT=$(curl -s -X POST "${OPENSEARCH_URL}/${INDEX_NAME}/_delete_by_query?conflicts=proceed" \
    -H 'Content-Type: application/json' \
    -d '{
        "query": {
            "term": {
                "object_type": "vote_response"
            }
        }
    }')

RESPONSE_DELETED=$(echo "$RESPONSE_RESULT" | jq -r '.deleted')
echo "Deleted $RESPONSE_DELETED vote_response documents from OpenSearch"

TOTAL_DELETED=$((VOTE_DELETED + RESPONSE_DELETED))

echo ""
echo "================================================"
echo "Cleanup Complete"
echo "================================================"
echo "OpenSearch documents deleted: $TOTAL_DELETED"
echo ""
echo "Waiting 5 seconds for OpenSearch to process deletions..."
sleep 5

# Verify OpenSearch cleanup
echo ""
echo "Step 5: Verifying OpenSearch cleanup..."

REMAINING_VOTE=$(curl -s -X GET "${OPENSEARCH_URL}/${INDEX_NAME}/_count" \
    -H 'Content-Type: application/json' \
    -d '{
        "query": {
            "term": {
                "object_type": "vote"
            }
        }
    }' | jq -r '.count')

REMAINING_RESPONSE=$(curl -s -X GET "${OPENSEARCH_URL}/${INDEX_NAME}/_count" \
    -H 'Content-Type: application/json' \
    -d '{
        "query": {
            "term": {
                "object_type": "vote_response"
            }
        }
    }' | jq -r '.count')

TOTAL_REMAINING=$((REMAINING_VOTE + REMAINING_RESPONSE))

echo "Remaining vote documents: $REMAINING_VOTE"
echo "Remaining vote_response documents: $REMAINING_RESPONSE"
echo "Total remaining: $TOTAL_REMAINING"

echo ""
if [ "$TOTAL_REMAINING" -eq 0 ]; then
    echo "✓ All OpenSearch documents successfully removed!"
else
    echo "⚠ Warning: $TOTAL_REMAINING documents still remain."
fi
