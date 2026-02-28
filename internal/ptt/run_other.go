//go:build !linux

package ptt

import (
	"context"
	"fmt"
	"runtime"
)

// Run is not supported on non-Linux platforms.
func Run(_ context.Context, _ Config) error {
	return fmt.Errorf("ptt-daemon requires Linux (evdev + PipeWire/PulseAudio); current platform: %s", runtime.GOOS)
}
