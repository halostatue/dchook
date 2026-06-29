package main

import (
	"testing"

	"github.com/halostatue/dchook/internal/dchook"
)

const (
	abc    = "abc"
	abc123 = "abc123"
	def    = "def"
	def456 = "def456"
)

func TestIsVersionCompatible(t *testing.T) {
	t.Parallel()

	tests := []struct {
		clientVer    string
		serverVer    string
		clientCommit string
		serverCommit string
		allowDev     bool
		want         bool
	}{
		{"dev", "v1.0.0", abc, def, true, true},
		{"dev", "v1.0.0", abc, def, false, false},
		{"v1.0.0", "dev", abc, def, true, true},
		{"v1.0.0", "dev", abc, def, false, false},
		{"v1.0.0", "v1.0.1", abc, def, false, true},
		{"v1.0.0", "v1.1.0", abc, def, false, false},
		{"v1.0.0", "v2.0.0", abc, def, false, false},
		{"v1.1.0", "v1.0.0", abc, def, false, false},
		{"1.0.0", "1.0.1", abc, def, false, true},
		{"invalid", "v1.0.0", abc, def, false, false},
		{"v1", "v1.0.0", abc, def, false, false},
		// Exact version match requires matching commit
		{"v1.0.0", "v1.0.0", abc123, abc123, false, true},
		{"v1.0.0", "v1.0.0", abc123, def456, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.clientVer+"_"+tt.serverVer+"_"+tt.clientCommit[:3], func(t *testing.T) {
			t.Parallel()
			got := dchook.IsVersionCompatible(
				tt.clientVer,
				tt.serverVer,
				tt.clientCommit,
				tt.serverCommit,
				tt.allowDev,
			)
			if got != tt.want {
				t.Errorf(
					"IsVersionCompatible(%q, %q, %q, %q, %v) = %v, want %v",
					tt.clientVer,
					tt.serverVer,
					tt.clientCommit,
					tt.serverCommit,
					tt.allowDev,
					got,
					tt.want,
				)
			}
		})
	}
}
