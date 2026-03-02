// SPDX-License-Identifier: Apache-2.0
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/abczzz13/clientip"
	"github.com/halostatue/dchook/internal/dchook"
)

var (
	errComposeSymlink      = errors.New("compose file must not be a symlink")
	errComposeNotAbsolute  = errors.New("compose file must be an absolute path")
	errComposeNotRegular   = errors.New("compose file must be a regular file")
	errProjectInvalidStart = errors.New("project name must start with lowercase letter or digit")
	errProjectInvalidChar  = errors.New("project name contains invalid character")
)

const (
	httpReadTimeout  = 10 * time.Second
	httpWriteTimeout = 10 * time.Second
	httpIdleTimeout  = 60 * time.Second
)

var (
	version = "dev"
	commit  = "unknown"

	// Build-time configurable rate limit for testing, override with
	// `-ldflags="-X main.rateLimitWindow=5s"`.
	rateLimitWindow = "60s"

	secretFile     = flag.String("s", "", "Path to webhook secret file")
	composeFile    = flag.String("c", "", "Path to docker-compose.yml")
	composeProject = flag.String("project", "", "Docker Compose project name")
	bindAddress    = flag.String("b", "", "Bind address")
	port           = flag.String("p", "", "HTTP port to listen on")
	algorithms     = flag.String(
		"algorithms",
		"sha256,sha384,sha512",
		"Comma-separated list of allowed HMAC algorithms",
	)
	showVersion = flag.Bool("version", false, "Show version information")
	showHelp    = flag.Bool("help", false, "Show help message")
)

const (
	deployMaxFailures    = 2
	deployBanDuration    = time.Hour
	statusRateWindow     = 3 * time.Second
	statusMaxRequests    = 15
	statusRateWindow2    = time.Minute
	replayTrackingWindow = 10 * time.Minute
)

