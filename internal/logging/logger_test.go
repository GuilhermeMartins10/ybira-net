package logging

import (
	"testing"

	"go.uber.org/zap/zapcore"
)

func TestNewLogger_ValidLevels(t *testing.T) {
	levels := []string{"debug", "info", "warn", "error"}
	for _, level := range levels {
		t.Run(level, func(t *testing.T) {
			logger, err := NewLogger(level)
			if err != nil {
				t.Fatalf("NewLogger(%q) returned error: %v", level, err)
			}
			if logger == nil {
				t.Fatalf("NewLogger(%q) returned nil logger", level)
			}
			logger.Sync()
		})
	}
}

func TestNewLogger_CaseInsensitive(t *testing.T) {
	cases := []string{"DEBUG", "Info", "WARN", "Error", " info "}
	for _, level := range cases {
		t.Run(level, func(t *testing.T) {
			logger, err := NewLogger(level)
			if err != nil {
				t.Fatalf("NewLogger(%q) returned error: %v", level, err)
			}
			if logger == nil {
				t.Fatalf("NewLogger(%q) returned nil logger", level)
			}
			logger.Sync()
		})
	}
}

func TestNewLogger_InvalidLevel(t *testing.T) {
	invalidLevels := []string{"", "trace", "fatal", "verbose", "xyz"}
	for _, level := range invalidLevels {
		t.Run(level, func(t *testing.T) {
			_, err := NewLogger(level)
			if err == nil {
				t.Fatalf("NewLogger(%q) should have returned error for invalid level", level)
			}
		})
	}
}

func TestNewLogger_NamedLogger(t *testing.T) {
	logger, err := NewLogger("info")
	if err != nil {
		t.Fatalf("NewLogger failed: %v", err)
	}
	named := logger.Named("capture")
	if named == nil {
		t.Fatal("Named() returned nil")
	}
	named.Sync()
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected zapcore.Level
		wantErr  bool
	}{
		{"debug", zapcore.DebugLevel, false},
		{"info", zapcore.InfoLevel, false},
		{"warn", zapcore.WarnLevel, false},
		{"error", zapcore.ErrorLevel, false},
		{"DEBUG", zapcore.DebugLevel, false},
		{"INFO", zapcore.InfoLevel, false},
		{"WARN", zapcore.WarnLevel, false},
		{"ERROR", zapcore.ErrorLevel, false},
		{" info ", zapcore.InfoLevel, false},
		{"", zapcore.InfoLevel, true},
		{"trace", zapcore.InfoLevel, true},
		{"fatal", zapcore.InfoLevel, true},
		{"invalid", zapcore.InfoLevel, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			level, err := ParseLevel(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseLevel(%q) should have returned error", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseLevel(%q) returned unexpected error: %v", tt.input, err)
			}
			if level != tt.expected {
				t.Fatalf("ParseLevel(%q) = %v, want %v", tt.input, level, tt.expected)
			}
		})
	}
}
