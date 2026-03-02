// Package dchook provides core functionality for secure webhook handling,
// including HMAC signature verification, rate limiting, and version compatibility checks.
package dchook

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"net/http"
	"os"
	"strings"
)

const (
	// DeployAcceptedStatus is the HTTP status code returned when a deployment is
	// accepted.
	DeployAcceptedStatus = http.StatusAccepted

	// MaxPayloadSize is the maximum size for payload data (1MiB).
	MaxPayloadSize = 1 << 20

	// MaxRequestBodySize is the maximum HTTP request body size (1MiB + 256 bytes for envelope overhead).
	MaxRequestBodySize = MaxPayloadSize + 1<<8

	// AlgorithmSHA256 is used for HMAC-SHA256.
	AlgorithmSHA256 = "sha256"
	// AlgorithmSHA384 is used for HMAC-SHA384.
	AlgorithmSHA384 = "sha384"
	// AlgorithmSHA512 is used for HMAC-SHA512.
	AlgorithmSHA512 = "sha512"

	// signatureParts is the expected number of parts in algorithm:hash format.
	signatureParts = 2
)

// ErrFlagRequired is returned when both flag and environment variable are empty.
var ErrFlagRequired = errors.New("flag or environment variable required")

// parseSignature splits a signature into algorithm and hash parts.
// Returns empty strings if the signature format is invalid.
func parseSignature(signature string) (string, string) {
	parts := strings.SplitN(signature, ":", signatureParts)
	if len(parts) < signatureParts {
		return "", ""
	}
	return parts[0], parts[1]
}

// IsVersionCompatible checks if client and server versions are compatible.
func IsVersionCompatible(clientVer, serverVer, clientCommit, serverCommit string) bool {
	client, err := ParseVersion(clientVer, clientCommit)
	if err != nil {
		return false
	}

	server, err := ParseVersion(serverVer, serverCommit)
	if err != nil {
		return false
	}

	return client.IsCompatible(server)
}

// IsPrintableUTF8 checks if data is valid UTF-8 and contains only printable characters.
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

	return "", fmt.Errorf(
		"%w: %s environment variable or %s flag is required",
		ErrFlagRequired,
		envVar,
		flagName,
	)
}

// GenerateSignature creates an HMAC signature for the payload using the specified algorithm.
// Returns the signature in the format "algorithm:hexhash".
func GenerateSignature(payload []byte, secret, algorithm string) string {
	var mac hash.Hash
	switch algorithm {
	case AlgorithmSHA256:
		mac = hmac.New(sha256.New, []byte(secret))
	case AlgorithmSHA384:
		mac = hmac.New(sha512.New384, []byte(secret))
	case AlgorithmSHA512:
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
func VerifySignature(
	payload []byte,
	signature, secret string,
	allowedAlgorithms map[string]bool,
) bool {
	algorithm, expectedHash := parseSignature(signature)
	if algorithm == "" {
		return false
	}

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
	_, actualHash := parseSignature(actualSignature)
	if actualHash == "" {
		return false
	}

	return hmac.Equal([]byte(expectedHash), []byte(actualHash))
}
