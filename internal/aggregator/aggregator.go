package aggregator

import (
	"context"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ybira-net/ybira-net/internal/types"
)

const batchSize = 256

type Window struct {
	mu      sync.RWMutex
	buckets []map[int]int64
	head    int
	size    int
	lastSec int64
}

func NewWindow(size int) *Window {
	buckets := make([]map[int]int64, size)
	for i := range buckets {
		buckets[i] = make(map[int]int64)
	}
	return &Window{
		buckets: buckets,
		head:    0,
		size:    size,
		lastSec: 0,
	}
}

func (w *Window) advance(delta int) {
	if delta <= 0 {
		return
	}
	if delta >= w.size {
		for i := range w.buckets {
			w.buckets[i] = make(map[int]int64)
		}
		w.head = (w.head + delta) % w.size
		return
	}
	for i := 0; i < delta; i++ {
		w.head = (w.head + 1) % w.size
		w.buckets[w.head] = make(map[int]int64)
	}
}

func (w *Window) add(pid int, bytes int64) {
	w.buckets[w.head][pid] += bytes
}

func (w *Window) query() []types.AggregatedStat {
	w.mu.RLock()
	totals := make(map[int]int64)
	for _, bucket := range w.buckets {
		for pid, b := range bucket {
			totals[pid] += b
		}
	}
	w.mu.RUnlock()

	stats := make([]types.AggregatedStat, 0, len(totals))
	for pid, total := range totals {
		if total > 0 {
			stats = append(stats, types.AggregatedStat{
				PID:        pid,
				TotalBytes: total,
				Window:     w.size,
			})
		}
	}

	sort.Slice(stats, func(i, j int) bool {
		return stats[i].TotalBytes > stats[j].TotalBytes
	})

	return stats
}

type Aggregator struct {
	windows       map[int]*Window
	currentSecond atomic.Int64
	drops         atomic.Int64
}

func New() *Aggregator {
	return &Aggregator{
		windows: map[int]*Window{
			60:   NewWindow(60),
			300:  NewWindow(300),
			3600: NewWindow(3600),
		},
	}
}

func (a *Aggregator) StartClock(ctx context.Context) {
	a.currentSecond.Store(time.Now().Unix())
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			a.currentSecond.Store(time.Now().Unix())
		case <-ctx.Done():
			return
		}
	}
}

func (a *Aggregator) Run(ctx context.Context, ch <-chan types.FlowEvent) {
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return
			}
			a.ingest(ev)
			a.drainBatch(ch)
		case <-ctx.Done():
			return
		}
	}
}

func (a *Aggregator) drainBatch(ch <-chan types.FlowEvent) {
	for i := 1; i < batchSize; i++ {
		select {
		case ev, ok := <-ch:
			if !ok {
				return
			}
			a.ingest(ev)
		default:
			return
		}
	}
}

func (a *Aggregator) ingest(ev types.FlowEvent) {
	now := a.currentSecond.Load()

	for _, w := range a.windows {
		w.mu.Lock()
		if w.lastSec == 0 {
			w.lastSec = now
		} else if now > w.lastSec {
			delta := int(now - w.lastSec)
			w.advance(delta)
			w.lastSec = now
		}
		w.add(ev.PID, int64(ev.Bytes))
		w.mu.Unlock()
	}
}

func (a *Aggregator) Query(window int) []types.AggregatedStat {
	w, ok := a.windows[window]
	if !ok {
		return nil
	}
	return w.query()
}

func (a *Aggregator) TopN(window, n int) []types.AggregatedStat {
	stats := a.Query(window)
	if len(stats) <= n {
		return stats
	}
	return stats[:n]
}

func (a *Aggregator) Drops() int64 {
	return a.drops.Load()
}
