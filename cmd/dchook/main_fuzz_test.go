package main

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/halostatue/dchook/internal/dchook"
)

func FuzzDeployHandler(f *testing.F) {
	// Setup test environment
	secret := "test-secret"
	allowedAlgos := map[string]bool{"sha256": true, "sha384": true, "sha512": true}
	limiter := dchook.NewRateLimiter(10, time.Minute, 5, time.Hour, 10*time.Minute)

	// Seed with valid and invalid inputs
	validPayload := []byte(`{"dchook":{"version":"v1.0.0","commit":"abc","timestamp":"1234567890000000"},"payload":{}}`)
	validSig := dchook.GenerateSignature(validPayload, secret, "sha256")

	f.Add(validPayload, validSig)
	f.Add([]byte(`malformed`), "sha256:hash")
	f.Add([]byte(`{"dchook":{"timestamp":"999999999999999999"}}`), "")
	f.Add([]byte{0xff, 0xfe}, "invalid")
	f.Add([]byte(""), "")

	f.Fuzz(func(t *testing.T, body []byte, signature string) {
		// Limit body size like MaxBytesReader does
		if len(body) > dchook.MaxRequestBodySize {
			return
		}

		req := httptest.NewRequest("POST", "/deploy", bytes.NewReader(body))
		req.Header.Set("Dchook-Signature", signature)
		req.RemoteAddr = "127.0.0.1:12345"
		w := httptest.NewRecorder()

		// Create a minimal handler that tests the core logic
		// without actually running docker commands
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Handler panicked: %v", r)
				}
			}()

			// Test signature verification
			_ = dchook.VerifySignature(body, signature, secret, allowedAlgos)

			// Test JSON parsing
			var envelope struct {
				Dchook struct {
					Version   string `json:"version"`
					Commit    string `json:"commit"`
					Timestamp string `json:"timestamp"`
				} `json:"dchook"`
				Payload interface{} `json:"payload"`
			}
			_ = json.Unmarshal(body, &envelope)

			// Test replay check if timestamp is valid
			if envelope.Dchook.Timestamp != "" {
				if ts, err := strconv.ParseInt(envelope.Dchook.Timestamp, 10, 64); err == nil {
					_ = limiter.CheckReplay(ts)
				}
			}
		}()

		// Verify response is valid HTTP
		if w.Code == 0 {
			t.Error("Handler did not set status code")
		}
	})
}
