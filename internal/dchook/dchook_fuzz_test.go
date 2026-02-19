package dchook_test

import (
	"testing"

	"github.com/halostatue/dchook/internal/dchook"
)

func FuzzVerifySignature(f *testing.F) {
	allowedAlgos := map[string]bool{"sha256": true, "sha384": true, "sha512": true}

	// Seed with valid and edge cases
	f.Add([]byte("payload"), "sha256:abc123", "secret")
	f.Add([]byte(""), "sha256:", "")
	f.Add([]byte("test"), "invalid", "secret")
	f.Add([]byte("test"), "sha256", "secret") // missing colon
	f.Add([]byte("test"), ":hash", "secret")  // missing algorithm

	f.Fuzz(func(t *testing.T, payload []byte, sig, secret string) {
		// Should never panic, always return bool
		_ = dchook.VerifySignature(payload, sig, secret, allowedAlgos)
	})
}

func FuzzGenerateSignature(f *testing.F) {
	// Seed with various inputs
	f.Add([]byte("payload"), "secret", "sha256")
	f.Add([]byte(""), "", "sha256")
	f.Add([]byte("test"), "secret", "invalid")

	f.Fuzz(func(t *testing.T, payload []byte, secret, algorithm string) {
		// Should never panic, returns empty string for invalid algorithm
		_ = dchook.GenerateSignature(payload, secret, algorithm)
	})
}

func FuzzFlagValue(f *testing.F) {
	// Seed with various inputs
	f.Add("flag-value", "ENV_VAR", "-f")
	f.Add("", "ENV_VAR", "-f")

	f.Fuzz(func(t *testing.T, flagVal, envVar, flagName string) {
		// Should never panic, returns error if both empty
		_, _ = dchook.FlagValue(flagVal, envVar, flagName)
	})
}

func FuzzIsVersionCompatible(f *testing.F) {
	// Seed with various version combinations
	f.Add("v1.0.0", "v1.0.0", "abc", "abc")
	f.Add("dev", "v1.0.0", "abc", "def")
	f.Add("v1.0.0", "v2.0.0", "abc", "def")
	f.Add("invalid", "v1.0.0", "abc", "def")

	f.Fuzz(func(t *testing.T, clientVer, serverVer, clientCommit, serverCommit string) {
		// Should never panic, always return bool
		_ = dchook.IsVersionCompatible(clientVer, serverVer, clientCommit, serverCommit)
	})
}

func FuzzIsPrintableUTF8(f *testing.F) {
	// Seed with various inputs
	f.Add([]byte("plain text"))
	f.Add([]byte("emoji 🚀"))
	f.Add([]byte{0xff, 0xfe})
	f.Add([]byte("\x00\x01"))
	f.Add([]byte("\t\n\r"))

	f.Fuzz(func(t *testing.T, data []byte) {
		// Should never panic, always return bool
		_ = dchook.IsPrintableUTF8(data)
	})
}
