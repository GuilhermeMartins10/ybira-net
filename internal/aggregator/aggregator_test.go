package aggregator

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ybira-net/ybira-net/internal/types"
)

func TestNewWindow(t *testing.T) {
	w := NewWindow(60)
	if w.size != 60 {
		t.Errorf("expected size 60, got %d", w.size)
	}
	if len(w.buckets) != 60 {
		t.Errorf("expected 60 buckets, got %d", len(w.buckets))
	}
	if w.head != 0 {
		t.Errorf("expected head 0, got %d", w.head)
	}
}

func TestNewAggregator(t *testing.T) {
	agg := New()
	if len(agg.windows) != 3 {
		t.Fatalf("expected 3 windows, got %d", len(agg.windows))
	}
	for _, size := range []int{60, 300, 3600} {
		if _, ok := agg.windows[size]; !ok {
			t.Errorf("missing window of size %d", size)
		}
	}
}

func TestIngestAndQuery(t *testing.T) {
	agg := New()
	// Manually set the clock so ingest works.
	agg.currentSecond.Store(time.Now().Unix())

	events := []types.FlowEvent{
		{PID: 1, Bytes: 100},
		{PID: 1, Bytes: 200},
		{PID: 2, Bytes: 50},
	}

	for _, ev := range events {
		agg.ingest(ev)
	}

	stats := agg.Query(60)
	if len(stats) != 2 {
		t.Fatalf("expected 2 stats, got %d", len(stats))
	}

	// PID 1 should be first (300 bytes > 50 bytes).
	if stats[0].PID != 1 || stats[0].TotalBytes != 300 {
		t.Errorf("expected PID 1 with 300 bytes, got PID %d with %d bytes", stats[0].PID, stats[0].TotalBytes)
	}
	if stats[1].PID != 2 || stats[1].TotalBytes != 50 {
		t.Errorf("expected PID 2 with 50 bytes, got PID %d with %d bytes", stats[1].PID, stats[1].TotalBytes)
	}
}

func TestTopN(t *testing.T) {
	agg := New()
	agg.currentSecond.Store(time.Now().Unix())

	// Ingest events for 5 different PIDs.
	for pid := 1; pid <= 5; pid++ {
		agg.ingest(types.FlowEvent{PID: pid, Bytes: pid * 100})
	}

	// TopN with n=3 should return top 3.
	stats := agg.TopN(60, 3)
	if len(stats) != 3 {
		t.Fatalf("expected 3 stats, got %d", len(stats))
	}
	if stats[0].PID != 5 {
		t.Errorf("expected top PID 5, got %d", stats[0].PID)
	}

	// TopN with n > total PIDs returns all.
	stats = agg.TopN(60, 10)
	if len(stats) != 5 {
		t.Fatalf("expected 5 stats, got %d", len(stats))
	}
}

func TestQueryInvalidWindow(t *testing.T) {
	agg := New()
	stats := agg.Query(999)
	if stats != nil {
		t.Errorf("expected nil for invalid window, got %v", stats)
	}
}

func TestBucketAdvancement(t *testing.T) {
	agg := New()
	now := time.Now().Unix()
	agg.currentSecond.Store(now)

	// Ingest at time T.
	agg.ingest(types.FlowEvent{PID: 1, Bytes: 100})

	// Advance clock by 2 seconds.
	agg.currentSecond.Store(now + 2)
	agg.ingest(types.FlowEvent{PID: 1, Bytes: 50})

	stats := agg.Query(60)
	if len(stats) != 1 {
		t.Fatalf("expected 1 stat, got %d", len(stats))
	}
	if stats[0].TotalBytes != 150 {
		t.Errorf("expected 150 bytes, got %d", stats[0].TotalBytes)
	}
}

