package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"
)

type statsResponse struct {
	Window int          `json:"window"`
	Stats  []statEntry  `json:"stats"`
	Meta   responseMeta `json:"meta"`
}

type statEntry struct {
	PID     int    `json:"pid"`
	Process string `json:"process"`
	Bytes   int64  `json:"bytes"`
}

type responseMeta struct {
	CaptureDrops    int64  `json:"capture_drops"`
	MapperDrops     int64  `json:"mapper_drops"`
	AggregatorDrops int64  `json:"aggregator_drops"`
	StoreDrops      int64  `json:"store_drops"`
	Timestamp       string `json:"timestamp"`
}

type errorResponse struct {
	Error string `json:"error"`
}

var validWindows = map[int]bool{
	60:   true,
	300:  true,
	3600: true,
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	windowStr := r.URL.Query().Get("window")
	window := 60
	if windowStr != "" {
		var err error
		window, err = strconv.Atoi(windowStr)
		if err != nil || !validWindows[window] {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(errorResponse{
				Error: "invalid window, must be 60, 300, or 3600",
			})
			return
		}
	}

	topStr := r.URL.Query().Get("top")
	top := 10
	if topStr != "" {
		var err error
		top, err = strconv.Atoi(topStr)
		if err != nil || top < 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(errorResponse{
				Error: "invalid top parameter, must be a positive integer",
			})
			return
		}
	}

	stats := s.querier.TopN(window, top)

	entries := make([]statEntry, 0, len(stats))
	for _, st := range stats {
		entries = append(entries, statEntry{
			PID:     st.PID,
			Process: st.Process,
			Bytes:   st.TotalBytes,
		})
	}

	resp := statsResponse{
		Window: window,
		Stats:  entries,
		Meta: responseMeta{
			CaptureDrops:    s.drops.CaptureDrops(),
			MapperDrops:     s.drops.MapperDrops(),
			AggregatorDrops: s.drops.AggregatorDrops(),
			StoreDrops:      s.drops.StoreDrops(),
			Timestamp:       time.Now().UTC().Format(time.RFC3339),
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
