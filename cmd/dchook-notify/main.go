// SPDX-License-Identifier: Apache-2.0
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/halostatue/dchook/internal/dchook"
)

const (
	ExitSuccess = 0

	// Pre-send errors (1-9)
	ExitConfigError  = 1 // Missing URL, secret file, invalid algorithm
	ExitPayloadError = 2 // File errors, too large, invalid format, marshal error
	ExitRequestError = 3 // Request creation or send error

	// Response errors (10+) - map to HTTP status where possible
	ExitBadRequest      = 40 // 400
	ExitUnauthorized    = 41 // 401
	ExitForbidden       = 43 // 403
	ExitPayloadTooLarge = 13 // 413
	ExitRateLimited     = 29 // 429
	ExitServerError     = 50 // 500
	ExitUnknownStatus   = 99 // Other non-202
)

var (
	version = "dev"
	commit  = "unknown"

	url         = flag.String("u", "", "Webhook endpoint URL")
	secretFile  = flag.String("s", "", "Path to webhook secret file")
	algorithm   = flag.String("a", "", "Hash algorithm (sha256, sha384, sha512)")
	quiet       = flag.Bool("q", false, "Quiet mode (suppress output, return only exit code)")
	showVersion = flag.Bool("version", false, "Show version information")
	showHelp    = flag.Bool("help", false, "Show help message")
)

func halt(code int, format string, args ...interface{}) {
	if !*quiet {
		fmt.Fprintf(os.Stderr, format, args...)
		if len(format) > 0 && format[len(format)-1] != '\n' {
			fmt.Fprintln(os.Stderr)
		}
	}
	os.Exit(code)
}

func success(format string, args ...interface{}) {
	if !*quiet {
		fmt.Printf(format, args...)
		if len(format) > 0 && format[len(format)-1] != '\n' {
			fmt.Println()
		}
	}
}

