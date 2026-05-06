package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"go.uber.org/zap"
	"pgregory.net/rapid"

	"github.com/ybira-net/ybira-net/internal/config"
	"github.com/ybira-net/ybira-net/internal/types"
)

// **Validates: Requirements 4.2**
// Property: For any valid /stats response, the stats array is sorted by bytes
// in strictly non-increasing order, and its length is at most the requested "top" parameter value.
func TestProperty_StatsResponseSortedAndBounded(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a random number of stats (0 to 50)
		numStats := rapid.IntRange(0, 50).Draw(t, "numStats")

		// Generate stats with unique PIDs and random byte counts
		stats := make([]types.AggregatedStat, numStats)
		usedPIDs := make(map[int]bool)
		for i := 0; i < numStats; i++ {
			pid := rapid.IntRange(1, 100000).Draw(t, "pid")
			for usedPIDs[pid] {
				pid = rapid.IntRange(1, 100000).Draw(t, "pid")
			}
			usedPIDs[pid] = true

			stats[i] = types.AggregatedStat{
				PID:        pid,
				Process:    rapid.StringMatching(`[a-z]{3,10}`).Draw(t, "process"),
				TotalBytes: rapid.Int64Range(1, 10_000_000).Draw(t, "bytes"),
				Window:     60,
			}
		}

		// Sort stats descending by bytes (simulating what the aggregator does)
		sortedStats := make([]types.AggregatedStat, len(stats))
		copy(sortedStats, stats)
		for i := 0; i < len(sortedStats); i++ {
			for j := i + 1; j < len(sortedStats); j++ {
				if sortedStats[j].TotalBytes > sortedStats[i].TotalBytes {
					sortedStats[i], sortedStats[j] = sortedStats[j], sortedStats[i]
				}
			}
		}

		querier := &mockQuerier{stats: sortedStats}

		// Generate a valid top parameter (1 to 50)
		top := rapid.IntRange(1, 50).Draw(t, "top")

		// Generate a valid window
		windows := []int{60, 300, 3600}
		window := windows[rapid.IntRange(0, 2).Draw(t, "windowIdx")]

		// Create server and make request
		logger, _ := zap.NewDevelopment()
		srv := NewServer(querier, zeroDrops(), logger, config.APIConfig{Listen: ":0"})

		url := "/stats?window=" + strconv.Itoa(window) + "&top=" + strconv.Itoa(top)
		req := httptest.NewRequest(http.MethodGet, url, nil)
		w := httptest.NewRecorder()

		srv.handleStats(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}

		var resp statsResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		// Property 1: length <= top
		if len(resp.Stats) > top {
			t.Fatalf("stats length %d exceeds top %d", len(resp.Stats), top)
		}

		// Property 2: sorted by bytes in non-increasing order
		for i := 1; i < len(resp.Stats); i++ {
			if resp.Stats[i].Bytes > resp.Stats[i-1].Bytes {
				t.Fatalf("stats not sorted: index %d has bytes %d > index %d has bytes %d",
					i, resp.Stats[i].Bytes, i-1, resp.Stats[i-1].Bytes)
			}
		}
	})
}
