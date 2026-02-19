package dchook

import (
	"log"
	"sync"
	"time"
)

// RateLimiter tracks request rates and bans for IP addresses
type RateLimiter struct {
	mu              sync.Mutex
	successRequests map[string][]time.Time
	failedRequests  map[string]int
	bannedUntil     map[string]time.Time
	seenTimestamps  map[int64]time.Time
	successLimit    int
	successWindow   time.Duration
	failLimit       int
	banDuration     time.Duration
	replayWindow    time.Duration
}

// NewRateLimiter creates a new rate limiter with the specified limits
func NewRateLimiter(successLimit int, successWindow time.Duration, failLimit int, banDuration time.Duration, replayWindow time.Duration) *RateLimiter {
	return &RateLimiter{
		successRequests: make(map[string][]time.Time),
		failedRequests:  make(map[string]int),
		bannedUntil:     make(map[string]time.Time),
		seenTimestamps:  make(map[int64]time.Time),
		successLimit:    successLimit,
		successWindow:   successWindow,
		failLimit:       failLimit,
		banDuration:     banDuration,
		replayWindow:    replayWindow,
	}
}

// IsBanned checks if an IP is currently banned
func (rl *RateLimiter) IsBanned(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	if banTime, exists := rl.bannedUntil[ip]; exists {
		if time.Now().Before(banTime) {
			return true
		}
		// Ban expired, clean up
		delete(rl.bannedUntil, ip)
		delete(rl.failedRequests, ip)
	}
	return false
}

// CheckReplay checks if a timestamp has been seen before or is invalid
func (rl *RateLimiter) CheckReplay(timestamp int64) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	requestTime := time.UnixMicro(timestamp)

	// Check if timestamp is too old or in the future
	if requestTime.Before(now.Add(-5*time.Minute)) || requestTime.After(now.Add(1*time.Minute)) {
		return false
	}

	// Check if we've seen this timestamp
	if _, seen := rl.seenTimestamps[timestamp]; seen {
		return false
	}

	// Clean up old timestamps
	cutoff := now.Add(-rl.replayWindow)
	for ts, recordedAt := range rl.seenTimestamps {
		if recordedAt.Before(cutoff) {
			delete(rl.seenTimestamps, ts)
		}
	}

	// Record this timestamp
	rl.seenTimestamps[timestamp] = now
	return true
}

// RecordSuccess records a successful request and returns false if rate limit exceeded
func (rl *RateLimiter) RecordSuccess(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-rl.successWindow)

	// Clean old successful requests
	var recent []time.Time
	for _, t := range rl.successRequests[ip] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}

	if len(recent) >= rl.successLimit {
		return false
	}

	recent = append(recent, now)
	rl.successRequests[ip] = recent

	// Reset failed count on success
	delete(rl.failedRequests, ip)
	return true
}

// RecordFailure records a failed request and bans the IP if threshold exceeded
func (rl *RateLimiter) RecordFailure(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	rl.failedRequests[ip]++
	if rl.failedRequests[ip] >= rl.failLimit {
		rl.bannedUntil[ip] = time.Now().Add(rl.banDuration)
		log.Printf("IP %s banned for %v after %d failed attempts", ip, rl.banDuration, rl.failedRequests[ip])
	}
}
