package dchook_test

import (
	"testing"
	"time"

	"github.com/halostatue/dchook/internal/dchook"
)

func TestRateLimiterSuccess(t *testing.T) {
	rl := dchook.NewRateLimiter(2, time.Second, 2, time.Hour, 10*time.Minute)

	// First success should be allowed
	if !rl.RecordSuccess("127.0.0.1") {
		t.Error("First success should be allowed")
	}

	// Second success should be allowed
	if !rl.RecordSuccess("127.0.0.1") {
		t.Error("Second success should be allowed")
	}

	// Third should be blocked (limit is 2)
	if rl.RecordSuccess("127.0.0.1") {
		t.Error("Third success should be blocked")
	}

	// Different IP should be allowed
	if !rl.RecordSuccess("192.168.1.1") {
		t.Error("Different IP should be allowed")
	}
}

func TestRateLimiterBan(t *testing.T) {
	rl := dchook.NewRateLimiter(1, time.Minute, 2, time.Hour, 10*time.Minute)

	ip := "10.0.0.1"

	// Should not be banned initially
	if rl.IsBanned(ip) {
		t.Error("IP should not be banned initially")
	}

	// First failure
	rl.RecordFailure(ip)
	if rl.IsBanned(ip) {
		t.Error("IP should not be banned after 1 failure")
	}

	// Second failure should trigger ban
	rl.RecordFailure(ip)
	if !rl.IsBanned(ip) {
		t.Error("IP should be banned after 2 failures")
	}
}

func TestRateLimiterSuccessResetsFails(t *testing.T) {
	rl := dchook.NewRateLimiter(1, time.Minute, 2, time.Hour, 10*time.Minute)

	ip := "10.0.0.2"

	// One failure
	rl.RecordFailure(ip)

	// Success should reset failure count
	if !rl.RecordSuccess(ip) {
		t.Error("Success should be allowed")
	}

	// Another failure - should not ban (count was reset)
	rl.RecordFailure(ip)
	if rl.IsBanned(ip) {
		t.Error("IP should not be banned - failure count was reset by success")
	}
}

func TestCheckReplay(t *testing.T) {
	rl := dchook.NewRateLimiter(1, time.Minute, 2, time.Hour, 10*time.Minute)

	now := time.Now()

	// Valid timestamp should be accepted
	validTS := now.UnixMicro()
	if !rl.CheckReplay(validTS) {
		t.Error("Valid timestamp should be accepted")
	}

	// Same timestamp should be rejected (replay)
	if rl.CheckReplay(validTS) {
		t.Error("Duplicate timestamp should be rejected")
	}

	// Old timestamp should be rejected
	oldTS := now.Add(-10 * time.Minute).UnixMicro()
	if rl.CheckReplay(oldTS) {
		t.Error("Old timestamp should be rejected")
	}

	// Future timestamp should be rejected
	futureTS := now.Add(2 * time.Minute).UnixMicro()
	if rl.CheckReplay(futureTS) {
		t.Error("Future timestamp should be rejected")
	}
}
