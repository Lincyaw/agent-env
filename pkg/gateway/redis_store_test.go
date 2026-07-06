package gateway

import (
	"encoding/json"
	"testing"
	"time"
)

func TestRedisLegacySessionCompactionDropsMonolithicHistory(t *testing.T) {
	timestamp := time.Unix(1700000000, 0).UTC()
	legacy := redisSessionData{
		Info: SessionInfo{ID: "s1", Status: "active"},
		HistoryRecords: []redisLegacyStepRecord{{
			Index:      7,
			Name:       "execute",
			Input:      json.RawMessage(`{"cmd":"pwd"}`),
			SnapshotID: "7",
			DurationMs: 12,
			Timestamp:  timestamp,
		}},
		HistoryNextIndex: 8,
	}

	if !redisSessionNeedsLegacyCompaction(legacy) {
		t.Fatal("legacy session was not marked for compaction")
	}

	s := redisDataToSession(legacy, redisSessionActionsData{})
	actions := sessionActionsToRedisData(s)
	if len(actions.Records) != 1 {
		t.Fatalf("actions records = %d, want 1", len(actions.Records))
	}
	if actions.NextIndex != 8 {
		t.Fatalf("actions next index = %d, want 8", actions.NextIndex)
	}
	record := actions.Records[0]
	if record.Index != 7 || record.Name != "execute" || record.SnapshotID != "7" || record.DurationMs != 12 {
		t.Fatalf("compacted action record = %#v, want legacy replay metadata", record)
	}
	if string(record.Input) != `{"cmd":"pwd"}` {
		t.Fatalf("compacted action input = %s, want legacy input", record.Input)
	}
	if !record.Timestamp.Equal(timestamp) {
		t.Fatalf("compacted action timestamp = %s, want %s", record.Timestamp, timestamp)
	}
	if record.Output.Stdout != "" || record.Output.Stderr != "" {
		t.Fatalf("compacted action kept observation output: %#v", record.Output)
	}

	compactedMeta := sessionToRedisData(s)
	if len(compactedMeta.HistoryRecords) != 0 || compactedMeta.HistoryNextIndex != 0 {
		t.Fatalf("compacted metadata still has legacy history: %#v", compactedMeta)
	}
	if redisSessionNeedsLegacyCompaction(compactedMeta) {
		t.Fatal("compacted metadata still requires legacy compaction")
	}
}
