// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/halostatue/dchook/internal/dchook"
)

var errPayloadTooLarge = errors.New("payload exceeds 1MiB limit")

const (
	nonceSize = 8

	subcommandDeploy = "deploy"
	subcommandStatus = "status"
	subcommandList   = "list"

	exitSuccess = 0

	exitConfigError  = 1 // Missing URL, secret file, invalid algorithm
	exitPayloadError = 2 // File errors, too large, invalid format, serialization error
	exitRequestError = 3 // Request creation or send error

	exitBadRequest         = 40 // 400
	exitUnauthorized       = 41 // 401
	exitForbidden          = 43 // 403
	exitNotFound           = 44 // 404
	exitPayloadTooLarge    = 13 // 413
	exitRateLimited        = 29 // 429
	exitServerError        = 50 // 500
	exitServiceUnavailable = 53 // 503
	exitUnknownStatus      = 99 // Other non-202
)

var (
	version = "dev"
	commit  = "unknown"

	url         = flag.String("u", "", "Webhook endpoint URL")
	secretFile  = flag.String("s", "", "Path to webhook secret file")
	algorithm   = flag.String("a", "", "Hash algorithm (sha256, sha384, sha512)")
	quiet       = flag.Bool("q", false, "Quiet mode (suppress output, return only exit code)")
	jsonOutput  = flag.Bool("j", false, "JSON output mode (machine-readable)")
	showVersion = flag.Bool("version", false, "Show version information")
	showHelp    = flag.Bool("help", false, "Show help message")
)

func haltf(code int, format string, args ...any) {
	if !*quiet {
		//nolint:gosec
		fmt.Fprintf(os.Stderr, format, args...)
		if len(format) > 0 && format[len(format)-1] != '\n' {
			fmt.Fprintln(os.Stderr)
		}
	}
	os.Exit(code)
}

func successf(format string, args ...any) {
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
		os.Exit(exitSuccess)
	}

	if *showVersion {
		fmt.Printf("dchook-notify v%s (commit: %s)\n", version, commit)
		os.Exit(exitSuccess)
	}

	// Determine subcommand
	args := flag.Args()
	if len(args) == 0 {
		flag.Usage()
		os.Exit(exitConfigError)
	}

	subcommand := args[0]

	if subcommand == subcommandDeploy || subcommand == subcommandStatus ||
		subcommand == subcommandList {
		args = args[1:]
	} else {
		// This will be a warning in version 1.3 and an error in later versions.
		subcommand = subcommandDeploy
	}

	switch subcommand {
	case subcommandDeploy:
		deployCommand(args)
	case subcommandStatus:
		statusCommand(args)
	case subcommandList:
		listCommand(args)
	default:
		fmt.Fprintf(os.Stderr, "Unknown subcommand: %s\n", subcommand)
		flag.Usage()
		os.Exit(exitConfigError)
	}
}

func printUsage(w io.Writer) {
	progName := filepath.Base(os.Args[0])

	//nolint:errcheck,gosec // Writing to stderr/stdout
	fmt.Fprintf(w, `Usage: %s [OPTIONS] [deploy] <payload-file>
       %s [OPTIONS] status <deployment-id>
       %s [OPTIONS] list

Interacts with the configured dchook listener.

Subcommands:
  deploy        Deploys the provided payload file (use '-' for stdin)
  status        Get the JSON status for the provided deployment ID
  list          Returns the JSON list of the most recent ten deployments

Options:
`, progName, progName, progName)

	flag.CommandLine.SetOutput(w)
	flag.PrintDefaults()

	//nolint:errcheck,gosec // Writing to stderr/stdout
	fmt.Fprintf(w, `
Note that -q takes precedence over -j.

Environment Variables:
  DCHOOK_URL           *    Webhook endpoint URL
  DCHOOK_SECRET_FILE   *    Path to webhook secret file
  DCHOOK_ALGORITHM          Hash algorithm: sha256, sha384, sha512
                            (default: sha256)

Variables marked with * are required.

Examples:
  # Deploy with environment variables
  export DCHOOK_URL=https://hook.example.com/deploy
  export DCHOOK_SECRET_FILE=/path/to/secret
  echo '{"image":"app:latest"}' | %s deploy -
  %s deploy payload.json

  # Deploy with flags and JSON output
  %s -u https://hook.example.com/deploy -s /path/to/secret -j deploy payload.json

  # Query deployment status
  %s status abc123def456

  # List recent deployments
  %s list

  # Quiet mode (exit code only)
  %s -q deploy payload.json && echo "Success" || echo "Failed"

  # With password manager (process substitution)
  %s -s <(pass show webhook-secret) deploy payload.json
`, progName, progName, progName, progName, progName, progName, progName)
}

