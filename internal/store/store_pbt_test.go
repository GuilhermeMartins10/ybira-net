package store

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
	"pgregory.net/rapid"

	"github.com/ybira-net/ybira-net/internal/types"
)

var pbtCounter atomic.Int64

// **Validates: Requirements 6.1**
// Property 7: Store round-trip persistence
// For any batch of FlowEvents written to the Store, reading back records with
// matching timestamps produces equivalent data (timestamp, PID, process, bytes all match).
func TestProperty_StoreRoundTripPersistence(t *testing.T) {
	baseDir, err := os.MkdirTemp("", "store_pbt_*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(baseDir)

	rapid.Check(t, func(t *rapid.T) {
		// Setup: create a fresh store for each test case using a unique path
		n := pbtCounter.Add(1)
		dbPath := filepath.Join(baseDir, fmt.Sprintf("pbt_%d.db", n))
		logger, _ := zap.NewDevelopment()
		store := NewSQLiteStore(dbPath, time.Hour, logger)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		if err := store.Start(ctx); err != nil {
			t.Fatalf("Start failed: %v", err)
		}
		defer store.Close()

		// Generate a batch of FlowEvents
		batchSize := rapid.IntRange(1, 100).Draw(t, "batchSize")
		events := make([]types.FlowEvent, batchSize)

		for i := range events {
			// Generate realistic values
			ts := rapid.Int64Range(1000000000, 2000000000).Draw(t, "timestamp")
			pid := rapid.IntRange(1, 65535).Draw(t, "pid")
			process := rapid.StringMatching(`[a-z]{1,15}`).Draw(t, "process")
			bytes := rapid.IntRange(1, 1000000).Draw(t, "bytes")
			protocol := rapid.SampledFrom([]string{"tcp", "udp"}).Draw(t, "protocol")

			// Generate IP addresses as 4-byte slices
			srcIPBytes := make(net.IP, 4)
			for j := range srcIPBytes {
				srcIPBytes[j] = byte(rapid.IntRange(1, 254).Draw(t, "srcIPByte"))
			}
			dstIPBytes := make(net.IP, 4)
			for j := range dstIPBytes {
				dstIPBytes[j] = byte(rapid.IntRange(1, 254).Draw(t, "dstIPByte"))
			}

			events[i] = types.FlowEvent{
				Timestamp: time.Unix(ts, 0),
				PID:       pid,
				Process:   process,
				Bytes:     bytes,
				SrcIP:     srcIPBytes,
				DstIP:     dstIPBytes,
				Protocol:  protocol,
			}
		}

		// Write events to store
		if err := store.Write(events); err != nil {
			t.Fatalf("Write failed: %v", err)
		}

		// Flush to persist
		if err := store.Flush(); err != nil {
			t.Fatalf("Flush failed: %v", err)
		}

		// Read back all records
		rows, err := store.db.Query(
			"SELECT timestamp, pid, process, bytes, src_ip, dst_ip, protocol FROM traffic ORDER BY id")
		if err != nil {
			t.Fatalf("query failed: %v", err)
		}
		defer rows.Close()

		var readBack []types.FlowEvent
		for rows.Next() {
			var ts int64
			var pid, bytes int
			var process, srcIP, dstIP, protocol string
			if err := rows.Scan(&ts, &pid, &process, &bytes, &srcIP, &dstIP, &protocol); err != nil {
				t.Fatalf("scan failed: %v", err)
			}
			readBack = append(readBack, types.FlowEvent{
				Timestamp: time.Unix(ts, 0),
				PID:       pid,
				Process:   process,
				Bytes:     bytes,
				SrcIP:     net.ParseIP(srcIP),
				DstIP:     net.ParseIP(dstIP),
				Protocol:  protocol,
			})
		}

		// Verify: same number of records
		if len(readBack) != len(events) {
			t.Fatalf("expected %d records, got %d", len(events), len(readBack))
		}

		// Verify: each record matches (timestamp, PID, process, bytes)
		for i, ev := range events {
			rb := readBack[i]

			if rb.Timestamp.Unix() != ev.Timestamp.Unix() {
				t.Fatalf("record %d: timestamp mismatch: want %d, got %d",
					i, ev.Timestamp.Unix(), rb.Timestamp.Unix())
			}
			if rb.PID != ev.PID {
				t.Fatalf("record %d: PID mismatch: want %d, got %d", i, ev.PID, rb.PID)
			}
			if rb.Process != ev.Process {
				t.Fatalf("record %d: process mismatch: want %q, got %q", i, ev.Process, rb.Process)
			}
			if rb.Bytes != ev.Bytes {
				t.Fatalf("record %d: bytes mismatch: want %d, got %d", i, ev.Bytes, rb.Bytes)
			}
			if rb.Protocol != ev.Protocol {
				t.Fatalf("record %d: protocol mismatch: want %q, got %q", i, ev.Protocol, rb.Protocol)
			}
			// Also verify IPs round-trip correctly
			if !rb.SrcIP.Equal(ev.SrcIP) {
				t.Fatalf("record %d: src_ip mismatch: want %s, got %s", i, ev.SrcIP, rb.SrcIP)
			}
			if !rb.DstIP.Equal(ev.DstIP) {
				t.Fatalf("record %d: dst_ip mismatch: want %s, got %s", i, ev.DstIP, rb.DstIP)
			}
		}
	})
}
