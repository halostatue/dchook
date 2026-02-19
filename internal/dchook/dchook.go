package dchook

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"hash"
	"net/http"
	"os"
	"strconv"
	"strings"
)

const (
	// DeployAcceptedStatus is the HTTP status code returned when a deployment is accepted
	DeployAcceptedStatus = http.StatusAccepted

	// MaxPayloadSize is the maximum size for payload data (1MiB)
	MaxPayloadSize = 1 << 20

	// MaxRequestBodySize is the maximum HTTP request body size (1MiB + 256 bytes for envelope overhead)
	MaxRequestBodySize = MaxPayloadSize + 1<<8
)

// IsVersionCompatible checks if client and server versions are compatible
func IsVersionCompatible(clientVer, serverVer, clientCommit, serverCommit string) bool {
	// "dev" versions are always compatible
	if clientVer == "dev" || serverVer == "dev" {
		return true
	}

	// Parse versions (expecting vX.Y.Z or X.Y.Z)
	clientParts := strings.TrimPrefix(clientVer, "v")
	serverParts := strings.TrimPrefix(serverVer, "v")

	clientSegs := strings.Split(clientParts, ".")
	serverSegs := strings.Split(serverParts, ".")

	if len(clientSegs) < 2 || len(serverSegs) < 2 {
		return false
	}

	// Compare major.minor
	clientMajor, err1 := strconv.Atoi(clientSegs[0])
	clientMinor, err2 := strconv.Atoi(clientSegs[1])
	serverMajor, err3 := strconv.Atoi(serverSegs[0])
	serverMinor, err4 := strconv.Atoi(serverSegs[1])

	if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
		return false
	}

	// Major.minor must match
	if clientMajor != serverMajor || clientMinor != serverMinor {
		return false
	}

	// If versions match exactly, commits must also match
	if clientVer == serverVer && clientCommit != serverCommit {
		return false
	}

	return true
}

// IsPrintableUTF8 checks if data is valid UTF-8 and contains only printable characters
func IsPrintableUTF8(data []byte) bool {
	s := string(data)
	for _, r := range s {
		if r == '\ufffd' || (r < 32 && r != '\t' && r != '\n' && r != '\r') || r == 127 {
			return false
		}
	}
	return true
}

// FlagValue returns the value from flag if non-empty, otherwise from environment variable.
// Returns an error if neither is set.
func FlagValue(flagVal, envVar, flagName string) (string, error) {
	if flagVal != "" {
		return flagVal, nil
	}

	if envVal := os.Getenv(envVar); envVal != "" {
		return envVal, nil
	}

	return "", fmt.Errorf("%s environment variable or %s flag is required", envVar, flagName)
}

// GenerateSignature creates an HMAC signature for the payload using the specified algorithm.
// Returns the signature in the format "algorithm:hexhash".
func GenerateSignature(payload []byte, secret string, algorithm string) string {
	var mac hash.Hash
	switch algorithm {
	case "sha256":
		mac = hmac.New(sha256.New, []byte(secret))
	case "sha384":
		mac = hmac.New(sha512.New384, []byte(secret))
	case "sha512":
		mac = hmac.New(sha512.New, []byte(secret))
	default:
		return ""
	}

	mac.Write(payload)
	hashHex := hex.EncodeToString(mac.Sum(nil))

	return fmt.Sprintf("%s:%s", algorithm, hashHex)
}

// VerifySignature checks if the signature matches the payload using constant-time comparison.
// Only algorithms in allowedAlgorithms are accepted.
func VerifySignature(payload []byte, signature string, secret string, allowedAlgorithms map[string]bool) bool {
	parts := strings.SplitN(signature, ":", 2)
	if len(parts) != 2 {
		return false
	}

	algorithm := parts[0]
	expectedHash := parts[1]

	// Check if algorithm is allowed
	if !allowedAlgorithms[algorithm] {
		return false
	}

	// Generate signature and compare
	actualSignature := GenerateSignature(payload, secret, algorithm)
	if actualSignature == "" {
		return false
	}

	// Extract hash from generated signature
	actualParts := strings.SplitN(actualSignature, ":", 2)
	if len(actualParts) != 2 {
		return false
	}
	actualHash := actualParts[1]

	return hmac.Equal([]byte(expectedHash), []byte(actualHash))
}
