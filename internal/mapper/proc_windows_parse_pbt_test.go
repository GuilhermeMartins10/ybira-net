package mapper

import (
	"encoding/binary"
	"fmt"
	"testing"

	"pgregory.net/rapid"
)

// **Validates: Requirements 1.1, 1.2, 3.1, 3.2**
// Property 2: Windows table parsing produces valid ConnectionEvents
// For any valid MIB_TCPROW_OWNER_PID or MIB_UDPROW_OWNER_PID byte buffer, parsing SHALL
// produce ConnectionEvents where LocalIP, LocalPort, RemoteIP, RemotePort, Protocol, and
// PID match the encoded values. UDP entries SHALL have RemoteIP="0.0.0.0" and RemotePort=0.

func TestProperty_WindowsTcpTableParsing(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a random number of TCP entries.
		numEntries := rapid.IntRange(1, 20).Draw(t, "numEntries")

		type tcpEntry struct {
			State      uint32
			LocalIP    [4]byte
			LocalPort  uint16
			RemoteIP   [4]byte
			RemotePort uint16
			PID        uint32
		}

		entries := make([]tcpEntry, numEntries)
		processNames := make(map[uint32]string)

		for i := 0; i < numEntries; i++ {
			// Generate random state (non-zero for realism).
			state := rapid.Uint32Range(1, 12).Draw(t, fmt.Sprintf("state_%d", i))

			// Generate random IP bytes.
			lip := [4]byte{
				byte(rapid.IntRange(0, 255).Draw(t, fmt.Sprintf("lip_0_%d", i))),
				byte(rapid.IntRange(0, 255).Draw(t, fmt.Sprintf("lip_1_%d", i))),
				byte(rapid.IntRange(0, 255).Draw(t, fmt.Sprintf("lip_2_%d", i))),
				byte(rapid.IntRange(0, 255).Draw(t, fmt.Sprintf("lip_3_%d", i))),
			}

			rip := [4]byte{
				byte(rapid.IntRange(0, 255).Draw(t, fmt.Sprintf("rip_0_%d", i))),
				byte(rapid.IntRange(0, 255).Draw(t, fmt.Sprintf("rip_1_%d", i))),
				byte(rapid.IntRange(0, 255).Draw(t, fmt.Sprintf("rip_2_%d", i))),
				byte(rapid.IntRange(0, 255).Draw(t, fmt.Sprintf("rip_3_%d", i))),
			}

			// Generate random ports (1-65535 to be valid).
			lport := rapid.Uint16Range(1, 65535).Draw(t, fmt.Sprintf("lport_%d", i))
			rport := rapid.Uint16Range(0, 65535).Draw(t, fmt.Sprintf("rport_%d", i))

			// Generate PID > 0 (PID 0 is skipped by the parser).
			pid := rapid.Uint32Range(1, 65535).Draw(t, fmt.Sprintf("pid_%d", i))

			entries[i] = tcpEntry{
				State:      state,
				LocalIP:    lip,
				LocalPort:  lport,
				RemoteIP:   rip,
				RemotePort: rport,
				PID:        pid,
			}

			// Generate a process name for this PID.
			if _, exists := processNames[pid]; !exists {
				procName := rapid.StringMatching(`[a-z]{3,12}\.exe`).Draw(t, fmt.Sprintf("proc_%d", i))
				processNames[pid] = procName
			}
		}

		// Encode the entries into a valid MIB_TCPTABLE_OWNER_PID buffer.
		bufSize := 4 + numEntries*tcpRowSize
		data := make([]byte, bufSize)

		// Write dwNumEntries (uint32, little-endian).
		binary.LittleEndian.PutUint32(data[0:4], uint32(numEntries))

		for i, entry := range entries {
			offset := 4 + i*tcpRowSize
			row := data[offset : offset+tcpRowSize]

			// State (4 bytes, little-endian).
			binary.LittleEndian.PutUint32(row[0:4], entry.State)

			// LocalAddr (4 bytes, network byte order / big-endian — raw bytes).
			row[4] = entry.LocalIP[0]
			row[5] = entry.LocalIP[1]
			row[6] = entry.LocalIP[2]
			row[7] = entry.LocalIP[3]

			// LocalPort (first 2 bytes of 4-byte field, big-endian).
			row[8] = byte(entry.LocalPort >> 8)
			row[9] = byte(entry.LocalPort & 0xFF)
			row[10] = 0
			row[11] = 0

			// RemoteAddr (4 bytes, network byte order / big-endian — raw bytes).
			row[12] = entry.RemoteIP[0]
			row[13] = entry.RemoteIP[1]
			row[14] = entry.RemoteIP[2]
			row[15] = entry.RemoteIP[3]

			// RemotePort (first 2 bytes of 4-byte field, big-endian).
			row[16] = byte(entry.RemotePort >> 8)
			row[17] = byte(entry.RemotePort & 0xFF)
			row[18] = 0
			row[19] = 0

			// OwningPid (4 bytes, little-endian).
			binary.LittleEndian.PutUint32(row[20:24], entry.PID)
		}

		// Parse the buffer.
		events := parseTcpTable(data, processNames)

		// All entries have PID > 0, so none should be skipped.
		if len(events) != numEntries {
			t.Fatalf("expected %d events, got %d", numEntries, len(events))
		}

		// Verify each parsed event matches the generated entry.
		for i, entry := range entries {
			event := events[i]

			expectedLocalIP := fmt.Sprintf("%d.%d.%d.%d",
				entry.LocalIP[0], entry.LocalIP[1], entry.LocalIP[2], entry.LocalIP[3])
			expectedRemoteIP := fmt.Sprintf("%d.%d.%d.%d",
				entry.RemoteIP[0], entry.RemoteIP[1], entry.RemoteIP[2], entry.RemoteIP[3])
			expectedProcessName := processNames[entry.PID]

			if event.Protocol != "tcp" {
				t.Fatalf("entry %d: Protocol = %q, want %q", i, event.Protocol, "tcp")
			}
			if event.LocalIP != expectedLocalIP {
				t.Fatalf("entry %d: LocalIP = %q, want %q", i, event.LocalIP, expectedLocalIP)
			}
			if event.LocalPort != entry.LocalPort {
				t.Fatalf("entry %d: LocalPort = %d, want %d", i, event.LocalPort, entry.LocalPort)
			}
			if event.RemoteIP != expectedRemoteIP {
				t.Fatalf("entry %d: RemoteIP = %q, want %q", i, event.RemoteIP, expectedRemoteIP)
			}
			if event.RemotePort != entry.RemotePort {
				t.Fatalf("entry %d: RemotePort = %d, want %d", i, event.RemotePort, entry.RemotePort)
			}
			if event.PID != int(entry.PID) {
				t.Fatalf("entry %d: PID = %d, want %d", i, event.PID, int(entry.PID))
			}
			if event.ProcessName != expectedProcessName {
				t.Fatalf("entry %d: ProcessName = %q, want %q", i, event.ProcessName, expectedProcessName)
			}
		}
	})
}

