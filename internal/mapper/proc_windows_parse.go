package mapper

import (
	"encoding/binary"
	"fmt"
)

type mibTcpRowOwnerPid struct {
	State      uint32
	LocalAddr  [4]byte
	LocalPort  [4]byte
	RemoteAddr [4]byte
	RemotePort [4]byte
	OwningPid  uint32
}

type mibUdpRowOwnerPid struct {
	LocalAddr [4]byte
	LocalPort [4]byte
	OwningPid uint32
}

const (
	tcpRowSize = 24
	udpRowSize = 12
)

func parseTcpTable(data []byte, processNames map[uint32]string) []ConnectionEvent {
	if len(data) < 4 {
		return nil
	}

	numEntries := binary.LittleEndian.Uint32(data[0:4])
	offset := 4

	var events []ConnectionEvent

	for i := uint32(0); i < numEntries; i++ {
		if offset+tcpRowSize > len(data) {
			break
		}

		row := data[offset : offset+tcpRowSize]
		offset += tcpRowSize

		pid := binary.LittleEndian.Uint32(row[20:24])
		if pid == 0 {
			continue
		}

		localIP := fmt.Sprintf("%d.%d.%d.%d", row[4], row[5], row[6], row[7])
		remoteIP := fmt.Sprintf("%d.%d.%d.%d", row[12], row[13], row[14], row[15])
		localPort := uint16(row[8])<<8 | uint16(row[9])
		remotePort := uint16(row[16])<<8 | uint16(row[17])

		processName := "unknown"
		if name, ok := processNames[pid]; ok {
			processName = name
		}

		events = append(events, ConnectionEvent{
			Protocol:    "tcp",
			LocalIP:     localIP,
			LocalPort:   localPort,
			RemoteIP:    remoteIP,
			RemotePort:  remotePort,
			PID:         int(pid),
			ProcessName: processName,
		})
	}

	return events
}

func parseUdpTable(data []byte, processNames map[uint32]string) []ConnectionEvent {
	if len(data) < 4 {
		return nil
	}

	numEntries := binary.LittleEndian.Uint32(data[0:4])
	offset := 4

	var events []ConnectionEvent

	for i := uint32(0); i < numEntries; i++ {
		if offset+udpRowSize > len(data) {
			break
		}

		row := data[offset : offset+udpRowSize]
		offset += udpRowSize

		pid := binary.LittleEndian.Uint32(row[8:12])
		if pid == 0 {
			continue
		}

		localIP := fmt.Sprintf("%d.%d.%d.%d", row[0], row[1], row[2], row[3])
		localPort := uint16(row[4])<<8 | uint16(row[5])

		processName := "unknown"
		if name, ok := processNames[pid]; ok {
			processName = name
		}

		events = append(events, ConnectionEvent{
			Protocol:    "udp",
			LocalIP:     localIP,
			LocalPort:   localPort,
			RemoteIP:    "0.0.0.0",
			RemotePort:  0,
			PID:         int(pid),
			ProcessName: processName,
		})
	}

	return events
}
