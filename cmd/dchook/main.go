// SPDX-License-Identifier: Apache-2.0
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/abczzz13/clientip"
	"github.com/halostatue/dchook/internal/dchook"
)

var (
	version = "dev"
	commit  = "unknown"

	secretFile        = flag.String("s", "", "Path to webhook secret file")
	composeFile       = flag.String("c", "", "Path to docker-compose.yml")
	bindAddress       = flag.String("b", "", "Bind address")
	port              = flag.String("p", "", "HTTP port to listen on")
	algorithms        = flag.String("algorithms", "sha256,sha384,sha512", "Comma-separated list of allowed HMAC algorithms")
	enableVersionInfo = flag.Bool("enable-version-endpoint", false, "Enable /version endpoint")
	showVersion       = flag.Bool("version", false, "Show version information")
	showHelp          = flag.Bool("help", false, "Show help message")
)

const (
	// Limit request body size (1MB + 256 bytes for envelope overhead)
	maxBodySize = dchook.MaxRequestBodySize
)

func printUsage(w io.Writer) {
	progName := filepath.Base(os.Args[0])
	fmt.Fprintf(w, `Usage: %s [OPTIONS]

Secure webhook receiver for Docker Compose deployments.

Options:
`, progName)
	flag.CommandLine.SetOutput(w)
	flag.PrintDefaults()
	fmt.Fprintf(w, `
Environment Variables:
  DCHOOK_SECRET_FILE         *    Path to webhook secret file
  DCHOOK_COMPOSE_FILE        *    Path to docker-compose.yml to manage
  DCHOOK_BIND_ADDRESS             Bind address (default: 127.0.0.1)
  DCHOOK_PORT                     HTTP port to listen on (default: 7999)
  DCHOOK_ALLOWED_ALGORITHMS       Comma-separated list of allowed HMAC
                                  algorithms (default: sha256,sha384,sha512)

Endpoints:
  POST /deploy    Trigger deployment (requires valid signature)
  GET  /health    Health check (returns 200 OK)
  GET  /version   Version information (only if enabled)

Examples:
  # Using environment variables
  export DCHOOK_SECRET_FILE=/etc/dchook/secret
  export DCHOOK_COMPOSE_FILE=/opt/app/docker-compose.yml
  %s

  # Using flags
  %s -s /etc/dchook/secret -c /opt/app/docker-compose.yml -p 8080

  # Enable version endpoint
  %s -s /etc/dchook/secret -c /opt/app/docker-compose.yml \
  	--enable-version-endpoint
`, progName, progName, progName)
}

