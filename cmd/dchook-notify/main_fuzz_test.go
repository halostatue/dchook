package main

import (
	"encoding/json"
	"testing"

	"github.com/halostatue/dchook/internal/dchook"
)

func FuzzPayloadValidation(f *testing.F) {
	// Seed with edge cases
	f.Add([]byte(`{"valid":"json"}`))
	f.Add([]byte(`"plain text"`))
	f.Add([]byte("plain text no quotes"))
	f.Add([]byte{0xff, 0xfe, 0xfd}) // binary
	f.Add([]byte("\x00\x01\x02"))   // null bytes
	f.Add([]byte("\t\n\r"))         // whitespace
	f.Add([]byte("emoji: 🚀"))       // unicode

	f.Fuzz(func(t *testing.T, payload []byte) {
		// Limit to 1MB like the real code
		if len(payload) > dchook.MaxPayloadSize {
			return
		}

		// Test JSON parsing
		var parsed interface{}
		err := json.Unmarshal(payload, &parsed)
		if err != nil {
			// Should check if printable - must not panic
			_ = dchook.IsPrintableUTF8(payload)
		}
	})
}
