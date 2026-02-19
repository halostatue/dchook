package dchook_test

import (
	"testing"

	"github.com/halostatue/dchook/internal/dchook"
)

func TestGenerateSignature(t *testing.T) {
	tests := []struct {
		name       string
		payload    []byte
		secret     string
		algorithm  string
		wantPrefix string
		wantEmpty  bool
	}{
		{
			name:       "sha256 with object",
			payload:    []byte(`{"test":"data"}`),
			secret:     "test-secret",
			algorithm:  "sha256",
			wantPrefix: "sha256:",
		},
		{
			name:       "sha256 with string",
			payload:    []byte(`"deadbeef"`),
			secret:     "test-secret",
			algorithm:  "sha256",
			wantPrefix: "sha256:",
		},
		{
			name:       "sha256 with number",
			payload:    []byte(`42`),
			secret:     "test-secret",
			algorithm:  "sha256",
			wantPrefix: "sha256:",
		},
		{
			name:       "sha256 with array",
			payload:    []byte(`[1,2,3]`),
			secret:     "test-secret",
			algorithm:  "sha256",
			wantPrefix: "sha256:",
		},
		{
			name:       "sha256 with null",
			payload:    []byte(`null`),
			secret:     "test-secret",
			algorithm:  "sha256",
			wantPrefix: "sha256:",
		},
		{
			name:       "sha384",
			payload:    []byte(`{"test":"data"}`),
			secret:     "test-secret",
			algorithm:  "sha384",
			wantPrefix: "sha384:",
		},
		{
			name:       "sha512",
			payload:    []byte(`{"test":"data"}`),
			secret:     "test-secret",
			algorithm:  "sha512",
			wantPrefix: "sha512:",
		},
		{
			name:      "invalid algorithm",
			payload:   []byte(`{"test":"data"}`),
			secret:    "test-secret",
			algorithm: "md5",
			wantEmpty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sig := dchook.GenerateSignature(tt.payload, tt.secret, tt.algorithm)

			if tt.wantEmpty {
				if sig != "" {
					t.Errorf("Expected empty signature for invalid algorithm, got %q", sig)
				}
				return
			}

			if len(sig) == 0 {
				t.Error("Signature should not be empty")
			}
			if len(sig) < len(tt.wantPrefix) || sig[:len(tt.wantPrefix)] != tt.wantPrefix {
				t.Errorf("Signature should start with %q, got %q", tt.wantPrefix, sig)
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

func TestVerifySignature(t *testing.T) {
	secret := "test-secret"
	payload := []byte(`{"test":"data"}`)
	allowedAlgos := map[string]bool{"sha256": true, "sha384": true, "sha512": true}

	tests := []struct {
		name      string
		payload   []byte
		signature string
		allowed   map[string]bool
		want      bool
	}{
		{
			name:      "valid sha256",
			payload:   payload,
			signature: dchook.GenerateSignature(payload, secret, "sha256"),
			allowed:   allowedAlgos,
			want:      true,
		},
		{
			name:      "valid sha384",
			payload:   payload,
			signature: dchook.GenerateSignature(payload, secret, "sha384"),
			allowed:   allowedAlgos,
			want:      true,
		},
		{
			name:      "valid sha512",
			payload:   payload,
			signature: dchook.GenerateSignature(payload, secret, "sha512"),
			allowed:   allowedAlgos,
			want:      true,
		},
		{
			name:      "wrong secret",
			payload:   payload,
			signature: dchook.GenerateSignature(payload, "wrong-secret", "sha256"),
			allowed:   allowedAlgos,
			want:      false,
		},
		{
			name:      "wrong payload",
			payload:   []byte(`{"different":"data"}`),
			signature: dchook.GenerateSignature(payload, secret, "sha256"),
			allowed:   allowedAlgos,
			want:      false,
		},
		{
			name:      "algorithm not allowed",
			payload:   payload,
			signature: dchook.GenerateSignature(payload, secret, "sha512"),
			allowed:   map[string]bool{"sha256": true},
			want:      false,
		},
		{
			name:      "malformed signature",
			payload:   payload,
			signature: "not-a-valid-signature",
			allowed:   allowedAlgos,
			want:      false,
		},
		{
			name:      "empty signature",
			payload:   payload,
			signature: "",
			allowed:   allowedAlgos,
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dchook.VerifySignature(tt.payload, tt.signature, secret, tt.allowed)
			if got != tt.want {
				t.Errorf("VerifySignature() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFlagValue(t *testing.T) {
	tests := []struct {
		name     string
		flagVal  string
		envVar   string
		envValue string
		flagName string
		want     string
		wantErr  bool
	}{
		{
			name:     "flag takes precedence",
			flagVal:  "from-flag",
			envVar:   "TEST_VAR",
			envValue: "from-env",
			flagName: "-f",
			want:     "from-flag",
			wantErr:  false,
		},
		{
			name:     "env var when flag empty",
			flagVal:  "",
			envVar:   "TEST_VAR",
			envValue: "from-env",
			flagName: "-f",
			want:     "from-env",
			wantErr:  false,
		},
		{
			name:     "error when both empty",
			flagVal:  "",
			envVar:   "NONEXISTENT_VAR",
			envValue: "",
			flagName: "-f",
			want:     "",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue != "" {
				t.Setenv(tt.envVar, tt.envValue)
			}

			got, err := dchook.FlagValue(tt.flagVal, tt.envVar, tt.flagName)
			if (err != nil) != tt.wantErr {
				t.Errorf("FlagValue() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("FlagValue() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsVersionCompatible(t *testing.T) {
	tests := []struct {
		clientVer    string
		serverVer    string
		clientCommit string
		serverCommit string
		want         bool
	}{
		{"dev", "v1.0.0", "abc", "def", true},
		{"v1.0.0", "dev", "abc", "def", true},
		{"v1.0.0", "v1.0.1", "abc", "def", true},
		{"v1.0.0", "v1.1.0", "abc", "def", false},
		{"v1.0.0", "v2.0.0", "abc", "def", false},
		{"v1.1.0", "v1.0.0", "abc", "def", false},
		{"1.0.0", "1.0.1", "abc", "def", true},
		{"invalid", "v1.0.0", "abc", "def", false},
		{"v1", "v1.0.0", "abc", "def", false},
		{"v1.0.0", "v1.0.0", "abc123", "abc123", true},
		{"v1.0.0", "v1.0.0", "abc123", "def456", false},
	}

	for _, tt := range tests {
		t.Run(tt.clientVer+"_"+tt.serverVer, func(t *testing.T) {
			got := dchook.IsVersionCompatible(tt.clientVer, tt.serverVer, tt.clientCommit, tt.serverCommit)
			if got != tt.want {
				t.Errorf("IsVersionCompatible(%q, %q, %q, %q) = %v, want %v",
					tt.clientVer, tt.serverVer, tt.clientCommit, tt.serverCommit, got, tt.want)
			}
		})
	}
}

func TestIsPrintableUTF8(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want bool
	}{
		{"plain text", []byte("hello world"), true},
		{"with tabs", []byte("hello\tworld"), true},
		{"with newlines", []byte("hello\nworld"), true},
		{"with carriage return", []byte("hello\rworld"), true},
		{"emoji", []byte("hello 🚀 world"), true},
		{"null byte", []byte("hello\x00world"), false},
		{"control char", []byte("hello\x01world"), false},
		{"DEL char", []byte("hello\x7fworld"), false},
		{"invalid UTF-8", []byte{0xff, 0xfe}, false},
		{"binary data", []byte{0x00, 0x01, 0x02}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dchook.IsPrintableUTF8(tt.data)
			if got != tt.want {
				t.Errorf("IsPrintableUTF8(%q) = %v, want %v", tt.data, got, tt.want)
			}
		})
	}
}