func TestProperty_WindowsUdpTableParsing(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a random number of UDP entries.
		numEntries := rapid.IntRange(1, 20).Draw(t, "numEntries")

		type udpEntry struct {
			LocalIP   [4]byte
			LocalPort uint16
			PID       uint32
		}

		entries := make([]udpEntry, numEntries)
		processNames := make(map[uint32]string)

		for i := 0; i < numEntries; i++ {
			// Generate random IP bytes.
			lip := [4]byte{
				byte(rapid.IntRange(0, 255).Draw(t, fmt.Sprintf("lip_0_%d", i))),
				byte(rapid.IntRange(0, 255).Draw(t, fmt.Sprintf("lip_1_%d", i))),
				byte(rapid.IntRange(0, 255).Draw(t, fmt.Sprintf("lip_2_%d", i))),
				byte(rapid.IntRange(0, 255).Draw(t, fmt.Sprintf("lip_3_%d", i))),
			}

			// Generate random port (1-65535 to be valid).
			lport := rapid.Uint16Range(1, 65535).Draw(t, fmt.Sprintf("lport_%d", i))

			// Generate PID > 0 (PID 0 is skipped by the parser).
			pid := rapid.Uint32Range(1, 65535).Draw(t, fmt.Sprintf("pid_%d", i))

			entries[i] = udpEntry{
				LocalIP:   lip,
				LocalPort: lport,
				PID:       pid,
			}

			// Generate a process name for this PID.
			if _, exists := processNames[pid]; !exists {
				procName := rapid.StringMatching(`[a-z]{3,12}\.exe`).Draw(t, fmt.Sprintf("proc_%d", i))
				processNames[pid] = procName
			}
		}

		// Encode the entries into a valid MIB_UDPTABLE_OWNER_PID buffer.
		bufSize := 4 + numEntries*udpRowSize
		data := make([]byte, bufSize)

		// Write dwNumEntries (uint32, little-endian).
		binary.LittleEndian.PutUint32(data[0:4], uint32(numEntries))

		for i, entry := range entries {
			offset := 4 + i*udpRowSize
			row := data[offset : offset+udpRowSize]

			// LocalAddr (4 bytes, network byte order / big-endian — raw bytes).
			row[0] = entry.LocalIP[0]
			row[1] = entry.LocalIP[1]
			row[2] = entry.LocalIP[2]
			row[3] = entry.LocalIP[3]

			// LocalPort (first 2 bytes of 4-byte field, big-endian).
			row[4] = byte(entry.LocalPort >> 8)
			row[5] = byte(entry.LocalPort & 0xFF)
			row[6] = 0
			row[7] = 0

			// OwningPid (4 bytes, little-endian).
			binary.LittleEndian.PutUint32(row[8:12], entry.PID)
		}

		// Parse the buffer.
		events := parseUdpTable(data, processNames)

		// All entries have PID > 0, so none should be skipped.
		if len(events) != numEntries {
			t.Fatalf("expected %d events, got %d", numEntries, len(events))
		}

		// Verify each parsed event matches the generated entry.
		for i, entry := range entries {
			event := events[i]

			expectedLocalIP := fmt.Sprintf("%d.%d.%d.%d",
				entry.LocalIP[0], entry.LocalIP[1], entry.LocalIP[2], entry.LocalIP[3])
			expectedProcessName := processNames[entry.PID]

			if event.Protocol != "udp" {
				t.Fatalf("entry %d: Protocol = %q, want %q", i, event.Protocol, "udp")
			}
			if event.LocalIP != expectedLocalIP {
				t.Fatalf("entry %d: LocalIP = %q, want %q", i, event.LocalIP, expectedLocalIP)
			}
			if event.LocalPort != entry.LocalPort {
				t.Fatalf("entry %d: LocalPort = %d, want %d", i, event.LocalPort, entry.LocalPort)
			}
			if event.RemoteIP != "0.0.0.0" {
				t.Fatalf("entry %d: RemoteIP = %q, want %q", i, event.RemoteIP, "0.0.0.0")
			}
			if event.RemotePort != 0 {
				t.Fatalf("entry %d: RemotePort = %d, want %d", i, event.RemotePort, 0)
			}
			if event.PID != int(entry.PID) {
				t.Fatalf("entry %d: PID = %d, want %d", i, event.PID, int(entry.PID))
			}
			if event.ProcessName != expectedProcessName {
				t.Fatalf("entry %d: ProcessName = %q, want %q", i, event.ProcessName, expectedProcessName)
			}
		}
	})
}
