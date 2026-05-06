package store

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"github.com/ybira-net/ybira-net/internal/types"

	_ "modernc.org/sqlite"
)

type Storer interface {
	Start(ctx context.Context) error
	Write(events []types.FlowEvent) error
	Flush() error
	Close() error
}

const maxBufferSize = 10000

type SQLiteStore struct {
	db            *sql.DB
	logger        *zap.Logger
	dbPath        string
	flushInterval time.Duration

	mu     sync.Mutex
	buffer []types.FlowEvent

	dropCount atomic.Int64

	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
}

func NewSQLiteStore(dbPath string, flushInterval time.Duration, logger *zap.Logger) *SQLiteStore {
	return &SQLiteStore{
		dbPath:        dbPath,
		flushInterval: flushInterval,
		logger:        logger.Named("store"),
		buffer:        make([]types.FlowEvent, 0, 1024),
		done:          make(chan struct{}),
	}
}

func (s *SQLiteStore) Start(ctx context.Context) error {
	db, err := sql.Open("sqlite", s.dbPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}

	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return fmt.Errorf("set journal mode: %w", err)
	}

	s.db = db

	if err := s.createSchema(); err != nil {
		db.Close()
		return fmt.Errorf("create schema: %w", err)
	}

	s.ctx, s.cancel = context.WithCancel(ctx)

	go s.flushLoop()

	return nil
}

func (s *SQLiteStore) createSchema() error {
	const createTable = `
		CREATE TABLE IF NOT EXISTS traffic (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp INTEGER NOT NULL,
			pid INTEGER NOT NULL,
			process TEXT NOT NULL,
			bytes INTEGER NOT NULL,
			src_ip TEXT,
			dst_ip TEXT,
			protocol TEXT
		);`

	const createTimestampIndex = `CREATE INDEX IF NOT EXISTS idx_traffic_timestamp ON traffic(timestamp);`
	const createPIDIndex = `CREATE INDEX IF NOT EXISTS idx_traffic_pid ON traffic(pid);`

	if _, err := s.db.Exec(createTable); err != nil {
		return fmt.Errorf("create table: %w", err)
	}
	if _, err := s.db.Exec(createTimestampIndex); err != nil {
		return fmt.Errorf("create timestamp index: %w", err)
	}
	if _, err := s.db.Exec(createPIDIndex); err != nil {
		return fmt.Errorf("create pid index: %w", err)
	}

	return nil
}

func (s *SQLiteStore) Write(events []types.FlowEvent) error {
	if len(events) == 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.buffer = append(s.buffer, events...)

	if len(s.buffer) > maxBufferSize {
		overflow := len(s.buffer) - maxBufferSize
		s.buffer = s.buffer[overflow:]
		s.dropCount.Add(int64(overflow))
		s.logger.Warn("buffer overflow, dropped oldest events",
			zap.Int("dropped", overflow),
			zap.Int64("total_drops", s.dropCount.Load()),
		)
	}

	return nil
}

func (s *SQLiteStore) Flush() error {
	s.mu.Lock()
	if len(s.buffer) == 0 {
		s.mu.Unlock()
		return nil
	}
	batch := s.buffer
	s.buffer = make([]types.FlowEvent, 0, 1024)
	s.mu.Unlock()

	if err := s.writeBatch(batch); err != nil {
		s.mu.Lock()
		combined := append(batch, s.buffer...)
		if len(combined) > maxBufferSize {
			overflow := len(combined) - maxBufferSize
			combined = combined[overflow:]
			s.dropCount.Add(int64(overflow))
		}
		s.buffer = combined
		s.mu.Unlock()

		s.logger.Error("flush failed, retained buffer for retry",
			zap.Error(err),
			zap.Int("batch_size", len(batch)),
		)
		return err
	}

	return nil
}

func (s *SQLiteStore) writeBatch(events []types.FlowEvent) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	stmt, err := tx.Prepare(`INSERT INTO traffic (timestamp, pid, process, bytes, src_ip, dst_ip, protocol)
		VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("prepare statement: %w", err)
	}
	defer stmt.Close()

	for _, ev := range events {
		srcIP := ""
		if ev.SrcIP != nil {
			srcIP = ev.SrcIP.String()
		}
		dstIP := ""
		if ev.DstIP != nil {
			dstIP = ev.DstIP.String()
		}

		_, err := stmt.Exec(
			ev.Timestamp.Unix(),
			ev.PID,
			ev.Process,
			ev.Bytes,
			srcIP,
			dstIP,
			ev.Protocol,
		)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("insert event: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

func (s *SQLiteStore) flushLoop() {
	defer close(s.done)

	ticker := time.NewTicker(s.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := s.Flush(); err != nil {
				s.logger.Error("periodic flush failed", zap.Error(err))
			}
		case <-s.ctx.Done():
			if err := s.Flush(); err != nil {
				s.logger.Error("shutdown flush failed", zap.Error(err))
			}
			return
		}
	}
}

func (s *SQLiteStore) Close() error {
	s.cancel()
	<-s.done

	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

func (s *SQLiteStore) DropCount() int64 {
	return s.dropCount.Load()
}
