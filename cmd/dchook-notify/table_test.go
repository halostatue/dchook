// SPDX-License-Identifier: Apache-2.0
package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestFormatDuration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ms   int64
		want string
	}{
		{"zero", 0, "0ms"},
		{"milliseconds", 500, "500ms"},
		{"one second", 1000, "1.0s"},
		{"seconds", 3500, "3.5s"},
		{"under a minute", 59999, "60.0s"},
		{"one minute", 60000, "1m0.0s"},
		{"minutes and seconds", 90500, "1m30.5s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := formatDuration(tt.ms)
			if got != tt.want {
				t.Errorf("formatDuration(%d) = %q, want %q", tt.ms, got, tt.want)
			}
		})
	}
}

func TestOutputStatusTable(t *testing.T) {
	t.Parallel()

	t.Run("complete deployment", func(t *testing.T) {
		t.Parallel()

		data := []byte(`{
			"dchook": {"version": "1.3.2", "commit": "abc123"},
			"deployment": {
				"id": "abc123def456",
				"timestamp": "2025-01-01T12:00:00Z",
				"status": "complete",
				"pull": {"exit_code": 0, "output": "Pulled image", "duration_ms": 2500},
				"restart": {"exit_code": 0, "output": "Restarted services", "duration_ms": 1200}
			}
		}`)

		var buf bytes.Buffer
		outputStatusTable(&buf, data)
		output := buf.String()

		if !strings.Contains(output, "ID") {
			t.Error("output missing header")
		}
		if !strings.Contains(output, "PULL") {
			t.Error("output missing PULL header")
		}
		if !strings.Contains(output, "RESTART") {
			t.Error("output missing RESTART header")
		}
		if !strings.Contains(output, "abc123def456") {
			t.Error("output missing deployment ID")
		}
		if !strings.Contains(output, "complete") {
			t.Error("output missing status")
		}
		if !strings.Contains(output, "2.5s/0") {
			t.Error("output missing pull result")
		}
		if !strings.Contains(output, "1.2s/0") {
			t.Error("output missing restart result")
		}
		if strings.Contains(output, "Pulled image") {
			t.Error("output should not contain command output in table mode")
		}
	})

	t.Run("pending deployment", func(t *testing.T) {
		t.Parallel()

		data := []byte(`{
			"dchook": {"version": "1.3.2", "commit": "abc123"},
			"deployment": {
				"id": "pending123",
				"timestamp": "2025-01-01T12:00:00Z",
				"status": "pending"
			}
		}`)

		var buf bytes.Buffer
		outputStatusTable(&buf, data)
		output := buf.String()

		if !strings.Contains(output, "pending123") {
			t.Error("output missing deployment ID")
		}
		if !strings.Contains(output, "pending") {
			t.Error("output missing status")
		}
		// Pull and Restart should show "—" for pending
		lines := strings.Split(output, "\n")
		dataLine := lines[2] // header, separator, data
		if !strings.Contains(dataLine, "—") {
			t.Error("pending deployment should show — for missing results")
		}
	})

	t.Run("failed restart", func(t *testing.T) {
		t.Parallel()

		data := []byte(`{
			"dchook": {"version": "1.3.2", "commit": "abc123"},
			"deployment": {
				"id": "fail789",
				"timestamp": "2025-01-01T13:00:00Z",
				"status": "failed",
				"pull": {"exit_code": 0, "output": "", "duration_ms": 1800},
				"restart": {"exit_code": 1, "output": "container exited", "duration_ms": 500}
			}
		}`)

		var buf bytes.Buffer
		outputStatusTable(&buf, data)
		output := buf.String()

		if !strings.Contains(output, "fail789") {
			t.Error("output missing deployment ID")
		}
		if !strings.Contains(output, "failed") {
			t.Error("output missing failed status")
		}
		if !strings.Contains(output, "1.8s/0") {
			t.Error("output missing successful pull result")
		}
		if !strings.Contains(output, "500ms/1") {
			t.Error("output missing failed restart result")
		}
		if strings.Contains(output, "container exited") {
			t.Error("output should not contain command output in table mode")
		}
	})
}

func TestOutputListTable(t *testing.T) {
	t.Parallel()

	t.Run("multiple deployments", func(t *testing.T) {
		t.Parallel()

		data := []byte(`{
			"dchook": {"version": "1.3.2", "commit": "abc123"},
			"deployments": [
				{
					"id": "deploy1",
					"timestamp": "2025-01-02T12:00:00Z",
					"status": "complete",
					"pull": {"exit_code": 0, "output": "", "duration_ms": 1500},
					"restart": {"exit_code": 0, "output": "", "duration_ms": 800}
				},
				{
					"id": "deploy2",
					"timestamp": "2025-01-01T12:00:00Z",
					"status": "failed",
					"pull": {"exit_code": 1, "output": "error", "duration_ms": 3000}
				}
			]
		}`)

		var buf bytes.Buffer
		outputListTable(&buf, data)
		output := buf.String()

		if !strings.Contains(output, "ID") {
			t.Error("output missing header")
		}
		if !strings.Contains(output, "PULL") {
			t.Error("output missing PULL header")
		}
		if !strings.Contains(output, "RESTART") {
			t.Error("output missing RESTART header")
		}
		if !strings.Contains(output, "deploy1") {
			t.Error("output missing first deployment")
		}
		if !strings.Contains(output, "deploy2") {
			t.Error("output missing second deployment")
		}
		if !strings.Contains(output, "complete") {
			t.Error("output missing complete status")
		}
		if !strings.Contains(output, "failed") {
			t.Error("output missing failed status")
		}
		if !strings.Contains(output, "1.5s/0") {
			t.Error("output missing pull result for deploy1")
		}
		if !strings.Contains(output, "800ms/0") {
			t.Error("output missing restart result for deploy1")
		}
		if !strings.Contains(output, "3.0s/1") {
			t.Error("output missing pull result for deploy2 (failed)")
		}
		if !strings.Contains(output, "2 deployment(s)") {
			t.Error("output missing deployment count")
		}
	})

	t.Run("empty list", func(t *testing.T) {
		t.Parallel()

		data := []byte(`{
			"dchook": {"version": "1.3.2", "commit": "abc123"},
			"deployments": []
		}`)

		var buf bytes.Buffer
		outputListTable(&buf, data)
		output := buf.String()

		if !strings.Contains(output, "No deployments found.") {
			t.Error("output missing empty state message")
		}
	})
}

func TestOutputJSON(t *testing.T) {
	t.Parallel()

	t.Run("raw json output", func(t *testing.T) {
		t.Parallel()

		data := []byte(`{"key":"value"}`)

		var buf bytes.Buffer
		outputJSON(&buf, data)
		output := buf.String()

		if !strings.Contains(output, `{"key":"value"}`) {
			t.Errorf("expected raw JSON, got: %s", output)
		}
	})
}
