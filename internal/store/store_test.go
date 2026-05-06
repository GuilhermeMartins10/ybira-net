package store

import (
	"context"
	"database/sql"
	"net"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/ybira-net/ybira-net/internal/types"
)

func testLogger() *zap.Logger {
	logger, _ := zap.NewDevelopment()
	return logger
}

func tempDBPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "test.db")
}

func makeFlowEvent(ts time.Time, pid int, process string, bytes int) types.FlowEvent {
	return types.FlowEvent{
		Timestamp: ts,
		PID:       pid,
		Process:   process,
		Bytes:     bytes,
		SrcIP:     net.ParseIP("192.168.1.1"),
		DstIP:     net.ParseIP("10.0.0.1"),
		Protocol:  "tcp",
	}
}

func TestSQLiteStore_StartCreatesSchema(t *testing.T) {
	dbPath := tempDBPath(t)
	store := NewSQLiteStore(dbPath, time.Second, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := store.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer store.Close()

	// Verify table exists
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	var tableName string
	err = db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='traffic'").Scan(&tableName)
	if err != nil {
		t.Fatalf("table not found: %v", err)
	}
	if tableName != "traffic" {
		t.Errorf("expected table 'traffic', got %q", tableName)
	}
}

func TestSQLiteStore_StartIdempotent(t *testing.T) {
	dbPath := tempDBPath(t)
	logger := testLogger()

	// Start and close twice to verify idempotent schema creation
	for i := 0; i < 2; i++ {
		store := NewSQLiteStore(dbPath, time.Second, logger)
		ctx, cancel := context.WithCancel(context.Background())

		if err := store.Start(ctx); err != nil {
			t.Fatalf("Start (iteration %d) failed: %v", i, err)
		}
		cancel()
		store.Close()
	}
}

func TestSQLiteStore_WriteAndFlush(t *testing.T) {
	dbPath := tempDBPath(t)
	store := NewSQLiteStore(dbPath, 10*time.Second, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := store.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer store.Close()

	now := time.Now().Truncate(time.Second)
	events := []types.FlowEvent{
		makeFlowEvent(now, 100, "firefox", 1024),
		makeFlowEvent(now.Add(time.Second), 200, "curl", 512),
	}

	if err := store.Write(events); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	if err := store.Flush(); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	// Verify data in DB
	rows, err := store.db.Query("SELECT timestamp, pid, process, bytes, src_ip, dst_ip, protocol FROM traffic ORDER BY id")
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	defer rows.Close()

	var results []types.FlowEvent
	for rows.Next() {
		var ts int64
		var pid, bytes int
		var process, srcIP, dstIP, protocol string
		if err := rows.Scan(&ts, &pid, &process, &bytes, &srcIP, &dstIP, &protocol); err != nil {
			t.Fatalf("scan failed: %v", err)
		}
		results = append(results, types.FlowEvent{
			Timestamp: time.Unix(ts, 0),
			PID:       pid,
			Process:   process,
			Bytes:     bytes,
			SrcIP:     net.ParseIP(srcIP),
			DstIP:     net.ParseIP(dstIP),
			Protocol:  protocol,
		})
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(results))
	}

	if results[0].PID != 100 || results[0].Process != "firefox" || results[0].Bytes != 1024 {
		t.Errorf("row 0 mismatch: %+v", results[0])
	}
	if results[1].PID != 200 || results[1].Process != "curl" || results[1].Bytes != 512 {
		t.Errorf("row 1 mismatch: %+v", results[1])
	}
}

func TestSQLiteStore_BufferOverflow(t *testing.T) {
	dbPath := tempDBPath(t)
	store := NewSQLiteStore(dbPath, 10*time.Second, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := store.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer store.Close()

	// Write more than maxBufferSize events
	now := time.Now()
	batch := make([]types.FlowEvent, maxBufferSize+500)
	for i := range batch {
		batch[i] = makeFlowEvent(now, i, "proc", 100)
	}

	if err := store.Write(batch); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	store.mu.Lock()
	bufLen := len(store.buffer)
	store.mu.Unlock()

	if bufLen != maxBufferSize {
		t.Errorf("expected buffer size %d, got %d", maxBufferSize, bufLen)
	}

	drops := store.DropCount()
	if drops != 500 {
		t.Errorf("expected 500 drops, got %d", drops)
	}
}

func TestSQLiteStore_FlushEmptyBuffer(t *testing.T) {
	dbPath := tempDBPath(t)
	store := NewSQLiteStore(dbPath, 10*time.Second, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := store.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer store.Close()

	// Flush with empty buffer should be a no-op
	if err := store.Flush(); err != nil {
		t.Fatalf("Flush on empty buffer failed: %v", err)
	}
}

func TestSQLiteStore_GracefulShutdown(t *testing.T) {
	dbPath := tempDBPath(t)
	store := NewSQLiteStore(dbPath, 1*time.Hour, testLogger()) // Long interval so periodic flush won't trigger

	ctx, cancel := context.WithCancel(context.Background())

	if err := store.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	now := time.Now().Truncate(time.Second)
	events := []types.FlowEvent{
		makeFlowEvent(now, 42, "shutdown-test", 999),
	}

	if err := store.Write(events); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Cancel context to trigger graceful shutdown flush
	cancel()
	store.Close()

	// Verify data was flushed to DB
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM traffic").Scan(&count); err != nil {
		t.Fatalf("count query failed: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 row after shutdown flush, got %d", count)
	}
}

func TestSQLiteStore_PeriodicFlush(t *testing.T) {
	dbPath := tempDBPath(t)
	store := NewSQLiteStore(dbPath, 100*time.Millisecond, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := store.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer store.Close()

	now := time.Now().Truncate(time.Second)
	events := []types.FlowEvent{
		makeFlowEvent(now, 1, "periodic", 256),
	}

	if err := store.Write(events); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Wait for periodic flush to trigger
	time.Sleep(300 * time.Millisecond)

	var count int
	if err := store.db.QueryRow("SELECT COUNT(*) FROM traffic").Scan(&count); err != nil {
		t.Fatalf("count query failed: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 row after periodic flush, got %d", count)
	}
}

func TestSQLiteStore_NilIPs(t *testing.T) {
	dbPath := tempDBPath(t)
	store := NewSQLiteStore(dbPath, 10*time.Second, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := store.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer store.Close()

	now := time.Now().Truncate(time.Second)
	events := []types.FlowEvent{
		{
			Timestamp: now,
			PID:       1,
			Process:   "test",
			Bytes:     100,
			SrcIP:     nil,
			DstIP:     nil,
			Protocol:  "udp",
		},
	}

	if err := store.Write(events); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if err := store.Flush(); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	var srcIP, dstIP string
	err := store.db.QueryRow("SELECT src_ip, dst_ip FROM traffic").Scan(&srcIP, &dstIP)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if srcIP != "" || dstIP != "" {
		t.Errorf("expected empty IPs for nil, got src=%q dst=%q", srcIP, dstIP)
	}
}

func TestSQLiteStore_ErrorRetainsBuffer(t *testing.T) {
	// Use a path that will cause write failure after schema creation
	dbPath := tempDBPath(t)
	store := NewSQLiteStore(dbPath, 10*time.Second, testLogger())

	ctx, cancel := context.WithCancel(context.Background())

	if err := store.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	now := time.Now().Truncate(time.Second)
	events := []types.FlowEvent{
		makeFlowEvent(now, 1, "test", 100),
	}

	if err := store.Write(events); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Close the underlying DB to force a write failure
	store.db.Close()

	// Flush should fail
	err := store.Flush()
	if err == nil {
		t.Fatal("expected flush to fail after DB close")
	}

	// Buffer should be retained
	store.mu.Lock()
	bufLen := len(store.buffer)
	store.mu.Unlock()

	if bufLen != 1 {
		t.Errorf("expected buffer to retain 1 event after failure, got %d", bufLen)
	}

	// Reopen DB so Close() can properly shut down
	store.db, _ = sql.Open("sqlite", dbPath)
	cancel()
	<-store.done
	store.db.Close()
}
