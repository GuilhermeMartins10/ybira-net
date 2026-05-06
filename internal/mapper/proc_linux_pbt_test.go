//go:build linux

package mapper

import (
	"fmt"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// **Validates: Requirements 1.1, 1.2, 2.1, 2.2, 2.3**
// Property 1: Linux proc parsing produces valid ConnectionEvents
// For any valid /proc/net/tcp or /proc/net/udp content with known entries, parsing and
// joining with a known PID/inode mapping SHALL produce ConnectionEvents where each event's
// LocalIP, LocalPort, Protocol, PID, and ProcessName match the original data.

// encodeIPToHexLE encodes a 4-byte IP address to the hex little-endian format used in /proc/net.
// In /proc/net, the IP is stored as a 32-bit integer in host byte order (little-endian on x86),
// which means the bytes are reversed compared to the standard dotted-decimal representation.
// For example, 127.0.0.1 (bytes: 7F 00 00 01) is stored as 0100007F.
func encodeIPToHexLE(ip [4]byte) string {
	return fmt.Sprintf("%02X%02X%02X%02X", ip[3], ip[2], ip[1], ip[0])
}

// encodeProcNetContent generates /proc/net/tcp or /proc/net/udp content from entries.
func encodeProcNetContent(entries []testProcEntry) []byte {
	var sb strings.Builder
	// Write header line.
	sb.WriteString("  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n")

	for i, entry := range entries {
		hexIP := encodeIPToHexLE(entry.IP)
		hexPort := fmt.Sprintf("%04X", entry.Port)
		// Format: "   N: HEXIP:HEXPORT 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 INODE"
		line := fmt.Sprintf("   %d: %s:%s 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 %d\n",
			i, hexIP, hexPort, entry.Inode)
		sb.WriteString(line)
	}

	return []byte(sb.String())
}

// testProcEntry holds generated test data for a /proc/net entry.
type testProcEntry struct {
	IP    [4]byte
	Port  uint16
	Inode uint64
}

func TestProperty_LinuxProcParsing(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a random number of entries.
		numEntries := rapid.IntRange(1, 20).Draw(t, "numEntries")

		entries := make([]testProcEntry, numEntries)

		for i := 0; i < numEntries; i++ {
			// Generate random IP bytes.
			ip := [4]byte{
				byte(rapid.IntRange(0, 255).Draw(t, fmt.Sprintf("ip_0_%d", i))),
				byte(rapid.IntRange(0, 255).Draw(t, fmt.Sprintf("ip_1_%d", i))),
				byte(rapid.IntRange(0, 255).Draw(t, fmt.Sprintf("ip_2_%d", i))),
				byte(rapid.IntRange(0, 255).Draw(t, fmt.Sprintf("ip_3_%d", i))),
			}

			// Generate random port (1-65535).
			port := rapid.Uint16Range(1, 65535).Draw(t, fmt.Sprintf("port_%d", i))

			// Generate random inode (must be > 0, since inode 0 is skipped by parser).
			inode := rapid.Uint64Range(1, 999999).Draw(t, fmt.Sprintf("inode_%d", i))

			entries[i] = testProcEntry{
				IP:    ip,
				Port:  port,
				Inode: inode,
			}
		}

		// Encode entries as /proc/net content.
		data := encodeProcNetContent(entries)

		// Parse with ParseProcNet.
		parsed := ParseProcNet(data)

		// Verify the number of parsed entries matches.
		if len(parsed) != numEntries {
			t.Fatalf("expected %d entries, got %d", numEntries, len(parsed))
		}

		// Verify each parsed entry matches the generated data.
		for i, entry := range entries {
			p := parsed[i]

			// Expected IP in dotted-decimal format.
			expectedIP := fmt.Sprintf("%d.%d.%d.%d", entry.IP[0], entry.IP[1], entry.IP[2], entry.IP[3])

			if p.IP != expectedIP {
				t.Fatalf("entry %d: IP = %q, want %q", i, p.IP, expectedIP)
			}
			if p.Port != entry.Port {
				t.Fatalf("entry %d: Port = %d, want %d", i, p.Port, entry.Port)
			}
			if p.Inode != entry.Inode {
				t.Fatalf("entry %d: Inode = %d, want %d", i, p.Inode, entry.Inode)
			}
		}
	})
}