func deployCommand(args []string) {
	if len(args) != 1 {
		fmt.Fprintf(os.Stderr, "Usage: dchook-notify deploy <payload-file>\n")
		os.Exit(exitConfigError)
	}

	baseURL, secret, algo := getConfig()

	bodyFile := args[0]
	payloadBody, err := readPayloadBody(bodyFile)
	if err != nil {
		haltf(exitPayloadError, "%v", err)
	}

	var payload any
	if err := json.Unmarshal(payloadBody, &payload); err != nil {
		if !dchook.IsPrintableUTF8(payloadBody) {
			haltf(exitPayloadError, "Error: Payload must be valid JSON or printable UTF-8 text")
		}
		payload = string(payloadBody)
	}

	envelope := map[string]any{
		"dchook": map[string]any{
			"version":   version,
			"commit":    commit,
			"timestamp": strconv.FormatInt(time.Now().UnixMicro(), 10),
		},
		"payload": payload,
	}

	body, err := json.Marshal(envelope)
	if err != nil {
		haltf(exitPayloadError, "Error serializing envelope: %v", err)
	}

	signature := dchook.GenerateSignature(body, secret, algo)

	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		baseURL+"/deploy",
		strings.NewReader(string(body)),
	)
	if err != nil {
		haltf(exitRequestError, "Error creating request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Dchook-Signature", signature)

	client := &http.Client{}
	resp, err := client.Do(req) //nolint:gosec // Controlled input
	if err != nil {
		haltf(exitRequestError, "Error sending webhook: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck // Best effort close in defer

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		haltf(exitRequestError, "Error reading response: %v", err)
	}

	handleDeployResponse(resp, respBody)
}

func readPayloadBody(bodyFile string) ([]byte, error) {
	if bodyFile == "-" {
		payloadBody, err := io.ReadAll(io.LimitReader(os.Stdin, dchook.MaxPayloadSize+1))
		if err != nil {
			return nil, fmt.Errorf("error reading stdin: %w", err)
		}
		if len(payloadBody) > dchook.MaxPayloadSize {
			return nil, fmt.Errorf("%w (stdin)", errPayloadTooLarge)
		}

		return payloadBody, nil
	}

	info, err := os.Stat(bodyFile)
	if err != nil {
		return nil, fmt.Errorf("error reading file: %w", err)
	}
	if info.Mode().IsRegular() && info.Size() > dchook.MaxPayloadSize {
		return nil, fmt.Errorf("%w: %d bytes (max 1MB)", errPayloadTooLarge, info.Size())
	}

	f, err := os.Open(filepath.Clean(bodyFile))
	if err != nil {
		return nil, fmt.Errorf("error opening file: %w", err)
	}
	defer f.Close() //nolint:errcheck // Best effort close in defer

	payloadBody, err := io.ReadAll(io.LimitReader(f, dchook.MaxPayloadSize+1))
	if err != nil {
		return nil, fmt.Errorf("error reading file: %w", err)
	}
	if len(payloadBody) > dchook.MaxPayloadSize {
		return nil, errPayloadTooLarge
	}

	return payloadBody, nil
}

func handleDeployResponse(resp *http.Response, respBody []byte) {
	if resp.StatusCode == dchook.DeployAcceptedStatus {
		handleAcceptedDeploy(resp, respBody)
	} else {
		msg := "✗ Webhook rejected (status: " + strconv.Itoa(resp.StatusCode) + ")"
		if len(respBody) > 0 {
			msg += "\nResponse: " + string(respBody)
		}

		// Map HTTP status to exit code
		switch resp.StatusCode {
		case http.StatusBadRequest:
			haltf(exitBadRequest, "%s", msg)
		case http.StatusUnauthorized:
			haltf(exitUnauthorized, "%s", msg)
		case http.StatusForbidden:
			haltf(exitForbidden, "%s", msg)
		case http.StatusNotFound:
			haltf(exitNotFound, "%s", msg)
		case http.StatusRequestEntityTooLarge:
			haltf(exitPayloadTooLarge, "%s", msg)
		case http.StatusTooManyRequests:
			haltf(exitRateLimited, "%s", msg)
		case http.StatusInternalServerError:
			haltf(exitServerError, "%s", msg)
		case http.StatusServiceUnavailable:
			haltf(exitServiceUnavailable, "%s", msg)
		default:
			haltf(exitUnknownStatus, "%s", msg)
		}
	}
}

func handleAcceptedDeploy(resp *http.Response, respBody []byte) {
	var jsonResp map[string]string
	if json.Unmarshal(respBody, &jsonResp) != nil || jsonResp["deployment_id"] == "" {
		// No valid deployment_id in response
		successf("✓ Webhook accepted (status: %d)", resp.StatusCode)
		if len(respBody) > 0 && !*quiet {
			fmt.Printf("Response: %s\n", string(respBody))
		}
		return
	}

	deployID := jsonResp["deployment_id"]
	if *jsonOutput {
		fmt.Println(string(respBody))
	} else {
		successf("✓ Webhook accepted (deployment_id: %s)", deployID)
	}
}

func getConfig() (string, string, string) {
	webhookURL, err := dchook.FlagValue(*url, "DCHOOK_URL", "-u")
	if err != nil {
		haltf(exitConfigError, "%v", err)
	}

	// v1.2: Warn and strip /deploy suffix (will be error in v1.3+)
	if strings.HasSuffix(webhookURL, "/deploy/") {
		fmt.Fprintf(
			os.Stderr,
			"Warning: DCHOOK_URL should not end with /deploy/ (will be an error in v1.3+)\n",
		)
		webhookURL = strings.TrimSuffix(webhookURL, "/deploy/")
	} else if strings.HasSuffix(webhookURL, "/deploy") {
		fmt.Fprintf(
			os.Stderr,
			"Warning: DCHOOK_URL should not end with /deploy (will be an error in v1.3+)\n",
		)
		webhookURL = strings.TrimSuffix(webhookURL, "/deploy")
	}

	secretFilePath, err := dchook.FlagValue(*secretFile, "DCHOOK_SECRET_FILE", "-s")
	if err != nil {
		haltf(exitConfigError, "%v", err)
	}

	secret, err := dchook.ReadSecretFileLax(secretFilePath)
	if err != nil {
		haltf(exitConfigError, "%v", err)
	}

	algo, err := dchook.FlagValue(*algorithm, "DCHOOK_ALGORITHM", "-a")
	if err != nil {
		algo = dchook.AlgorithmSHA256
	}

	if algo != dchook.AlgorithmSHA256 && algo != dchook.AlgorithmSHA384 &&
		algo != dchook.AlgorithmSHA512 {
		haltf(
			exitConfigError,
			"Error: Invalid algorithm '%s' (must be %s, %s, or %s)",
			algo,
			dchook.AlgorithmSHA256,
			dchook.AlgorithmSHA384,
			dchook.AlgorithmSHA512,
		)
	}

	return webhookURL, secret, algo
}

func makeStatusRequest(endpoint, payload, secret, algo string) {
	timestamp := strconv.FormatInt(time.Now().UnixMicro(), 10)

	var signaturePayload string
	var nonce string
	if payload != "" {
		signaturePayload = timestamp + ":" + payload
	} else {
		nonceBytes := make([]byte, nonceSize)
		if _, err := rand.Read(nonceBytes); err != nil {
			haltf(exitRequestError, "Error generating nonce: %v", err)
		}
		nonce = hex.EncodeToString(nonceBytes)
		signaturePayload = timestamp + ":" + nonce
	}

	signature := dchook.GenerateSignature([]byte(signaturePayload), secret, algo)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, endpoint, nil)
	if err != nil {
		haltf(exitRequestError, "Error creating request: %v", err)
	}

	req.Header.Set("X-Dchook-Timestamp", timestamp)
	req.Header.Set("X-Dchook-Signature", signature)
	if nonce != "" {
		req.Header.Set("X-Dchook-Nonce", nonce)
	}

	client := &http.Client{}
	resp, err := client.Do(req) //nolint:gosec // Controlled input
	if err != nil {
		haltf(exitRequestError, "Error sending request: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck // Best effort close in defer

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		haltf(exitRequestError, "Error reading response: %v", err)
	}

	if resp.StatusCode == http.StatusOK {
		fmt.Println(string(respBody))
	} else {
		msg := "Request failed (status: " + strconv.Itoa(resp.StatusCode) + ")"
		if len(respBody) > 0 {
			msg += "\n" + string(respBody)
		}

		switch resp.StatusCode {
		case http.StatusUnauthorized:
			haltf(exitUnauthorized, "%s", msg)
		case http.StatusNotFound:
			haltf(exitNotFound, "%s", msg)
		default:
			haltf(exitUnknownStatus, "%s", msg)
		}
	}
}

func statusCommand(args []string) {
	if len(args) != 1 {
		fmt.Fprintf(os.Stderr, "Usage: dchook-notify status <deployment-id>\n")
		os.Exit(exitConfigError)
	}

	baseURL, secret, algo := getConfig()
	deploymentID := args[0]
	makeStatusRequest(baseURL+"/deploy/status/"+deploymentID, deploymentID, secret, algo)
}

func listCommand(args []string) {
	if len(args) != 0 {
		fmt.Fprintf(os.Stderr, "Usage: dchook-notify list\n")
		os.Exit(exitConfigError)
	}

	baseURL, secret, algo := getConfig()
	makeStatusRequest(baseURL+"/deploy/status", "", secret, algo)
}
