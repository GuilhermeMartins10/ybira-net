//go:build windows

package mapper

import (
	"syscall"
	"unsafe"

	"go.uber.org/zap"
	"golang.org/x/sys/windows"
)

const (
	afINET                  = 2
	tcpTableOwnerPidAll     = 5
	udpTableOwnerPid        = 1
	errorInsufficientBuffer = 122
)

type WindowsProcReader struct {
	iphlpapi *syscall.LazyDLL
	logger   *zap.Logger
}

func NewDefaultProcReader() *WindowsProcReader {
	return &WindowsProcReader{
		iphlpapi: syscall.NewLazyDLL("iphlpapi.dll"),
		logger:   zap.NewNop(),
	}
}

func NewWindowsProcReaderWithLogger(logger *zap.Logger) *WindowsProcReader {
	return &WindowsProcReader{
		iphlpapi: syscall.NewLazyDLL("iphlpapi.dll"),
		logger:   logger,
	}
}

func (r *WindowsProcReader) ReadConnections() ([]ConnectionEvent, error) {
	processNames := r.buildProcessNameMap()

	var events []ConnectionEvent

	tcpData, err := r.getExtendedTcpTable()
	if err != nil {
		r.logger.Warn("GetExtendedTcpTable failed, continuing with UDP only", zap.Error(err))
	} else {
		events = append(events, parseTcpTable(tcpData, processNames)...)
	}

	udpData, err := r.getExtendedUdpTable()
	if err != nil {
		r.logger.Warn("GetExtendedUdpTable failed, returning TCP results only", zap.Error(err))
	} else {
		events = append(events, parseUdpTable(udpData, processNames)...)
	}

	return events, nil
}

func (r *WindowsProcReader) buildProcessNameMap() map[uint32]string {
	processNames := make(map[uint32]string)

	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		r.logger.Warn("CreateToolhelp32Snapshot failed, process names will be unknown", zap.Error(err))
		return processNames
	}
	defer windows.CloseHandle(snapshot)

	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))

	err = windows.Process32First(snapshot, &entry)
	if err != nil {
		r.logger.Warn("Process32First failed", zap.Error(err))
		return processNames
	}

	for {
		name := windows.UTF16ToString(entry.ExeFile[:])
		if name != "" {
			processNames[entry.ProcessID] = name
		}

		err = windows.Process32Next(snapshot, &entry)
		if err != nil {
			break
		}
	}

	return processNames
}

func (r *WindowsProcReader) getExtendedTcpTable() ([]byte, error) {
	proc := r.iphlpapi.NewProc("GetExtendedTcpTable")

	var size uint32
	ret, _, _ := proc.Call(
		0,
		uintptr(unsafe.Pointer(&size)),
		0,
		uintptr(afINET),
		uintptr(tcpTableOwnerPidAll),
		0,
	)

	if ret != errorInsufficientBuffer && ret != 0 {
		return nil, syscall.Errno(ret)
	}

	if size == 0 {
		return nil, nil
	}

	buf := make([]byte, size)
	ret, _, _ = proc.Call(
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&size)),
		0,
		uintptr(afINET),
		uintptr(tcpTableOwnerPidAll),
		0,
	)

	if ret != 0 {
		return nil, syscall.Errno(ret)
	}

	return buf[:size], nil
}

func (r *WindowsProcReader) getExtendedUdpTable() ([]byte, error) {
	proc := r.iphlpapi.NewProc("GetExtendedUdpTable")

	var size uint32
	ret, _, _ := proc.Call(
		0,
		uintptr(unsafe.Pointer(&size)),
		0,
		uintptr(afINET),
		uintptr(udpTableOwnerPid),
		0,
	)

	if ret != errorInsufficientBuffer && ret != 0 {
		return nil, syscall.Errno(ret)
	}

	if size == 0 {
		return nil, nil
	}

	buf := make([]byte, size)
	ret, _, _ = proc.Call(
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&size)),
		0,
		uintptr(afINET),
		uintptr(udpTableOwnerPid),
		0,
	)

	if ret != 0 {
		return nil, syscall.Errno(ret)
	}

	return buf[:size], nil
}
