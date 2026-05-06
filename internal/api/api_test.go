package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"

	"github.com/ybira-net/ybira-net/internal/config"
	"github.com/ybira-net/ybira-net/internal/types"
)

// mockQuerier implements StatsQuerier for testing.
type mockQuerier struct {
	stats []types.AggregatedStat
}

func (m *mockQuerier) Query(window int) []types.AggregatedStat {
	return m.stats
}

func (m *mockQuerier) TopN(window, n int) []types.AggregatedStat {
	if len(m.stats) <= n {
		return m.stats
	}
	return m.stats[:n]
}

// zeroDrops returns a DropCounters that always returns 0.
func zeroDrops() DropCounters {
	return DropCounters{
		CaptureDrops:    func() int64 { return 0 },
		MapperDrops:     func() int64 { return 0 },
		AggregatorDrops: func() int64 { return 0 },
		StoreDrops:      func() int64 { return 0 },
	}
}

func newTestServer(querier StatsQuerier) *Server {
	logger, _ := zap.NewDevelopment()
	return NewServer(querier, zeroDrops(), logger, config.APIConfig{Listen: ":0"})
}

func TestHandleStats_DefaultParams(t *testing.T) {
	querier := &mockQuerier{
		stats: []types.AggregatedStat{
			{PID: 1, Process: "firefox", TotalBytes: 1000, Window: 60},
			{PID: 2, Process: "curl", TotalBytes: 500, Window: 60},
		},
	}
	srv := newTestServer(querier)

	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	w := httptest.NewRecorder()

	srv.handleStats(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp statsResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Window != 60 {
		t.Errorf("expected window=60, got %d", resp.Window)
	}
	if len(resp.Stats) != 2 {
		t.Errorf("expected 2 stats, got %d", len(resp.Stats))
	}
	if resp.Stats[0].PID != 1 || resp.Stats[0].Bytes != 1000 {
		t.Errorf("unexpected first stat: %+v", resp.Stats[0])
	}
}

func TestHandleStats_WithWindowParam(t *testing.T) {
	querier := &mockQuerier{
		stats: []types.AggregatedStat{
			{PID: 10, Process: "nginx", TotalBytes: 2048, Window: 300},
		},
	}
	srv := newTestServer(querier)

	req := httptest.NewRequest(http.MethodGet, "/stats?window=300", nil)
	w := httptest.NewRecorder()

	srv.handleStats(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp statsResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Window != 300 {
		t.Errorf("expected window=300, got %d", resp.Window)
	}
}

func TestHandleStats_InvalidWindow(t *testing.T) {
	srv := newTestServer(&mockQuerier{})

	tests := []struct {
		name  string
		query string
	}{
		{"non-numeric", "/stats?window=abc"},
		{"invalid value", "/stats?window=120"},
		{"zero", "/stats?window=0"},
		{"negative", "/stats?window=-1"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.query, nil)
			w := httptest.NewRecorder()

			srv.handleStats(w, req)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d", w.Code)
			}

			var errResp errorResponse
			if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
				t.Fatalf("failed to decode error response: %v", err)
			}

			if errResp.Error != "invalid window, must be 60, 300, or 3600" {
				t.Errorf("unexpected error message: %s", errResp.Error)
			}
		})
	}
}

func TestHandleStats_TopParam(t *testing.T) {
	stats := make([]types.AggregatedStat, 20)
	for i := range stats {
		stats[i] = types.AggregatedStat{
			PID:        i + 1,
			Process:    "proc",
			TotalBytes: int64(1000 - i*10),
			Window:     60,
		}
	}
	querier := &mockQuerier{stats: stats}
	srv := newTestServer(querier)

	req := httptest.NewRequest(http.MethodGet, "/stats?top=5", nil)
	w := httptest.NewRecorder()

	srv.handleStats(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp statsResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(resp.Stats) != 5 {
		t.Errorf("expected 5 stats, got %d", len(resp.Stats))
	}
}

func TestHandleStats_InvalidTop(t *testing.T) {
	srv := newTestServer(&mockQuerier{})

	req := httptest.NewRequest(http.MethodGet, "/stats?top=abc", nil)
	w := httptest.NewRecorder()

	srv.handleStats(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleStats_MetaDropCounters(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	drops := DropCounters{
		CaptureDrops:    func() int64 { return 10 },
		MapperDrops:     func() int64 { return 5 },
		AggregatorDrops: func() int64 { return 3 },
		StoreDrops:      func() int64 { return 1 },
	}
	srv := NewServer(&mockQuerier{}, drops, logger, config.APIConfig{Listen: ":0"})

	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	w := httptest.NewRecorder()

	srv.handleStats(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp statsResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Meta.CaptureDrops != 10 {
		t.Errorf("expected capture_drops=10, got %d", resp.Meta.CaptureDrops)
	}
	if resp.Meta.MapperDrops != 5 {
		t.Errorf("expected mapper_drops=5, got %d", resp.Meta.MapperDrops)
	}
	if resp.Meta.AggregatorDrops != 3 {
		t.Errorf("expected aggregator_drops=3, got %d", resp.Meta.AggregatorDrops)
	}
	if resp.Meta.StoreDrops != 1 {
		t.Errorf("expected store_drops=1, got %d", resp.Meta.StoreDrops)
	}
	if resp.Meta.Timestamp == "" {
		t.Error("expected non-empty timestamp")
	}
}

func TestHandleStats_ResponseContentType(t *testing.T) {
	srv := newTestServer(&mockQuerier{})

	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	w := httptest.NewRecorder()

	srv.handleStats(w, req)

	ct := w.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", ct)
	}
}

func TestHandleStats_EmptyStats(t *testing.T) {
	srv := newTestServer(&mockQuerier{stats: nil})

	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	w := httptest.NewRecorder()

	srv.handleStats(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp statsResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Stats == nil {
		t.Error("expected non-nil stats array (empty)")
	}
	if len(resp.Stats) != 0 {
		t.Errorf("expected 0 stats, got %d", len(resp.Stats))
	}
}
