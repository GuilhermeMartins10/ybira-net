//go:build windows

package main

import (
	"github.com/ybira-net/ybira-net/internal/mapper"
	"go.uber.org/zap"
)

func Preflight(logger *zap.Logger) {
	if err := mapper.CheckNpcap(); err != nil {
		logger.Fatal("Npcap is required for packet capture on Windows",
			zap.Error(err),
			zap.String("install_url", "https://npcap.com/#download"),
		)
	}
}