func printUsage(w io.Writer) {
	progName := filepath.Base(os.Args[0])
	//nolint:errcheck,gosec // Writing to stderr/stdout
	fmt.Fprintf(w, `Usage: %s [OPTIONS]

Secure webhook receiver for Docker Compose deployments.

Options:
`, progName)
	flag.CommandLine.SetOutput(w)
	flag.PrintDefaults()
	//nolint:errcheck,gosec // Writing to stderr/stdout
	fmt.Fprintf(w, `
Environment Variables:
  DCHOOK_SECRET_FILE         *    Path to webhook secret file
  DCHOOK_COMPOSE_FILE        *    Path to docker-compose.yml to manage
  DCHOOK_COMPOSE_PROJECT          Docker Compose project name
  DCHOOK_BIND_ADDRESS             Bind address (default: 127.0.0.1)
  DCHOOK_PORT                     HTTP port to listen on (default: 7999)
  DCHOOK_ALLOWED_ALGORITHMS       Comma-separated list of allowed HMAC
                                  algorithms (default: sha256,sha384,sha512)

Variables marked with * are required.

Examples:
  # Using environment variables
  export DCHOOK_SECRET_FILE=/etc/dchook/secret
  export DCHOOK_COMPOSE_FILE=/opt/app/docker-compose.yml
  export DCHOOK_COMPOSE_PROJECT=myapp
  %s

  # Using flags
  %s -s /etc/dchook/secret -c /opt/app/docker-compose.yml --project myapp -p 8080
`, progName, progName)
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

	// Initialize structured logger
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	slog.Info("starting dchook", "version", version, "commit", commit)

	secret, err := readSecretFile()
	if err != nil {
		slog.Error("failed to read secret file", "error", err)
		os.Exit(1)
	}

	allowedAlgos, err := dchook.FlagValue(*algorithms, "DCHOOK_ALLOWED_ALGORITHMS", "-a")
	if err != nil {
		allowedAlgos = "sha256,sha384,sha512"
	}

	allowedAlgorithms := make(map[string]bool)
	for algo := range strings.SplitSeq(allowedAlgos, ",") {
		algo = strings.TrimSpace(algo)
		if algo == "sha256" || algo == "sha384" || algo == "sha512" {
			allowedAlgorithms[algo] = true
		} else {
			slog.Error("invalid algorithm", "algorithm", algo)
			os.Exit(1)
		}
	}

	composeFilePath, err := dchook.FlagValue(*composeFile, "DCHOOK_COMPOSE_FILE", "-c")
	if err != nil {
		slog.Error("missing compose file", "error", err)
		os.Exit(1)
	}

	composeFilePath, err = validateComposeFile(composeFilePath)
	if err != nil {
		slog.Error("invalid compose file", "error", err)
		os.Exit(1)
	}

	//nolint:errcheck // Optional
	projectName, _ := dchook.FlagValue(
		*composeProject,
		"DCHOOK_COMPOSE_PROJECT",
		"--project",
	)

	if projectName != "" {
		if err := validateProjectName(projectName); err != nil {
			slog.Error("invalid project name", "error", err)
			os.Exit(1)
		}
	}

	controller := &DockerComposeAdapter{
		ComposeFile: composeFilePath,
		ProjectName: projectName,
	}
	dockerAvailable := true
	if err := controller.Available(); err != nil {
		slog.Warn("docker unavailable, deployments will fail with 503", "error", err)
		dockerAvailable = false
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

	history := NewDeploymentHistory()

	ipExtractor, err := clientip.New(clientip.PresetVMReverseProxy())
	if err != nil {
		slog.Error("failed to create IP extractor", "error", err)
		os.Exit(1)
	}

	rateLimitDuration, err := time.ParseDuration(rateLimitWindow)
	if err != nil {
		slog.Error("invalid rate limit window", "window", rateLimitWindow, "error", err)
		os.Exit(1)
	}

	deployLimiter := dchook.NewRateLimiter(
		1,
		rateLimitDuration,
		deployMaxFailures,
		deployBanDuration,
		replayTrackingWindow,
	)
	statusLimiter := dchook.NewRateLimiter(
		1,
		statusRateWindow,
		statusMaxRequests,
		statusRateWindow2,
		replayTrackingWindow,
	)

	// Create handler configuration
	cfg := &HandlerConfig{
		dockerAvailable:   dockerAvailable,
		ipExtractor:       ipExtractor,
		secret:            secret,
		allowedAlgorithms: allowedAlgorithms,
		adapter:           controller,
		history:           history,
		version:           version,
		commit:            commit,
	}

	// Register handlers (most specific first)
	http.HandleFunc("/deploy/status/", createStatusHandler(cfg, statusLimiter))
	http.HandleFunc("/deploy", createDeployHandler(cfg, deployLimiter))
	http.HandleFunc("/health", createHealthHandler(cfg))

	slog.Info(
		"server starting",
		"version",
		version,
		"commit",
		commit,
		"address",
		listenAddr,
		"port",
		listenPort,
	)

	server := &http.Server{
		Addr:         listenAddr + ":" + listenPort,
		ReadTimeout:  httpReadTimeout,
		WriteTimeout: httpWriteTimeout,
		IdleTimeout:  httpIdleTimeout,
	}

	if err := server.ListenAndServe(); err != nil {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
}

func readSecretFile() (string, error) {
	secretFilePath, err := dchook.FlagValue(*secretFile, "DCHOOK_SECRET_FILE", "-s")
	if err != nil {
		return "", fmt.Errorf("secret file configuration: %w", err)
	}

	secret, err := dchook.ReadSecretFileStrict(secretFilePath)
	if err != nil {
		return "", fmt.Errorf("failed to read secret: %w", err)
	}
	return secret, nil
}

func validateComposeFile(path string) (string, error) {
	// Check if the path is a symlink
	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("failed to stat compose file %q: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("%w: %q", errComposeSymlink, path)
	}

	// Resolve and validate path
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("invalid compose file path %q: %w", path, err)
	}

	if !filepath.IsAbs(resolvedPath) {
		return "", fmt.Errorf("%w: %q", errComposeNotAbsolute, path)
	}

	// Verify it's a regular file
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%w: %q", errComposeNotRegular, path)
	}

	return resolvedPath, nil
}

func validateProjectName(name string) error {
	if name == "" {
		return nil
	}

	// Must start with lowercase letter or digit
	first := rune(name[0])
	if (first < 'a' || first > 'z') && (first < '0' || first > '9') {
		return fmt.Errorf("%w: %q", errProjectInvalidStart, name)
	}

	// Must contain only lowercase letters, digits, dashes, and underscores
	for _, r := range name {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' && r != '_' {
			return fmt.Errorf("%w %q in %q", errProjectInvalidChar, r, name)
		}
	}

	return nil
}