func TestWindowExpiry(t *testing.T) {
	// Use a small window for testing.
	w := NewWindow(5)
	agg := &Aggregator{
		windows: map[int]*Window{5: w},
	}

	now := time.Now().Unix()
	agg.currentSecond.Store(now)

	// Ingest at time T.
	agg.ingest(types.FlowEvent{PID: 1, Bytes: 100})

	// Advance past the window (5 seconds).
	agg.currentSecond.Store(now + 5)
	agg.ingest(types.FlowEvent{PID: 2, Bytes: 50})

	stats := agg.Query(5)
	// PID 1's data should be expired (zeroed when head advanced over it).
	found := false
	for _, s := range stats {
		if s.PID == 1 {
			found = true
			t.Errorf("PID 1 should have been expired, but found with %d bytes", s.TotalBytes)
		}
	}
	if !found {
		// Good: PID 1 was expired.
	}
	// PID 2 should still be present.
	foundPID2 := false
	for _, s := range stats {
		if s.PID == 2 && s.TotalBytes == 50 {
			foundPID2 = true
		}
	}
	if !foundPID2 {
		t.Error("expected PID 2 with 50 bytes")
	}
}

func TestRunConsumerLoop(t *testing.T) {
	agg := New()
	agg.currentSecond.Store(time.Now().Unix())

	ch := make(chan types.FlowEvent, 10)
	ctx, cancel := context.WithCancel(context.Background())

	// Send some events.
	ch <- types.FlowEvent{PID: 1, Bytes: 100}
	ch <- types.FlowEvent{PID: 2, Bytes: 200}
	ch <- types.FlowEvent{PID: 3, Bytes: 300}

	// Start consumer in background.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		agg.Run(ctx, ch)
	}()

	// Give consumer time to process.
	time.Sleep(50 * time.Millisecond)
	cancel()
	wg.Wait()

	stats := agg.Query(60)
	if len(stats) != 3 {
		t.Fatalf("expected 3 stats, got %d", len(stats))
	}
}

func TestBatchDrain(t *testing.T) {
	agg := New()
	agg.currentSecond.Store(time.Now().Unix())

	ch := make(chan types.FlowEvent, 300)
	// Fill channel with 256+ events.
	for i := 0; i < 260; i++ {
		ch <- types.FlowEvent{PID: 1, Bytes: 1}
	}

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		agg.Run(ctx, ch)
	}()

	// Give consumer time to drain.
	time.Sleep(50 * time.Millisecond)
	cancel()
	wg.Wait()

	stats := agg.Query(60)
	if len(stats) != 1 {
		t.Fatalf("expected 1 stat, got %d", len(stats))
	}
	if stats[0].TotalBytes != 260 {
		t.Errorf("expected 260 bytes, got %d", stats[0].TotalBytes)
	}
}

func TestConcurrentReadWrite(t *testing.T) {
	agg := New()
	agg.currentSecond.Store(time.Now().Unix())

	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan types.FlowEvent, 1024)

	// Start consumer.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		agg.Run(ctx, ch)
	}()

	// Concurrent writer.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			ch <- types.FlowEvent{PID: i % 10, Bytes: 100}
		}
	}()

	// Concurrent readers.
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = agg.Query(60)
				time.Sleep(time.Millisecond)
			}
		}()
	}

	// Wait for writer to finish, then stop.
	time.Sleep(200 * time.Millisecond)
	cancel()
	wg.Wait()
}

func TestStartClock(t *testing.T) {
	agg := New()
	ctx, cancel := context.WithCancel(context.Background())

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		agg.StartClock(ctx)
	}()

	// Give clock time to start.
	time.Sleep(50 * time.Millisecond)

	sec := agg.currentSecond.Load()
	now := time.Now().Unix()
	if sec < now-1 || sec > now+1 {
		t.Errorf("clock value %d not close to current time %d", sec, now)
	}

	cancel()
	wg.Wait()
}

func TestQuerySortOrder(t *testing.T) {
	agg := New()
	agg.currentSecond.Store(time.Now().Unix())

	// Ingest in random order.
	agg.ingest(types.FlowEvent{PID: 3, Bytes: 50})
	agg.ingest(types.FlowEvent{PID: 1, Bytes: 300})
	agg.ingest(types.FlowEvent{PID: 2, Bytes: 150})

	stats := agg.Query(60)
	for i := 1; i < len(stats); i++ {
		if stats[i].TotalBytes > stats[i-1].TotalBytes {
			t.Errorf("stats not sorted: index %d (%d) > index %d (%d)",
				i, stats[i].TotalBytes, i-1, stats[i-1].TotalBytes)
		}
	}
}
