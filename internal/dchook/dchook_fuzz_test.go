package dchook_test

import (
	"testing"

	"github.com/halostatue/dchook/internal/dchook"
)

func FuzzVerifySignature(f *testing.F) {
	allowedAlgos := map[string]bool{"sha256": true, "sha384": true, "sha512": true}

	f.Add([]byte("payload"), "sha256:abc123", "secret")
	f.Add([]byte(""), "sha256:", "")
	f.Add([]byte("test"), "invalid", "secret")
	f.Add([]byte("test"), "sha256hash", "secret")
	f.Add([]byte("test"), ":hash", "secret")

	f.Fuzz(func(_ *testing.T, payload []byte, sig, secret string) {
		_ = dchook.VerifySignature(payload, sig, secret, allowedAlgos)
	})
}

func FuzzGenerateSignature(f *testing.F) {
	f.Add([]byte("payload"), "secret", "sha256")
	f.Add([]byte(""), "", "sha256")
	f.Add([]byte("test"), "secret", "invalid")

	f.Fuzz(func(_ *testing.T, payload []byte, secret, algorithm string) {
		_ = dchook.GenerateSignature(payload, secret, algorithm)
	})
}

func FuzzIsVersionCompatible(f *testing.F) {
	f.Add("v1.0.0", "v1.0.0", "abc", "abc")
	f.Add("dev", "v1.0.0", "abc", "def")
	f.Add("v1.0.0", "v2.0.0", "abc", "def")
	f.Add("invalid", "v1.0.0", "abc", "def")

	f.Fuzz(func(_ *testing.T, clientVer, serverVer, clientCommit, serverCommit string) {
		_ = dchook.IsVersionCompatible(clientVer, serverVer, clientCommit, serverCommit)
	})
}

func FuzzIsPrintableUTF8(f *testing.F) {
	f.Add([]byte("plain text"))
	f.Add([]byte("emoji 🚀"))
	f.Add([]byte{0xff, 0xfe})
	f.Add([]byte("\x00\x01"))
	f.Add([]byte("\t\n\r"))

	f.Fuzz(func(_ *testing.T, data []byte) {
		_ = dchook.IsPrintableUTF8(data)
	})
}
