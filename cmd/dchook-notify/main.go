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

var (
	version = "dev"
	commit  = "unknown"

	url         = flag.String("u", "", "Webhook endpoint URL")
	secretFile  = flag.String("s", "", "Path to webhook secret file")
	algorithm   = flag.String("a", "", "Hash algorithm (sha256, sha384, sha512)")
	showVersion = flag.Bool("version", false, "Show version information")
	showHelp    = flag.Bool("help", false, "Show help message")
)

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
		os.Exit(1)
	}

	webhookURL, err := dchook.FlagValue(*url, "DCHOOK_URL", "-u")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	secretFilePath, err := dchook.FlagValue(*secretFile, "DCHOOK_SECRET_FILE", "-s")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	secretBytes, err := os.ReadFile(secretFilePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading secret file: %v\n", err)
		os.Exit(1)
	}
	secret := strings.TrimSpace(string(secretBytes))

	algo, err := dchook.FlagValue(*algorithm, "DCHOOK_ALGORITHM", "-a")
	if err != nil {
		algo = "sha256"
	}

	if algo != "sha256" && algo != "sha384" && algo != "sha512" {
		fmt.Fprintf(os.Stderr, "Error: Invalid algorithm '%s' (must be sha256, sha384, or sha512)\n", algo)
		os.Exit(1)
	}

	bodyFile := flag.Arg(0)
	var payloadBody []byte

	if bodyFile == "-" {
		// Read up to MaxPayloadSize + 1 byte to detect oversized input
		payloadBody, err = io.ReadAll(io.LimitReader(os.Stdin, dchook.MaxPayloadSize+1))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading stdin: %v\n", err)
			os.Exit(1)
		}
		if len(payloadBody) > dchook.MaxPayloadSize {
			fmt.Fprintf(os.Stderr, "Error: Stdin payload exceeds 1MiB limit\n")
			os.Exit(1)
		}
	} else {
		info, err := os.Stat(bodyFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading file: %v\n", err)
			os.Exit(1)
		}
		if info.Mode().IsRegular() && info.Size() > dchook.MaxPayloadSize {
			fmt.Fprintf(os.Stderr, "Error: Payload file too large (%d bytes, max 1MB)\n", info.Size())
			os.Exit(1)
		}

		f, err := os.Open(bodyFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error opening file: %v\n", err)
			os.Exit(1)
		}
		defer f.Close()

		// Read up to 1MiB + 1 byte to detect oversized input
		payloadBody, err = io.ReadAll(io.LimitReader(f, dchook.MaxPayloadSize+1))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading file: %v\n", err)
			os.Exit(1)
		}
		if len(payloadBody) > dchook.MaxPayloadSize {
			fmt.Fprintf(os.Stderr, "Error: Payload exceeds 1MiB limit\n")
			os.Exit(1)
		}
	}

	var payload interface{}
	if err := json.Unmarshal(payloadBody, &payload); err != nil {
		if !dchook.IsPrintableUTF8(payloadBody) {
			fmt.Fprintf(os.Stderr, "Error: Payload must be valid JSON or printable UTF-8 text\n")
			os.Exit(1)
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
		fmt.Fprintf(os.Stderr, "Error marshaling envelope: %v\n", err)
		os.Exit(1)
	}

	signature := dchook.GenerateSignature(body, secret, algo)

	req, err := http.NewRequest("POST", webhookURL, strings.NewReader(string(body)))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating request: %v\n", err)
		os.Exit(1)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Dchook-Signature", signature)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error sending webhook: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == dchook.DeployAcceptedStatus {
		fmt.Printf("✓ Webhook accepted (status: %d)\n", resp.StatusCode)
		if len(respBody) > 0 {
			fmt.Printf("Response: %s\n", string(respBody))
		}
	} else {
		fmt.Fprintf(os.Stderr, "✗ Webhook rejected (status: %d)\n", resp.StatusCode)
		if len(respBody) > 0 {
			fmt.Fprintf(os.Stderr, "Response: %s\n", string(respBody))
		}
		os.Exit(1)
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
