package main

import (
	"testing"

	"github.com/halostatue/dchook/internal/dchook"
)

func TestGenerateSignature(t *testing.T) {
	payload := []byte(`{"test":"data"}`)
	secret := "test-secret"

	tests := []struct {
		algorithm  string
		wantPrefix string
	}{
		{"sha256", "sha256:"},
		{"sha384", "sha384:"},
		{"sha512", "sha512:"},
	}

	for _, tt := range tests {
		t.Run(tt.algorithm, func(t *testing.T) {
			sig := dchook.GenerateSignature(payload, secret, tt.algorithm)
			if len(sig) == 0 {
				t.Error("Signature should not be empty")
			}
			if sig[:len(tt.wantPrefix)] != tt.wantPrefix {
				t.Errorf("Signature should start with %q, got %q", tt.wantPrefix, sig[:len(tt.wantPrefix)])
			}
		})
	}
}

func TestGenerateSignatureConsistency(t *testing.T) {
	payload := []byte(`{"test":"data"}`)
	secret := "test-secret"

	sig1 := dchook.GenerateSignature(payload, secret, "sha256")
	sig2 := dchook.GenerateSignature(payload, secret, "sha256")

	if sig1 != sig2 {
		t.Error("Same payload should generate same signature")
	}

	sig3 := dchook.GenerateSignature([]byte(`{"different":"data"}`), secret, "sha256")
	if sig1 == sig3 {
		t.Error("Different payload should generate different signature")
	}
}
