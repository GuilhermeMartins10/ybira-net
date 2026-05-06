//go:build linux

package mapper

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type LinuxProcReader struct {
	ProcRoot string
}

func NewDefaultProcReader() *LinuxProcReader {
	return &LinuxProcReader{}
}

func (r *LinuxProcReader) procRoot() string {
	if r.ProcRoot != "" {
		return r.ProcRoot
	}
	return "/proc"
}

func (r *LinuxProcReader) readNetTCP() ([]byte, error) {
	return os.ReadFile(filepath.Join(r.procRoot(), "net", "tcp"))
}

func (r *LinuxProcReader) readNetUDP() ([]byte, error) {
	return os.ReadFile(filepath.Join(r.procRoot(), "net", "udp"))
}

func (r *LinuxProcReader) listPIDs() ([]int, error) {
	entries, err := os.ReadDir(r.procRoot())
	if err != nil {
		return nil, fmt.Errorf("reading proc dir: %w", err)
	}

	var pids []int
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		pids = append(pids, pid)
	}
	return pids, nil
}

func (r *LinuxProcReader) listFDs(pid int) (map[uint64]bool, error) {
	fdDir := filepath.Join(r.procRoot(), strconv.Itoa(pid), "fd")
	entries, err := os.ReadDir(fdDir)
	if err != nil {
		return nil, err
	}

	inodes := make(map[uint64]bool)
	for _, entry := range entries {
		link, err := os.Readlink(filepath.Join(fdDir, entry.Name()))
		if err != nil {
			continue
		}
		inode, ok := parseSocketLink(link)
		if ok {
			inodes[inode] = true
		}
	}
	return inodes, nil
}

func (r *LinuxProcReader) readComm(pid int) (string, error) {
	data, err := os.ReadFile(filepath.Join(r.procRoot(), strconv.Itoa(pid), "comm"))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func (r *LinuxProcReader) ReadConnections() ([]ConnectionEvent, error) {
	var events []ConnectionEvent

	type pidInfo struct {
		PID  int
		Name string
	}
	inodeMap := make(map[uint64]pidInfo)

	pids, err := r.listPIDs()
	if err != nil {
		return nil, fmt.Errorf("listing PIDs: %w", err)
	}

	for _, pid := range pids {
		inodes, err := r.listFDs(pid)
		if err != nil {
			continue
		}
		if len(inodes) == 0 {
			continue
		}
		name, err := r.readComm(pid)
		if err != nil {
			name = "unknown"
		}
		for inode := range inodes {
			inodeMap[inode] = pidInfo{PID: pid, Name: name}
		}
	}

	tcpData, err := r.readNetTCP()
	if err == nil {
		entries := ParseProcNet(tcpData)
		for _, e := range entries {
			info := inodeMap[e.Inode]
			events = append(events, ConnectionEvent{
				Protocol:    "tcp",
				LocalIP:     e.IP,
				LocalPort:   e.Port,
				RemoteIP:    "0.0.0.0",
				RemotePort:  0,
				PID:         info.PID,
				ProcessName: info.Name,
			})
		}
	}

	udpData, err := r.readNetUDP()
	if err == nil {
		entries := ParseProcNet(udpData)
		for _, e := range entries {
			info := inodeMap[e.Inode]
			events = append(events, ConnectionEvent{
				Protocol:    "udp",
				LocalIP:     e.IP,
				LocalPort:   e.Port,
				RemoteIP:    "",
				RemotePort:  0,
				PID:         info.PID,
				ProcessName: info.Name,
			})
		}
	}

	return events, nil
}