func deploy(composeFile string) error {
	log.Println("Starting deployment...")

	// Pull latest images
	pullCmd := exec.Command("docker", "compose", "-f", composeFile, "pull")
	pullCmd.Stdout = os.Stdout
	pullCmd.Stderr = os.Stderr
	if err := pullCmd.Run(); err != nil {
		return fmt.Errorf("pull failed: %w", err)
	}

	// Restart services
	upCmd := exec.Command("docker", "compose", "-f", composeFile, "up", "-d", "--remove-orphans")
	upCmd.Stdout = os.Stdout
	upCmd.Stderr = os.Stderr
	if err := upCmd.Run(); err != nil {
		return fmt.Errorf("up failed: %w", err)
	}

	log.Println("Deployment complete")
	return nil
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
		fmt.Printf("dchook v%s (commit: %s)\n", version, commit)
		os.Exit(0)
	}

	secretFilePath, err := dchook.FlagValue(*secretFile, "DCHOOK_SECRET_FILE", "-s")
	if err != nil {
		log.Fatal(err)
	}

	if runtime.GOOS != "windows" {
		info, err := os.Stat(secretFilePath)
		if err != nil {
			log.Fatalf("Failed to stat webhook secret file: %v", err)
		}
		mode := info.Mode()

		if mode.IsRegular() {
			perm := mode.Perm()
			if perm != 0o600 && perm != 0o400 {
				log.Fatalf("Secret file has insecure permissions: %o (expected 0600 or 0400)", perm)
			}
		} else if mode&os.ModeNamedPipe == 0 {
			log.Fatalf("Secret file must be a regular file or named pipe, got: %s", mode)
		}
	}

	secretBytes, err := os.ReadFile(secretFilePath)
	if err != nil {
		log.Fatalf("Failed to read webhook secret: %v", err)
	}
	secret := strings.TrimSpace(string(secretBytes))

	allowedAlgos, err := dchook.FlagValue(*algorithms, "DCHOOK_ALLOWED_ALGORITHMS", "-a")
	if err != nil {
		allowedAlgos = "sha256,sha384,sha512"
	}

	allowedAlgorithms := make(map[string]bool)
	for _, algo := range strings.Split(allowedAlgos, ",") {
		algo = strings.TrimSpace(algo)
		if algo == "sha256" || algo == "sha384" || algo == "sha512" {
			allowedAlgorithms[algo] = true
		} else {
			log.Fatalf("Invalid algorithm: %s (must be sha256, sha384, or sha512)", algo)
		}
	}

	composeFilePath, err := dchook.FlagValue(*composeFile, "DCHOOK_COMPOSE_FILE", "-c")
	if err != nil {
		log.Fatal(err)
	}

	if _, err := os.Stat(composeFilePath); err != nil {
		log.Fatalf("Compose file not found: %s", composeFilePath)
	}

	if err := exec.Command("docker", "version").Run(); err != nil {
		log.Fatalf("Cannot access docker: %v (ensure docker is running and user has access)", err)
	}

	listenAddr, err := dchook.FlagValue(*bindAddress, "DCHOOK_BIND_ADDRESS", "-b")
	if err != nil {
		listenAddr = "127.0.0.1"
	}

	// Get port (flag overrides env var, defaults to 7999)
	listenPort, err := dchook.FlagValue(*port, "DCHOOK_PORT", "-p")
	if err != nil {
		listenPort = "7999"
	}

	versionEnabled := *enableVersionInfo

	var versionJSON []byte
	if versionEnabled {
		algos := make([]string, 0, len(allowedAlgorithms))
		for algo := range allowedAlgorithms {
			algos = append(algos, algo)
		}
		versionJSON, _ = json.Marshal(map[string]interface{}{
			"version":              version,
			"commit":               commit,
			"supported_algorithms": algos,
		})
	}

	// Initialize IP extractor for rate limiting
	ipExtractor, err := clientip.New(clientip.PresetVMReverseProxy())
	if err != nil {
		log.Fatalf("Failed to create IP extractor: %v", err)
	}

	limiter := dchook.NewRateLimiter(1, time.Minute, 2, time.Hour, 10*time.Minute)

	http.HandleFunc("/deploy", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)

		// Extract real client IP (handles X-Forwarded-For from trusted proxies)
		clientIP, err := ipExtractor.ExtractAddr(r)
		if err != nil {
			log.Printf("Failed to extract client IP: %v, using RemoteAddr", err)
			// Fallback to RemoteAddr
			ip, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				ip = r.RemoteAddr
			}
			clientIP = netip.MustParseAddr(ip)
		}
		ip := clientIP.String()

		if limiter.IsBanned(ip) {
			log.Printf("Banned IP attempted access: %s", ip)
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		// Read payload
		body, err := io.ReadAll(r.Body)
		if err != nil {
			log.Printf("Failed to read body: %v", err)
			limiter.RecordFailure(ip)
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		// Verify signature
		signature := r.Header.Get("Dchook-Signature")
		if !dchook.VerifySignature(body, signature, secret, allowedAlgorithms) {
			log.Printf("Invalid signature from %s", ip)
			limiter.RecordFailure(ip)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Parse envelope
		var envelope struct {
			Dchook struct {
				Version   string `json:"version"`
				Commit    string `json:"commit"`
				Timestamp string `json:"timestamp"`
			} `json:"dchook"`
			Payload interface{} `json:"payload"`
		}
		if err := json.Unmarshal(body, &envelope); err != nil {
			log.Printf("Invalid JSON from %s: %v", ip, err)
			limiter.RecordFailure(ip)
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		// Parse timestamp
		timestamp, err := strconv.ParseInt(envelope.Dchook.Timestamp, 10, 64)
		if err != nil {
			log.Printf("Invalid timestamp from %s: %v", ip, err)
			limiter.RecordFailure(ip)
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		// Check for replay attack
		if !limiter.CheckReplay(timestamp) {
			log.Printf("Replay attack detected from %s (timestamp: %s)", ip, envelope.Dchook.Timestamp)
			limiter.RecordFailure(ip)
			http.Error(w, "Invalid or replayed timestamp", http.StatusBadRequest)
			return
		}

		// Validate version compatibility (major.minor must match, exact version requires matching commit)
		if !dchook.IsVersionCompatible(envelope.Dchook.Version, version, envelope.Dchook.Commit, commit) {
			log.Printf("Version/commit mismatch: client=%s/%s server=%s/%s", envelope.Dchook.Version, envelope.Dchook.Commit, version, commit)
			limiter.RecordFailure(ip)
			http.Error(w, fmt.Sprintf("Version mismatch: server=%s/%s client=%s/%s", version, commit, envelope.Dchook.Version, envelope.Dchook.Commit), http.StatusBadRequest)
			return
		}

		// Check success rate limit
		if !limiter.RecordSuccess(ip) {
			log.Printf("Success rate limit exceeded for %s", ip)
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		log.Printf("Deployment triggered by client v%s (commit: %s)", envelope.Dchook.Version, envelope.Dchook.Commit)

		// Deploy asynchronously
		go func() {
			if err := deploy(composeFilePath); err != nil {
				log.Printf("Deployment failed: %v", err)
			}
		}()

		w.WriteHeader(dchook.DeployAcceptedStatus)
		fmt.Fprintf(w, "Deployment triggered\n")
	})

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "OK\n")
	})

	if versionEnabled {
		http.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write(versionJSON)
		})
	}

	log.Printf("dchook v%s (commit: %s) listening on %s:%s", version, commit, listenAddr, listenPort)
	if err := http.ListenAndServe(listenAddr+":"+listenPort, nil); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
