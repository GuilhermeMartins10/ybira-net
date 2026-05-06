package main

import (
	"fmt"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// **Validates: Requirements 10.2**
// Property: For any valid JSON stats response, the CLI produces a table with one row
// per stat entry containing the correct PID, process name, and byte count.
func TestProperty_CLITableOutput(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a random list of stat entries
		n := rapid.IntRange(0, 50).Draw(t, "numEntries")
		stats := make([]statEntry, n)
		for i := range stats {
			stats[i] = statEntry{
				PID:     rapid.IntRange(1, 999999).Draw(t, fmt.Sprintf("pid_%d", i)),
				Process: rapid.StringMatching(`[a-zA-Z][a-zA-Z0-9_\-]{0,14}`).Draw(t, fmt.Sprintf("process_%d", i)),
				Bytes:   int64(rapid.IntRange(0, 1<<40).Draw(t, fmt.Sprintf("bytes_%d", i))),
			}
		}

		// Format the table
		table := formatTable(stats)

		// The table must have a header line
		lines := strings.Split(strings.TrimRight(table, "\n"), "\n")
		if len(lines) < 1 {
			t.Fatal("table must have at least a header line")
		}

		// Verify header contains column names
		header := lines[0]
		if !strings.Contains(header, "PID") {
			t.Fatal("header must contain PID")
		}
		if !strings.Contains(header, "PROCESS") {
			t.Fatal("header must contain PROCESS")
		}
		if !strings.Contains(header, "BYTES") {
			t.Fatal("header must contain BYTES")
		}

		// Verify we have exactly one row per stat entry (header + n data rows)
		expectedLines := 1 + n
		if len(lines) != expectedLines {
			t.Fatalf("expected %d lines (1 header + %d rows), got %d", expectedLines, n, len(lines))
		}

		// Verify each data row contains the correct PID, process, and bytes
		for i, s := range stats {
			row := lines[i+1]
			pidStr := fmt.Sprintf("%d", s.PID)
			bytesStr := fmt.Sprintf("%d", s.Bytes)

			if !strings.Contains(row, pidStr) {
				t.Fatalf("row %d must contain PID %s, got: %s", i, pidStr, row)
			}
			if !strings.Contains(row, s.Process) {
				t.Fatalf("row %d must contain process %q, got: %s", i, s.Process, row)
			}
			if !strings.Contains(row, bytesStr) {
				t.Fatalf("row %d must contain bytes %s, got: %s", i, bytesStr, row)
			}
		}
	})
}