func main() {
	flag.Usage = func() {
		printUsage(os.Stderr)
	}
	flag.Parse()

	if *showHelp {
		printUsage(os.Stdout)
		os.Exit(0)
	}

	if *showVersion {
		fmt.Printf("dchook-notify v%s (commit: %s)\n", version, commit)
		os.Exit(0)
	}

	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(ExitConfigError)
	}

	webhookURL, err := dchook.FlagValue(*url, "DCHOOK_URL", "-u")
	if err != nil {
		halt(ExitConfigError, "%v", err)
	}

	secretFilePath, err := dchook.FlagValue(*secretFile, "DCHOOK_SECRET_FILE", "-s")
	if err != nil {
		halt(ExitConfigError, "%v", err)
	}

	secretBytes, err := os.ReadFile(secretFilePath)
	if err != nil {
		halt(ExitConfigError, "Error reading secret file: %v", err)
	}
	secret := strings.TrimSpace(string(secretBytes))

	algo, err := dchook.FlagValue(*algorithm, "DCHOOK_ALGORITHM", "-a")
	if err != nil {
		algo = "sha256"
	}

	if algo != "sha256" && algo != "sha384" && algo != "sha512" {
		halt(ExitConfigError, "Error: Invalid algorithm '%s' (must be sha256, sha384, or sha512)", algo)
	}

	bodyFile := flag.Arg(0)
	var payloadBody []byte

	if bodyFile == "-" {
		payloadBody, err = io.ReadAll(io.LimitReader(os.Stdin, dchook.MaxPayloadSize+1))
		if err != nil {
			halt(ExitPayloadError, "Error reading stdin: %v", err)
		}
		if len(payloadBody) > dchook.MaxPayloadSize {
			halt(ExitPayloadError, "Error: Stdin payload exceeds 1MiB limit")
		}
	} else {
		info, err := os.Stat(bodyFile)
		if err != nil {
			halt(ExitPayloadError, "Error reading file: %v", err)
		}
		if info.Mode().IsRegular() && info.Size() > dchook.MaxPayloadSize {
			halt(ExitPayloadError, "Error: Payload file too large (%d bytes, max 1MB)", info.Size())
		}

		f, err := os.Open(bodyFile)
		if err != nil {
			halt(ExitPayloadError, "Error opening file: %v", err)
		}
		defer f.Close()

		payloadBody, err = io.ReadAll(io.LimitReader(f, dchook.MaxPayloadSize+1))
		if err != nil {
			halt(ExitPayloadError, "Error reading file: %v", err)
		}
		if len(payloadBody) > dchook.MaxPayloadSize {
			halt(ExitPayloadError, "Error: Payload exceeds 1MiB limit")
		}
	}

	var payload interface{}
	if err := json.Unmarshal(payloadBody, &payload); err != nil {
		if !dchook.IsPrintableUTF8(payloadBody) {
			halt(ExitPayloadError, "Error: Payload must be valid JSON or printable UTF-8 text")
		}
		payload = string(payloadBody)
	}

	envelope := map[string]interface{}{
		"dchook": map[string]interface{}{
			"version":   version,
			"commit":    commit,
			"timestamp": fmt.Sprintf("%d", time.Now().UnixMicro()),
		},
		"payload": payload,
	}

	body, err := json.Marshal(envelope)
	if err != nil {
		halt(ExitPayloadError, "Error marshaling envelope: %v", err)
	}

	signature := dchook.GenerateSignature(body, secret, algo)

	req, err := http.NewRequest("POST", webhookURL, strings.NewReader(string(body)))
	if err != nil {
		halt(ExitRequestError, "Error creating request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Dchook-Signature", signature)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		halt(ExitRequestError, "Error sending webhook: %v", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == dchook.DeployAcceptedStatus {
		success("✓ Webhook accepted (status: %d)", resp.StatusCode)
		if len(respBody) > 0 && !*quiet {
			fmt.Printf("Response: %s\n", string(respBody))
		}
	} else {
		msg := fmt.Sprintf("✗ Webhook rejected (status: %d)", resp.StatusCode)
		if len(respBody) > 0 {
			msg += fmt.Sprintf("\nResponse: %s", string(respBody))
		}

		// Map HTTP status to exit code
		switch resp.StatusCode {
		case 400:
			halt(ExitBadRequest, "%s", msg)
		case 401:
			halt(ExitUnauthorized, "%s", msg)
		case 403:
			halt(ExitForbidden, "%s", msg)
		case 413:
			halt(ExitPayloadTooLarge, "%s", msg)
		case 429:
			halt(ExitRateLimited, "%s", msg)
		case 500:
			halt(ExitServerError, "%s", msg)
		default:
			halt(ExitUnknownStatus, "%s", msg)
		}
	}
}

func printUsage(w io.Writer) {
	progName := filepath.Base(os.Args[0])

	fmt.Fprintf(w, `Usage: %s [OPTIONS] <body-file>

Send authenticated webhook to dchook listener.

Arguments:
  body-file    Path to JSON payload file (use '-' for stdin)

Options:
`, progName)

	flag.CommandLine.SetOutput(w)
	flag.PrintDefaults()

	fmt.Fprintf(w, `
Environment Variables:
  DCHOOK_URL           *    Webhook endpoint URL
  DCHOOK_SECRET_FILE   *    Path to webhook secret file
  DCHOOK_ALGORITHM          Hash algorithm: sha256, sha384, sha512

Variables marked with * are required.

DCHOOK_ALGORITHM defaults to sha256 and must match the server configuration.

Examples:
  # Using environment variables
  echo '{"image":"app:latest"}' | %s -
  %s payload.json

  # Using flags
  %s -u https://hook.example.com/deploy -s /path/to/secret payload.json

  # With password manager (process substitution)
  %s -s <(pass show webhook-secret) payload.json
  %s -s <(op read op://MyServer/DCHook/secret) payload.json
`, progName, progName, progName, progName, progName)
}
