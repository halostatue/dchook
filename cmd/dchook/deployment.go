// SPDX-License-Identifier: Apache-2.0
package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

const (
	maxDeployments   = 10
	deploymentIDSize = 6 // bytes for random deployment ID

	statusPending    = "pending"
	statusPulling    = "pulling"
	statusRestarting = "restarting"
	statusComplete   = "complete"
	statusFailed     = "failed"
)

type DeploymentResult struct {
	ExitCode   int    `json:"exit_code"`
	Output     string `json:"output"`
	DurationMs int64  `json:"duration_ms"`
}

type Deployment struct {
	ID        string            `json:"id"`
	Timestamp time.Time         `json:"timestamp"`
	Status    string            `json:"status"` // "pending", "pulling", "restarting", "complete", "failed"
	Request   json.RawMessage   `json:"request,omitempty"`
	Pull      *DeploymentResult `json:"pull,omitempty"`
	Restart   *DeploymentResult `json:"restart,omitempty"`
}

type DeploymentHistory struct {
	mutex       sync.RWMutex
	deployments [maxDeployments]Deployment
	count       int // Number of deployments stored (0-10)
	next        int // Next write position (0-9)
}

func NewDeploymentHistory() *DeploymentHistory {
	return &DeploymentHistory{}
}

func (h *DeploymentHistory) Add(d Deployment) {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	h.deployments[h.next] = d
	h.next = (h.next + 1) % maxDeployments
	if h.count < maxDeployments {
		h.count++
	}
}

func (h *DeploymentHistory) Update(id string, updateFn func(*Deployment)) {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	for i := range h.count {
		if h.deployments[i].ID == id {
			updateFn(&h.deployments[i])
			return
		}
	}
}

func (h *DeploymentHistory) Get(id string) (Deployment, bool) {
	h.mutex.RLock()
	defer h.mutex.RUnlock()

	for i := range h.count {
		if h.deployments[i].ID == id {
			return h.deployments[i], true
		}
	}
	return Deployment{}, false
}

func (h *DeploymentHistory) List() []Deployment {
	h.mutex.RLock()
	defer h.mutex.RUnlock()

	result := make([]Deployment, h.count)
	for i := range h.count {
		result[i] = h.deployments[i]
	}

	for i := range len(result) / 2 {
		j := len(result) - 1 - i
		result[i], result[j] = result[j], result[i]
	}

	return result
}

func (h *DeploymentHistory) Stats() (int, int) {
	h.mutex.RLock()
	defer h.mutex.RUnlock()

	var success, failure int
	for i := range h.count {
		d := &h.deployments[i]

		if d.Pull != nil && d.Pull.ExitCode != 0 {
			failure++
			continue
		}

		if d.Restart != nil {
			if d.Restart.ExitCode != 0 {
				failure++
			} else {
				success++
			}
		}

	}
	return success, failure
}

func (h *DeploymentHistory) LastDeployment() *time.Time {
	h.mutex.RLock()
	defer h.mutex.RUnlock()

	if h.count == 0 {
		return nil
	}

	var latest *Deployment
	for i := range h.count {
		if latest == nil || h.deployments[i].Timestamp.After(latest.Timestamp) {
			latest = &h.deployments[i]
		}
	}

	if latest != nil {
		return &latest.Timestamp
	}
	return nil
}

func generateDeploymentID() string {
	b := make([]byte, deploymentIDSize)
	if _, err := rand.Read(b); err != nil {
		return "-" + hex.EncodeToString(fmt.Appendf(nil, "%d", time.Now().UnixNano()))[:12]
	}
	return hex.EncodeToString(b)
}
