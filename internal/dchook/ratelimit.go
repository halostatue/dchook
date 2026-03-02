package dchook

import (
	"log"
	"sync"
	"time"
)

// RateLimiter tracks request rates and bans for IP addresses.
type RateLimiter struct {
	mutex           sync.Mutex
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

// NewRateLimiter creates a new rate limiter with the specified limits.
func NewRateLimiter(
	successLimit int,
	successWindow time.Duration,
	failLimit int,
	banDuration, replayWindow time.Duration,
) *RateLimiter {
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

// IsBanned checks if an IP is currently banned.
func (limiter *RateLimiter) IsBanned(ipAddress string) bool {
	limiter.mutex.Lock()
	defer limiter.mutex.Unlock()

	if banTime, exists := limiter.bannedUntil[ipAddress]; exists {
		if time.Now().Before(banTime) {
			return true
		}
		// Ban expired, clean up
		delete(limiter.bannedUntil, ipAddress)
		delete(limiter.failedRequests, ipAddress)
	}
	return false
}

// CheckReplay checks if a timestamp has been seen before or is invalid.
func (limiter *RateLimiter) CheckReplay(timestamp int64) bool {
	limiter.mutex.Lock()
	defer limiter.mutex.Unlock()

	now := time.Now()
	requestTime := time.UnixMicro(timestamp)

	// Check if timestamp is too old or in the future
	if requestTime.Before(now.Add(-5*time.Minute)) || requestTime.After(now.Add(1*time.Minute)) {
		return false
	}

	// Check if we've seen this timestamp
	if _, seen := limiter.seenTimestamps[timestamp]; seen {
		return false
	}

	// Clean up old timestamps
	cutoff := now.Add(-limiter.replayWindow)
	for ts, recordedAt := range limiter.seenTimestamps {
		if recordedAt.Before(cutoff) {
			delete(limiter.seenTimestamps, ts)
		}
	}

	// Record this timestamp
	limiter.seenTimestamps[timestamp] = now
	return true
}

// RecordSuccess records a successful request and returns false if rate limit exceeded.
func (limiter *RateLimiter) RecordSuccess(ipAddress string) bool {
	limiter.mutex.Lock()
	defer limiter.mutex.Unlock()

	now := time.Now()
	cutoff := now.Add(-limiter.successWindow)

	// Clean old successful requests
	var recent []time.Time
	for _, t := range limiter.successRequests[ipAddress] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}

	if len(recent) >= limiter.successLimit {
		return false
	}

	recent = append(recent, now)
	limiter.successRequests[ipAddress] = recent

	// Reset failed count on success
	delete(limiter.failedRequests, ipAddress)
	return true
}

// RecordFailure records a failed request and bans the IP if threshold exceeded.
func (limiter *RateLimiter) RecordFailure(ipAddress string) {
	limiter.mutex.Lock()
	defer limiter.mutex.Unlock()

	limiter.failedRequests[ipAddress]++
	if limiter.failedRequests[ipAddress] >= limiter.failLimit {
		limiter.bannedUntil[ipAddress] = time.Now().Add(limiter.banDuration)
		log.Printf(
			"IP %s banned for %v after %d failed attempts",
			ipAddress,
			limiter.banDuration,
			limiter.failedRequests[ipAddress],
		)
	}
}
