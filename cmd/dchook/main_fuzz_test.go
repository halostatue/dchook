package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/abczzz13/clientip"

	"github.com/halostatue/dchook/internal/dchook"
)

func FuzzDeployHandler(f *testing.F) {
	// Setup test environment
	secret := "test-secret"
	allowedAlgos := map[string]bool{"sha256": true, "sha384": true, "sha512": true}
	limiter := dchook.NewRateLimiter(10, time.Minute, 5, time.Hour, 10*time.Minute)
	history := NewDeploymentHistory()
	adapter := &MockAdapter{}
	ipExtractor, err := clientip.New(clientip.PresetVMReverseProxy())
	if err != nil {
		f.Fatal(err)
	}

	cfg := &HandlerConfig{
		dockerAvailable:   true,
		ipExtractor:       ipExtractor,
		secret:            secret,
		allowedAlgorithms: allowedAlgos,
		adapter:           adapter,
		history:           history,
		version:           "v1.0.0",
		commit:            "abc",
	}

	handler := createDeployHandler(cfg, limiter)

	// Seed with valid and invalid inputs
	validPayload := []byte(
		`{"dchook":{"version":"v1.0.0","commit":"abc","timestamp":"1234567890000000"},"payload":{}}`,
	)
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

		req := httptest.NewRequest(http.MethodPost, "/deploy", bytes.NewReader(body))
		req.Header.Set("Dchook-Signature", signature)
		req.RemoteAddr = "127.0.0.1:12345"
		w := httptest.NewRecorder()

		// Call the actual handler - should not panic
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Handler panicked: %v", r)
				}
			}()

			handler(w, req)
		}()

		// Verify response is valid HTTP
		if w.Code == 0 {
			t.Error("Handler did not set status code")
		}
	})
}
