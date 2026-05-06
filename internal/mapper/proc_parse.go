package mapper

import (
	"encoding/hex"
	"fmt"
	"net"
	"strconv"
	"strings"
)

type ProcNetEntry struct {
	IP    string
	Port  uint16
	Inode uint64
}

func ParseProcNet(data []byte) []ProcNetEntry {
	lines := strings.Split(string(data), "\n")
	var entries []ProcNetEntry

	for i, line := range lines {
		if i == 0 {
			continue
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 10 {
			continue
		}

		ip, port, err := parseHexAddress(fields[1])
		if err != nil {
			continue
		}

		inode, err := strconv.ParseUint(fields[9], 10, 64)
		if err != nil {
			continue
		}

		if inode == 0 {
			continue
		}

		entries = append(entries, ProcNetEntry{
			IP:    ip,
			Port:  port,
			Inode: inode,
		})
	}

	return entries
}

func parseHexAddress(addr string) (string, uint16, error) {
	parts := strings.Split(addr, ":")
	if len(parts) != 2 {
		return "", 0, fmt.Errorf("invalid address format: %s", addr)
	}

	ipHex := parts[0]
	ip, err := parseHexIP(ipHex)
	if err != nil {
		return "", 0, err
	}

	portVal, err := strconv.ParseUint(parts[1], 16, 16)
	if err != nil {
		return "", 0, fmt.Errorf("invalid port: %s", parts[1])
	}

	return ip, uint16(portVal), nil
}

func parseHexIP(hexIP string) (string, error) {
	if len(hexIP) == 8 {
		b, err := hex.DecodeString(hexIP)
		if err != nil {
			return "", fmt.Errorf("invalid hex IP: %s", hexIP)
		}
		ip := net.IPv4(b[3], b[2], b[1], b[0])
		return ip.String(), nil
	} else if len(hexIP) == 32 {
		b, err := hex.DecodeString(hexIP)
		if err != nil {
			return "", fmt.Errorf("invalid hex IP: %s", hexIP)
		}
		for i := 0; i < 16; i += 4 {
			b[i], b[i+1], b[i+2], b[i+3] = b[i+3], b[i+2], b[i+1], b[i]
		}
		ip := net.IP(b)
		return ip.String(), nil
	}
	return "", fmt.Errorf("unsupported hex IP length: %d", len(hexIP))
}

func parseSocketLink(link string) (uint64, bool) {
	if !strings.HasPrefix(link, "socket:[") || !strings.HasSuffix(link, "]") {
		return 0, false
	}
	inodeStr := link[8 : len(link)-1]
	inode, err := strconv.ParseUint(inodeStr, 10, 64)
	if err != nil {
		return 0, false
	}
	return inode, true
}
