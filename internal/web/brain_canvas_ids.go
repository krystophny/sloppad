package web

import (
	"crypto/rand"
	"encoding/hex"
)

func newBrainCanvasID(prefix string, byteSize int) string {
	buf := make([]byte, byteSize)
	if _, err := rand.Read(buf); err != nil {
		return ""
	}
	return prefix + "-" + hex.EncodeToString(buf)
}
