package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/abczzz13/clientip"
	"github.com/halostatue/dchook/internal/dchook"
)

// HandlerConfig contains shared configuration for HTTP handlers.
type HandlerConfig struct {
	dockerAvailable   bool
	ipExtractor       *clientip.Extractor
	secret            string
	allowedAlgorithms map[string]bool
	adapter           ContainerAdapter
	history           *DeploymentHistory
	version           string
	commit            string
}

func extractClientIP(extractor *clientip.Extractor, r *http.Request) string {
	clientIP, err := extractor.ExtractAddr(r)
	if err != nil {
		//nolint:gosec // slog does not have log injection
		slog.Warn(
			"failed to extract client IP, using RemoteAddr",
			"error", err,
			"remote_addr", r.RemoteAddr,
		)
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			ip = r.RemoteAddr
		}
		clientIP = netip.MustParseAddr(ip)
	}
	return clientIP.String()
}

func createDeployHandler(
	cfg *HandlerConfig,
	limiter *dchook.RateLimiter,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Exact path match
		if r.URL.Path != "/deploy" {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}

		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if !cfg.dockerAvailable {
			http.Error(
				w,
				"Service unavailable: Docker not accessible",
				http.StatusServiceUnavailable,
			)
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, dchook.MaxRequestBodySize)

		ip := extractClientIP(cfg.ipExtractor, r)

		if limiter.IsBanned(ip) {
			//nolint:gosec // slog does not have log injection
			slog.Warn("banned IP attempted access", "ip", ip)
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		// Read payload
		body, err := io.ReadAll(r.Body)
		if err != nil {
			//nolint:gosec // slog does not have log injection
			slog.Warn("failed to read request body", "ip", ip, "error", err)
			limiter.RecordFailure(ip)
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		// Verify signature
		signature := r.Header.Get("Dchook-Signature")
		if !dchook.VerifySignature(body, signature, cfg.secret, cfg.allowedAlgorithms) {
			//nolint:gosec // slog does not have taint injection
			slog.Warn("invalid signature", "ip", ip)
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
			Payload any `json:"payload"`
		}
		if err := json.Unmarshal(body, &envelope); err != nil {
			//nolint:gosec // slog does not have taint injection
			slog.Warn("invalid JSON payload", "ip", ip, "error", err)
			limiter.RecordFailure(ip)
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		// Parse timestamp
		timestamp, err := strconv.ParseInt(envelope.Dchook.Timestamp, 10, 64)
		if err != nil {
			//nolint:gosec // slog does not have taint injection
			slog.Warn(
				"invalid timestamp",
				"ip",
				ip,
				"timestamp",
				envelope.Dchook.Timestamp,
				"error",
				err,
			)
			limiter.RecordFailure(ip)
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		// Check for replay attack
		if !limiter.CheckReplay(timestamp) {
			//nolint:gosec // slog does not have taint injection
			slog.Warn("replay attack detected", "ip", ip, "timestamp", envelope.Dchook.Timestamp)
			limiter.RecordFailure(ip)
			http.Error(w, "Invalid or replayed timestamp", http.StatusBadRequest)
			return
		}

		// Validate version compatibility
		if !dchook.IsVersionCompatible(
			envelope.Dchook.Version,
			cfg.version,
			envelope.Dchook.Commit,
			cfg.commit,
		) {
			slog.Warn(
				"version mismatch",
				"client_version",
				envelope.Dchook.Version,
				"client_commit",
				envelope.Dchook.Commit,
				"server_version",
				cfg.version,
				"server_commit",
				cfg.commit,
			)
			limiter.RecordFailure(ip)
			http.Error(
				w,
				fmt.Sprintf(
					"Version mismatch: server=%s/%s client=%s/%s",
					cfg.version,
					cfg.commit,
					envelope.Dchook.Version,
					envelope.Dchook.Commit,
				),
				http.StatusBadRequest,
			)
			return
		}

		// Check success rate limit
		if !limiter.RecordSuccess(ip) {
			//nolint:gosec // slog does not have taint injection
			slog.Warn("rate limit exceeded", "ip", ip)
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		//nolint:gosec // slog does not have taint injection
		slog.Info(
			"deployment triggered",
			"client_version",
			envelope.Dchook.Version,
			"client_commit",
			envelope.Dchook.Commit,
			"ip",
			ip,
		)

		// Generate deployment ID
		deploymentID := generateDeploymentID()
		deployment := Deployment{
			ID:        deploymentID,
			Timestamp: time.Now(),
			Status:    statusPending,
			Request:   json.RawMessage(body),
		}

		// Add to history immediately so it's queryable
		cfg.history.Add(deployment)

		// Deploy asynchronously
		cfg.adapter.Deploy(&deployment, cfg.history)

		// Check Accept header for JSON response
		acceptJSON := slices.Contains(r.Header["Accept"], "application/json")

		w.WriteHeader(dchook.DeployAcceptedStatus)
		if acceptJSON {
			w.Header().Set("Content-Type", "application/json")
			response := map[string]string{
				"deployment_id": deploymentID,
				"message":       "Deployment triggered",
			}
			if err := json.NewEncoder(w).Encode(response); err != nil {
				slog.Error("failed to encode JSON response", "error", err)
			}
		} else {
			if _, err := fmt.Fprintf(w, "Deployment triggered\n"); err != nil {
				slog.Error("failed to write response", "error", err)
			}
		}
	}
}

func createStatusHandler(
	cfg *HandlerConfig,
	limiter *dchook.RateLimiter,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		ip := extractClientIP(cfg.ipExtractor, r)

		// Rate limiting
		if !limiter.RecordSuccess(ip) {
			//nolint:gosec // slog does not have taint injection
			slog.Warn("status request rate limited", "ip", ip)
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		// Extract deployment ID from path
		path := strings.TrimPrefix(r.URL.Path, "/deploy/status/")

		// Validate path - should be empty (list) or a single ID (no extra slashes)
		if strings.Contains(path, "/") {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}

		if path == "" {
			handleListDeployments(w, r, cfg, limiter)
			return
		}

		// Get specific deployment
		handleGetDeployment(w, r, path, cfg, limiter)
	}
}

func handleListDeployments(w http.ResponseWriter, r *http.Request, cfg *HandlerConfig, limiter *dchook.RateLimiter) {
	timestamp := r.Header.Get("X-Dchook-Timestamp")
	signature := r.Header.Get("X-Dchook-Signature")
	nonce := r.Header.Get("X-Dchook-Nonce")

	if timestamp == "" || signature == "" {
		http.Error(w, "Missing authentication headers", http.StatusUnauthorized)
		return
	}

	// Verify signature of timestamp:nonce
	payload := timestamp
	if nonce != "" {
		payload = timestamp + ":" + nonce
	}

	if !dchook.VerifySignature(
		[]byte(payload),
		signature,
		cfg.secret,
		cfg.allowedAlgorithms,
	) {
		http.Error(w, "Invalid signature", http.StatusUnauthorized)
		return
	}

	// Validate timestamp
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || !limiter.CheckReplay(ts) {
		http.Error(w, "Invalid or expired timestamp", http.StatusUnauthorized)
		return
	}

	deployments := cfg.history.List()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{
		"dchook": map[string]string{
			"version": cfg.version,
			"commit":  cfg.commit,
		},
		"deployments": deployments,
	}); err != nil {
		slog.Error("failed to encode JSON response", "error", err)
	}
}

func handleGetDeployment(w http.ResponseWriter, r *http.Request, deploymentID string, cfg *HandlerConfig, limiter *dchook.RateLimiter) {
	timestamp := r.Header.Get("X-Dchook-Timestamp")
	signature := r.Header.Get("X-Dchook-Signature")

	if timestamp == "" || signature == "" {
		http.Error(w, "Missing authentication headers", http.StatusUnauthorized)
		return
	}

	// Verify signature of timestamp:deploymentID
	payload := timestamp + ":" + deploymentID
	if !dchook.VerifySignature([]byte(payload), signature, cfg.secret, cfg.allowedAlgorithms) {
		http.Error(w, "Invalid signature", http.StatusUnauthorized)
		return
	}

	// Validate timestamp
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || !limiter.CheckReplay(ts) {
		http.Error(w, "Invalid or expired timestamp", http.StatusUnauthorized)
		return
	}

	deployment, found := cfg.history.Get(deploymentID)
	if !found {
		http.Error(w, "Deployment not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{
		"dchook": map[string]string{
			"version": cfg.version,
			"commit":  cfg.commit,
		},
		"deployment": deployment,
	}); err != nil {
		slog.Error("failed to encode JSON response", "error", err)
	}
}

func createHealthHandler(cfg *HandlerConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Exact path match
		if r.URL.Path != "/health" {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}

		success, failure := cfg.history.Stats()
		lastDeploy := cfg.history.LastDeployment()

		response := map[string]any{
			"docker_available":    cfg.dockerAvailable,
			"deployments_success": success,
			"deployments_failure": failure,
		}

		if lastDeploy != nil {
			response["last_deployment"] = lastDeploy.Format(time.RFC3339)
		}

		if cfg.dockerAvailable {
			response["status"] = "ok"
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			if err := json.NewEncoder(w).Encode(response); err != nil {
				slog.Error("failed to encode JSON response", "error", err)
			}
		} else {
			response["status"] = "degraded"
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			if err := json.NewEncoder(w).Encode(response); err != nil {
				slog.Error("failed to encode JSON response", "error", err)
			}
		}
	}
}
