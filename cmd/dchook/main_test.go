package main

import (
	"testing"

	"github.com/halostatue/dchook/internal/dchook"
)

func TestIsVersionCompatible(t *testing.T) {
	tests := []struct {
		clientVer    string
		serverVer    string
		clientCommit string
		serverCommit string
		want         bool
	}{
		{"dev", "v1.0.0", "abc", "def", true},
		{"v1.0.0", "dev", "abc", "def", true},
		{"v1.0.0", "v1.0.1", "abc", "def", true},
		{"v1.0.0", "v1.1.0", "abc", "def", false},
		{"v1.0.0", "v2.0.0", "abc", "def", false},
		{"v1.1.0", "v1.0.0", "abc", "def", false},
		{"1.0.0", "1.0.1", "abc", "def", true},
		{"invalid", "v1.0.0", "abc", "def", false},
		{"v1", "v1.0.0", "abc", "def", false},
		// Exact version match requires matching commit
		{"v1.0.0", "v1.0.0", "abc123", "abc123", true},
		{"v1.0.0", "v1.0.0", "abc123", "def456", false},
	}

	for _, tt := range tests {
		t.Run(tt.clientVer+"_"+tt.serverVer+"_"+tt.clientCommit[:3], func(t *testing.T) {
			got := dchook.IsVersionCompatible(tt.clientVer, tt.serverVer, tt.clientCommit, tt.serverCommit)
			if got != tt.want {
				t.Errorf("IsVersionCompatible(%q, %q, %q, %q) = %v, want %v",
					tt.clientVer, tt.serverVer, tt.clientCommit, tt.serverCommit, got, tt.want)
			}
		})
	}
}
