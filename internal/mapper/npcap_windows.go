//go:build windows

package mapper

import (
	"fmt"
	"syscall"
)

func CheckNpcap() error {
	dll := syscall.NewLazyDLL("wpcap.dll")
	if err := dll.Load(); err != nil {
		return fmt.Errorf("npcap not found: wpcap.dll could not be loaded (%w). Install Npcap from https://npcap.com/#download", err)
	}
	return nil
}
