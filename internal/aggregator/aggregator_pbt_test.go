package aggregator

import (
	"testing"
	"time"

	"github.com/ybira-net/ybira-net/internal/types"
	"pgregory.net/rapid"
)

// **Validates: Requirements 3.1, 3.3**
// Property: For any sequence of FlowEvents within a window, the sum of bytes per PID
// in the query result equals the sum of input bytes for that PID (byte conservation).
func TestProperty_AggregatorByteConservation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		agg := New()
		now := time.Now().Unix()
		agg.currentSecond.Store(now)

		// Generate a sequence of FlowEvents with random PIDs and byte counts.
		numEvents := rapid.IntRange(1, 100).Draw(t, "numEvents")
		// Track expected totals per PID.
		expected := make(map[int]int64)

		for i := 0; i < numEvents; i++ {
			pid := rapid.IntRange(1, 20).Draw(t, "pid")
			bytes := rapid.IntRange(1, 10000).Draw(t, "bytes")

			ev := types.FlowEvent{
				PID:   pid,
				Bytes: bytes,
			}
			agg.ingest(ev)
			expected[pid] += int64(bytes)
		}

		// Query the 60-second window and verify byte conservation.
		stats := agg.Query(60)

		// Build a map from query results.
		actual := make(map[int]int64)
		for _, s := range stats {
			actual[s.PID] = s.TotalBytes
		}

		// Verify every expected PID has the correct total.
		for pid, expectedBytes := range expected {
			if actual[pid] != expectedBytes {
				t.Fatalf("byte conservation violated for PID %d: expected %d, got %d",
					pid, expectedBytes, actual[pid])
			}
		}

		// Verify no extra PIDs in results.
		for _, s := range stats {
			if _, ok := expected[s.PID]; !ok {
				t.Fatalf("unexpected PID %d in query results", s.PID)
			}
		}
	})
}

// **Validates: Requirements 3.6**
// Property: TopN always returns results in non-increasing order by TotalBytes,
// with length <= N.
func TestProperty_AggregatorTopNOrdering(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		agg := New()
		now := time.Now().Unix()
		agg.currentSecond.Store(now)

		// Generate a sequence of FlowEvents.
		numEvents := rapid.IntRange(1, 200).Draw(t, "numEvents")
		for i := 0; i < numEvents; i++ {
			pid := rapid.IntRange(1, 50).Draw(t, "pid")
			bytes := rapid.IntRange(1, 10000).Draw(t, "bytes")
			agg.ingest(types.FlowEvent{PID: pid, Bytes: bytes})
		}

		// Pick a random N and window.
		n := rapid.IntRange(1, 30).Draw(t, "n")
		window := rapid.SampledFrom([]int{60, 300, 3600}).Draw(t, "window")

		results := agg.TopN(window, n)

		// Property 1: length <= N.
		if len(results) > n {
			t.Fatalf("TopN returned %d results, expected <= %d", len(results), n)
		}

		// Property 2: non-increasing order by TotalBytes.
		for i := 1; i < len(results); i++ {
			if results[i].TotalBytes > results[i-1].TotalBytes {
				t.Fatalf("TopN not sorted: index %d (%d bytes) > index %d (%d bytes)",
					i, results[i].TotalBytes, i-1, results[i-1].TotalBytes)
			}
		}
	})
}
