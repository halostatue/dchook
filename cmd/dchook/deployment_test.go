package main

import (
	"testing"
	"time"
)

func TestDeploymentHistory(t *testing.T) {
	t.Parallel()

	t.Run("add and get", func(t *testing.T) {
		t.Parallel()
		h := NewDeploymentHistory()
		d := Deployment{
			ID:        "abc123",
			Timestamp: time.Now(),
		}

		h.Add(d)

		got, ok := h.Get("abc123")
		if !ok {
			t.Fatal("Get() returned false, want true")
		}
		if got.ID != d.ID {
			t.Errorf("Get() ID = %q, want %q", got.ID, d.ID)
		}
	})

	t.Run("get nonexistent", func(t *testing.T) {
		t.Parallel()
		h := NewDeploymentHistory()
		_, ok := h.Get("nonexistent")
		if ok {
			t.Error("Get() returned true for nonexistent ID")
		}
	})

	t.Run("ring buffer overflow", func(t *testing.T) {
		t.Parallel()
		h := NewDeploymentHistory()

		ids := make([]string, 15)
		for i := range 15 {
			ids[i] = generateDeploymentID()
			h.Add(Deployment{
				ID:        ids[i],
				Timestamp: time.Now(),
			})
		}

		list := h.List()
		if len(list) != 10 {
			t.Errorf("List() length = %d, want 10", len(list))
		}

		for i := range 5 {
			if _, found := h.Get(ids[i]); found {
				t.Errorf("Expected deployment %s to be dropped", ids[i])
			}
		}
		for i := 5; i < 15; i++ {
			if _, found := h.Get(ids[i]); !found {
				t.Errorf("Expected deployment %s to be retained", ids[i])
			}
		}
	})

	t.Run("list sorted by timestamp", func(t *testing.T) {
		t.Parallel()
		h := NewDeploymentHistory()
		now := time.Now()

		h.Add(Deployment{ID: "old", Timestamp: now.Add(-2 * time.Hour)})
		h.Add(Deployment{ID: "middle", Timestamp: now.Add(-1 * time.Hour)})
		h.Add(Deployment{ID: "new", Timestamp: now})

		list := h.List()
		if len(list) != 3 {
			t.Fatalf("List() length = %d, want 3", len(list))
		}

		if list[0].ID != "new" {
			t.Errorf("List()[0].ID = %q, want %q", list[0].ID, "new")
		}
		if list[1].ID != "middle" {
			t.Errorf("List()[1].ID = %q, want %q", list[1].ID, "middle")
		}
		if list[2].ID != "old" {
			t.Errorf("List()[2].ID = %q, want %q", list[2].ID, "old")
		}
	})

	t.Run("stats success", func(t *testing.T) {
		t.Parallel()
		h := NewDeploymentHistory()
		h.Add(Deployment{
			ID:      "success",
			Pull:    &DeploymentResult{ExitCode: 0},
			Restart: &DeploymentResult{ExitCode: 0},
		})

		success, failure := h.Stats()
		if success != 1 {
			t.Errorf("Stats() success = %d, want 1", success)
		}
		if failure != 0 {
			t.Errorf("Stats() failure = %d, want 0", failure)
		}
	})

	t.Run("stats failure", func(t *testing.T) {
		t.Parallel()
		h := NewDeploymentHistory()
		h.Add(Deployment{
			ID:      "pull-fail",
			Pull:    &DeploymentResult{ExitCode: 1},
			Restart: &DeploymentResult{ExitCode: 0},
		})
		h.Add(Deployment{
			ID:      "up-fail",
			Pull:    &DeploymentResult{ExitCode: 0},
			Restart: &DeploymentResult{ExitCode: 1},
		})

		success, failure := h.Stats()
		if success != 0 {
			t.Errorf("Stats() success = %d, want 0", success)
		}
		if failure != 2 {
			t.Errorf("Stats() failure = %d, want 2", failure)
		}
	})

	t.Run("stats incomplete deployment", func(t *testing.T) {
		t.Parallel()
		h := NewDeploymentHistory()
		h.Add(Deployment{
			ID:   "incomplete",
			Pull: &DeploymentResult{ExitCode: 0},
		})

		success, failure := h.Stats()
		if success != 0 {
			t.Errorf("Stats() success = %d, want 0", success)
		}
		if failure != 0 {
			t.Errorf("Stats() failure = %d, want 0", failure)
		}
	})
}
