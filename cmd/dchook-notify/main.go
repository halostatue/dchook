// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/itchyny/gojq"
	flag "github.com/spf13/pflag"

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

	devVersion = "dev"
)

var (
	version = devVersion
	commit  = "unknown"

	url        = flag.StringP("url", "u", "", "Webhook endpoint URL")
	secretFile = flag.StringP("secret-file", "s", "", "Path to webhook secret file")
	algorithm  = flag.StringP(
		"algorithms",
		"a",
		"",
		"Hash algorithm (sha256, sha384, sha512); only the first is used",
	)
	quiet = flag.BoolP(
		"quiet",
		"q",
		false,
		"Quiet mode (suppress output, return only exit code)",
	)
	jsonOutput  = flag.BoolP("json", "j", false, "JSON output mode (machine-readable)")
	tableOutput = flag.BoolP("table", "t", false, "Human-readable table output (status/list only)")
	jqExpr      = flag.String("jq", "", "jq expression to filter JSON response")

	allowDevVersions = flag.Bool(
		"allow-dev-versions",
		false,
		"Allow dev version compatibility bypass",
	)
	showVersion = flag.BoolP("version", "V", false, "Show version information")
	showHelp    = flag.BoolP("help", "h", false, "Show help message")
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

// outputJSON prints JSON data, optionally filtered through a jq expression.
//
//nolint:errcheck // Best-effort output to writer
func outputJSON(w io.Writer, data []byte) {
	if *quiet {
		return
	}

	if *jqExpr == "" {
		fmt.Fprintln(w, string(data))
		return
	}

	query, err := gojq.Parse(*jqExpr)
	if err != nil {
		haltf(exitConfigError, "Error: invalid --jq expression: %v", err)
	}

	var input any
	if err := json.Unmarshal(data, &input); err != nil {
		haltf(exitPayloadError, "Error: response is not valid JSON for --jq filtering: %v", err)
	}

	iter := query.Run(input)
	for {
		v, ok := iter.Next()
		if !ok {
			break
		}
		if err, isErr := v.(error); isErr {
			haltf(exitPayloadError, "Error: --jq evaluation failed: %v", err)
		}

		switch val := v.(type) {
		case string:
			fmt.Fprintln(w, val)
		default:
			out, err := json.Marshal(v)
			if err != nil {
				haltf(exitPayloadError, "Error: --jq result serialization failed: %v", err)
			}
			fmt.Fprintln(w, string(out))
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
		fmt.Fprintf(
			os.Stderr,
			"Warning: implicit deploy subcommand is deprecated; "+
				"use 'dchook-notify deploy' explicitly (will be an error in v2)\n",
		)
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
Note that -q takes precedence over -j, -t, and --jq. --jq implies -j and
overrides -t.

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

	if version == devVersion && !*allowDevVersions {
		haltf(exitConfigError, "Error: dev version requires --allow-dev-versions flag")
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
	// --jq implies JSON output mode
	if *jqExpr != "" {
		outputJSON(os.Stdout, respBody)
		return
	}

	var jsonResp map[string]string
	if json.Unmarshal(respBody, &jsonResp) != nil || jsonResp["deployment_id"] == "" {
		successf("✓ Webhook accepted (status: %d)", resp.StatusCode)
		if len(respBody) > 0 && !*quiet {
			fmt.Printf("Response: %s\n", string(respBody))
		}
		return
	}

	if *jsonOutput {
		fmt.Println(string(respBody))
	} else {
		successf("✓ Webhook accepted (deployment_id: %s)", jsonResp["deployment_id"])
	}
}

func getConfig() (string, string, string) {
	webhookURL, err := dchook.FlagValue(*url, "DCHOOK_URL", "--url/-u")
	if err != nil {
		haltf(exitConfigError, "%v", err)
	}

	// v1.3: Error if URL ends with /deploy
	if strings.HasSuffix(webhookURL, "/deploy/") || strings.HasSuffix(webhookURL, "/deploy") {
		haltf(
			exitConfigError,
			"Error: DCHOOK_URL should be the base URL (e.g., https://example.com), not including /deploy",
		)
	}

	// Strip trailing slash to avoid double slashes when constructing paths
	webhookURL = strings.TrimSuffix(webhookURL, "/")

	secretFilePath, err := dchook.FlagValue(*secretFile, "DCHOOK_SECRET_FILE", "--secret-file/-s")
	if err != nil {
		haltf(exitConfigError, "%v", err)
	}

	secret, err := dchook.ReadSecretFileLax(secretFilePath)
	if err != nil {
		haltf(exitConfigError, "%v", err)
	}

	algo, err := dchook.FlagValue(*algorithm, "DCHOOK_ALGORITHM", "--algorithms/-a")
	if err != nil {
		algo = dchook.AlgorithmSHA256
	}

	// Only the first algorithm is used currently
	if idx := strings.Index(algo, ","); idx >= 0 {
		algo = strings.TrimSpace(algo[:idx])
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

type statusMode int

const (
	modeStatus statusMode = iota
	modeList
)

func makeStatusRequest(endpoint, payload, secret, algo string, mode statusMode) {
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
		if *tableOutput && *jqExpr == "" {
			outputTable(os.Stdout, respBody, mode)
		} else {
			outputJSON(os.Stdout, respBody)
		}
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

type deploymentJSON struct {
	ID        string          `json:"id"`
	Timestamp time.Time       `json:"timestamp"`
	Status    string          `json:"status"`
	Request   json.RawMessage `json:"request,omitempty"`
	Pull      *resultJSON     `json:"pull,omitempty"`
	Restart   *resultJSON     `json:"restart,omitempty"`
}

type resultJSON struct {
	ExitCode   int    `json:"exit_code"`
	Output     string `json:"output"`
	DurationMs int64  `json:"duration_ms"`
}

func outputTable(w io.Writer, data []byte, mode statusMode) {
	if *quiet {
		return
	}

	switch mode {
	case modeStatus:
		outputStatusTable(w, data)
	case modeList:
		outputListTable(w, data)
	}
}

//nolint:errcheck // Best-effort output to writer
func outputStatusTable(w io.Writer, data []byte) {
	var resp struct {
		Deployment deploymentJSON `json:"deployment"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		haltf(exitPayloadError, "Error: failed to parse response for table output: %v", err)
	}

	d := resp.Deployment

	fmt.Fprintf(
		w,
		"%-14s  %-25s  %-11s  %-9s  %s\n",
		"ID", "TIMESTAMP", "STATUS", "PULL", "RESTART",
	)
	fmt.Fprintf(
		w,
		"%-14s  %-25s  %-11s  %-9s  %s\n",
		"──────────────", "─────────────────────────", "───────────", "─────────", "───────",
	)
	fmt.Fprintf(
		w,
		"%-14s  %-25s  %-11s  %-9s  %s\n",
		d.ID,
		d.Timestamp.Format(time.RFC3339),
		d.Status,
		formatResult(d.Pull),
		formatResult(d.Restart),
	)
}

func formatResult(r *resultJSON) string {
	if r == nil {
		return "—"
	}

	return fmt.Sprintf("%s/%d", formatDuration(r.DurationMs), r.ExitCode)
}

//nolint:errcheck // Best-effort output to writer
func outputListTable(w io.Writer, data []byte) {
	var resp struct {
		Deployments []deploymentJSON `json:"deployments"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		haltf(exitPayloadError, "Error: failed to parse response for table output: %v", err)
	}

	if len(resp.Deployments) == 0 {
		fmt.Fprintln(w, "No deployments found.")
		return
	}

	// Print header
	fmt.Fprintf(
		w,
		"%-14s  %-25s  %-11s  %-9s  %s\n",
		"ID", "TIMESTAMP", "STATUS", "PULL", "RESTART",
	)
	fmt.Fprintf(
		w,
		"%-14s  %-25s  %-11s  %-9s  %s\n",
		"──────────────", "─────────────────────────", "───────────", "─────────", "───────",
	)

	for _, d := range resp.Deployments {
		fmt.Fprintf(
			w,
			"%-14s  %-25s  %-11s  %-9s  %s\n",
			d.ID,
			d.Timestamp.Format(time.RFC3339),
			d.Status,
			formatResult(d.Pull),
			formatResult(d.Restart),
		)
	}

	fmt.Fprintf(w, "\n%d deployment(s)\n", len(resp.Deployments))
}

func formatDuration(ms int64) string {
	const (
		msPerSecond  = 1000
		secPerMinute = 60
	)

	if ms < msPerSecond {
		return fmt.Sprintf("%dms", ms)
	}

	seconds := float64(ms) / float64(msPerSecond)
	if seconds < secPerMinute {
		return fmt.Sprintf("%.1fs", seconds)
	}

	minutes := int(seconds) / secPerMinute
	secs := seconds - float64(minutes*secPerMinute)

	return fmt.Sprintf("%dm%.1fs", minutes, secs)
}

func statusCommand(args []string) {
	if len(args) != 1 {
		fmt.Fprintf(os.Stderr, "Usage: dchook-notify status <deployment-id>\n")
		os.Exit(exitConfigError)
	}

	if version == devVersion && !*allowDevVersions {
		haltf(exitConfigError, "Error: dev version requires --allow-dev-versions flag")
	}

	baseURL, secret, algo := getConfig()
	deploymentID := args[0]
	makeStatusRequest(
		baseURL+"/deploy/status/"+deploymentID, deploymentID, secret, algo, modeStatus,
	)
}

func listCommand(args []string) {
	if len(args) != 0 {
		fmt.Fprintf(os.Stderr, "Usage: dchook-notify list\n")
		os.Exit(exitConfigError)
	}

	if version == devVersion && !*allowDevVersions {
		haltf(exitConfigError, "Error: dev version requires --allow-dev-versions flag")
	}

	baseURL, secret, algo := getConfig()
	makeStatusRequest(baseURL+"/deploy/status", "", secret, algo, modeList)
}
