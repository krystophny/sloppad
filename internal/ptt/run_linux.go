//go:build linux

package ptt

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/krystophny/tabura/internal/stt"
)

const (
	evKey  = 0x01
	keyUp  = 0
	keyDown = 1

	inputEventSize = 24 // sizeof(struct input_event) on linux/amd64
)

type inputEvent struct {
	Sec   int64
	Usec  int64
	Type  uint16
	Code  uint16
	Value int32
}

func init() {
	if unsafe.Sizeof(inputEvent{}) != inputEventSize {
		panic("inputEvent struct size mismatch")
	}
}

// Run starts the PTT daemon: listens for key events, records audio, transcribes, outputs text.
func Run(ctx context.Context, cfg Config) error {
	if cfg.DevicePath == "" {
		path, err := findDeviceWithKey(cfg.KeyCode)
		if err != nil {
			return fmt.Errorf("no evdev device found with key %d: %w", cfg.KeyCode, err)
		}
		cfg.DevicePath = path
		log.Printf("auto-detected evdev device: %s", cfg.DevicePath)
	}

	f, err := os.Open(cfg.DevicePath)
	if err != nil {
		return fmt.Errorf("open evdev device %s: %w (try running with input group or sudo)", cfg.DevicePath, err)
	}
	defer f.Close()

	replacements := FetchReplacements(cfg.WebAPIURL)
	log.Printf("loaded %d STT replacements", len(replacements))

	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Printf("listening for key %d on %s (output: %s)", cfg.KeyCode, cfg.DevicePath, cfg.OutputMode)

	var recording bool
	var recorder *audioRecorder

	buf := make([]byte, inputEventSize)
	for {
		select {
		case <-ctx.Done():
			if recorder != nil {
				recorder.Stop()
			}
			return nil
		default:
		}

		n, err := f.Read(buf)
		if err != nil {
			return fmt.Errorf("read evdev: %w", err)
		}
		if n != inputEventSize {
			continue
		}

		var ev inputEvent
		if err := binary.Read(bytes.NewReader(buf), binary.LittleEndian, &ev); err != nil {
			continue
		}

		if ev.Type != evKey || ev.Code != cfg.KeyCode {
			continue
		}

		switch ev.Value {
		case keyDown:
			if recording {
				continue
			}
			recording = true
			log.Println("key down: recording started")
			recorder = startAudioRecorder()

		case keyUp:
			if !recording {
				continue
			}
			recording = false
			log.Println("key up: recording stopped")

			pcm := recorder.Stop()
			if len(pcm) == 0 {
				log.Println("no audio captured")
				continue
			}

			wav := WrapWAV(pcm)
			go processAndOutput(cfg, wav, replacements)
		}
	}
}

func processAndOutput(cfg Config, wav []byte, replacements []stt.Replacement) {
	text, err := TranscribeAudio(cfg.WhisperURL, wav, replacements)
	if err != nil {
		if stt.IsRetryableNoSpeechError(err) {
			log.Printf("no speech detected: %v", err)
			return
		}
		log.Printf("transcription error: %v", err)
		return
	}
	log.Printf("transcribed: %s", text)
	outputText(cfg.OutputMode, text)
}

type audioRecorder struct {
	cmd    *exec.Cmd
	stdout bytes.Buffer
	done   chan struct{}
}

func startAudioRecorder() *audioRecorder {
	rec := &audioRecorder{done: make(chan struct{})}

	// Try pw-cat first (PipeWire), fall back to parecord (PulseAudio)
	if path, err := exec.LookPath("pw-cat"); err == nil {
		rec.cmd = exec.Command(path, "--record", "--format", "s16", "--rate", "16000", "--channels", "1", "-")
	} else if path, err := exec.LookPath("parecord"); err == nil {
		rec.cmd = exec.Command(path, "--format=s16le", "--rate=16000", "--channels=1", "--raw", "/dev/stdout")
	} else {
		log.Println("no audio capture tool found (need pw-cat or parecord)")
		close(rec.done)
		return rec
	}

	rec.cmd.Stdout = &rec.stdout
	rec.cmd.Stderr = nil
	if err := rec.cmd.Start(); err != nil {
		log.Printf("failed to start audio capture: %v", err)
		close(rec.done)
		return rec
	}

	go func() {
		_ = rec.cmd.Wait()
		close(rec.done)
	}()

	return rec
}

func (r *audioRecorder) Stop() []byte {
	if r.cmd == nil || r.cmd.Process == nil {
		return nil
	}
	_ = r.cmd.Process.Signal(syscall.SIGINT)

	select {
	case <-r.done:
	case <-time.After(3 * time.Second):
		_ = r.cmd.Process.Kill()
		<-r.done
	}

	return r.stdout.Bytes()
}

func outputText(mode, text string) {
	switch mode {
	case "clipboard":
		cmd := exec.Command("wl-copy", text)
		if err := cmd.Run(); err != nil {
			log.Printf("wl-copy failed: %v", err)
		}
	default: // "type"
		cmd := exec.Command("ydotool", "type", "--", text)
		if err := cmd.Run(); err != nil {
			log.Printf("ydotool failed: %v", err)
		}
	}
}

// findDeviceWithKey scans /dev/input/event* for a device that supports the given key code.
func findDeviceWithKey(keyCode uint16) (string, error) {
	matches, err := filepath.Glob("/dev/input/event*")
	if err != nil {
		return "", err
	}
	for _, path := range matches {
		if deviceHasKey(path, keyCode) {
			return path, nil
		}
	}
	return "", fmt.Errorf("no device supports key %d", keyCode)
}

func deviceHasKey(path string, keyCode uint16) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	// EVIOCGBIT(EV_KEY, KEY_MAX/8+1) = ioctl to get key capabilities
	const keyMax = 0x2ff
	bufSize := keyMax/8 + 1
	bits := make([]byte, bufSize)

	// EVIOCGBIT(ev_type, len) = _IOC(_IOC_READ, 'E', 0x20+ev_type, len)
	req := uintptr(0x80000000) | (uintptr(bufSize) << 16) | ('E' << 8) | (0x20 + evKey)

	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), req, uintptr(unsafe.Pointer(&bits[0])))
	if errno != 0 {
		return false
	}

	byteIdx := keyCode / 8
	bitIdx := keyCode % 8
	if int(byteIdx) >= len(bits) {
		return false
	}
	return bits[byteIdx]&(1<<bitIdx) != 0
}

// FindKeyName returns a human-readable name for common key codes.
func FindKeyName(code uint16) string {
	names := map[uint16]string{
		183: "F13", 184: "F14", 185: "F15", 186: "F16",
		187: "F17", 188: "F18", 189: "F19", 190: "F20",
	}
	if name, ok := names[code]; ok {
		return name
	}
	return fmt.Sprintf("KEY_%d", code)
}

// ListInputDevices returns info about available input devices for diagnostics.
func ListInputDevices() []string {
	matches, _ := filepath.Glob("/dev/input/event*")
	var out []string
	for _, path := range matches {
		name := readDeviceName(path)
		if name != "" {
			out = append(out, fmt.Sprintf("%s: %s", path, name))
		}
	}
	return out
}

func readDeviceName(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	buf := make([]byte, 256)
	// EVIOCGNAME(len) = _IOC(_IOC_READ, 'E', 0x06, len)
	req := uintptr(0x80000000) | (256 << 16) | ('E' << 8) | 0x06
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), req, uintptr(unsafe.Pointer(&buf[0])))
	if errno != 0 {
		return ""
	}
	return strings.TrimRight(string(buf), "\x00")
}
