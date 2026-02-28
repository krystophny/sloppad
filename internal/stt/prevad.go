package stt

import (
	"encoding/binary"
	"fmt"
	"math"
)

const (
	defaultPreVADThresholdDB = -58.0
	defaultPreVADMinSpeechMS = 120
	defaultPreVADFrameMS     = 20
)

func defaultPreVADConfig() PreVADConfig {
	return PreVADConfig{
		Enabled:     true,
		ThresholdDB: defaultPreVADThresholdDB,
		MinSpeechMS: defaultPreVADMinSpeechMS,
		FrameMS:     defaultPreVADFrameMS,
	}
}

func detectSpeechPCM16WAV(data []byte, cfg PreVADConfig) (bool, error) {
	if len(data) < 44 {
		return false, fmt.Errorf("wav too short")
	}
	if string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return false, fmt.Errorf("not a RIFF/WAVE payload")
	}

	channels := 1
	sampleRate := 16000
	bitsPerSample := 16
	dataStart := -1
	dataLen := 0

	offset := 12
	for offset+8 <= len(data) {
		chunkID := string(data[offset : offset+4])
		chunkSize := int(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
		offset += 8
		if chunkSize < 0 {
			return false, fmt.Errorf("invalid chunk size")
		}
		if offset > len(data) {
			break
		}
		if offset+chunkSize > len(data) {
			chunkSize = len(data) - offset
		}

		switch chunkID {
		case "fmt ":
			if chunkSize < 16 {
				return false, fmt.Errorf("invalid fmt chunk")
			}
			audioFormat := binary.LittleEndian.Uint16(data[offset : offset+2])
			if audioFormat != 1 {
				return false, fmt.Errorf("unsupported wav format: %d", audioFormat)
			}
			channels = int(binary.LittleEndian.Uint16(data[offset+2 : offset+4]))
			sampleRate = int(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
			bitsPerSample = int(binary.LittleEndian.Uint16(data[offset+14 : offset+16]))
		case "data":
			dataStart = offset
			dataLen = chunkSize
		}

		offset += chunkSize
		if chunkSize%2 == 1 {
			offset += 1
		}
	}

	if dataStart < 0 || dataLen <= 0 {
		return false, fmt.Errorf("wav data chunk missing")
	}
	if bitsPerSample != 16 {
		return false, fmt.Errorf("unsupported bits per sample: %d", bitsPerSample)
	}
	if channels <= 0 || sampleRate <= 0 {
		return false, fmt.Errorf("invalid wav format values")
	}

	frameMS := cfg.FrameMS
	if frameMS <= 0 {
		frameMS = defaultPreVADFrameMS
	}
	minSpeechMS := cfg.MinSpeechMS
	if minSpeechMS <= 0 {
		minSpeechMS = defaultPreVADMinSpeechMS
	}

	thresholdDB := cfg.ThresholdDB
	if !isFinite(thresholdDB) {
		thresholdDB = defaultPreVADThresholdDB
	}

	samplesPerFrame := (sampleRate * frameMS) / 1000
	if samplesPerFrame <= 0 {
		samplesPerFrame = 1
	}
	bytesPerSample := bitsPerSample / 8
	frameStrideBytes := samplesPerFrame * channels * bytesPerSample
	if frameStrideBytes <= 0 {
		return false, fmt.Errorf("invalid frame stride")
	}

	limit := dataStart + dataLen
	if limit > len(data) {
		limit = len(data)
	}

	accumSpeechMS := 0
	for pos := dataStart; pos+frameStrideBytes <= limit; pos += frameStrideBytes {
		sumSquares := 0.0
		sampleCount := 0
		frameEnd := pos + frameStrideBytes
		for i := pos; i+1 < frameEnd; i += channels * bytesPerSample {
			sample := int16(binary.LittleEndian.Uint16(data[i : i+2]))
			value := float64(sample) / 32768.0
			sumSquares += value * value
			sampleCount++
		}
		if sampleCount == 0 {
			continue
		}
		rms := math.Sqrt(sumSquares / float64(sampleCount))
		db := -100.0
		if rms > 0 {
			db = 20 * math.Log10(rms)
		}
		if db >= thresholdDB {
			accumSpeechMS += frameMS
			if accumSpeechMS >= minSpeechMS {
				return true, nil
			}
		}
	}

	return false, nil
}

func isFinite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
