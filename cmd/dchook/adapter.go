package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

const dockerVersionTimeout = 5 * time.Second

// ContainerAdapter manages container deployments.
type ContainerAdapter interface {
	Available() error
	Deploy(deployment *Deployment, history *DeploymentHistory)
}

// DockerComposeAdapter implements ContainerAdapter using docker compose.
type DockerComposeAdapter struct {
	ComposeFile    string
	ProjectName    string
	ExceptServices []string
}

func (d *DockerComposeAdapter) Available() error {
	ctx, cancel := context.WithTimeout(context.Background(), dockerVersionTimeout)
	defer cancel()

	// Check docker socket access
	cmd := exec.CommandContext(ctx, "docker", "version")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker version check failed: %w", err)
	}

	// Check docker compose is available
	cmd = exec.CommandContext(ctx, "docker", "compose", "version")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker compose version check failed: %w", err)
	}
	return nil
}

func (d *DockerComposeAdapter) Deploy(deployment *Deployment, history *DeploymentHistory) {
	go func() {
		// Update status to pulling
		history.Update(deployment.ID, func(d *Deployment) {
			d.Status = statusPulling
		})

		// Pull
		if !d.executePull(deployment) {
			history.Update(deployment.ID, func(d *Deployment) {
				d.Status = statusFailed
				d.Pull = deployment.Pull
			})
			return
		}

		// Update status to restarting
		history.Update(deployment.ID, func(d *Deployment) {
			d.Status = statusRestarting
			d.Pull = deployment.Pull
		})

		// Restart
		d.executeRestart(deployment)

		// Update final status
		status := statusComplete
		if deployment.Restart != nil && deployment.Restart.ExitCode != 0 {
			status = statusFailed
		}
		history.Update(deployment.ID, func(d *Deployment) {
			d.Status = status
			d.Restart = deployment.Restart
		})
	}()
}

func (d *DockerComposeAdapter) executePull(deployment *Deployment) bool {
	start := time.Now()
	pullOutput, pullErr := d.pull()
	pullDuration := time.Since(start)

	pullExitCode := 0
	if pullErr != nil {
		var exitErr *exec.ExitError
		if errors.As(pullErr, &exitErr) {
			pullExitCode = exitErr.ExitCode()
		} else {
			pullExitCode = 1
		}
	}

	deployment.Pull = &DeploymentResult{
		ExitCode:   pullExitCode,
		Output:     string(pullOutput),
		DurationMs: pullDuration.Milliseconds(),
	}

	if pullErr != nil {
		slog.Error(
			"deployment pull failed",
			"deployment_id",
			deployment.ID,
			"command",
			d.formatCommand("pull"),
			"exit_code",
			pullExitCode,
			"output",
			string(pullOutput),
			"error",
			pullErr,
		)
		return false
	}

	slog.Info(
		"deployment pull complete",
		"deployment_id",
		deployment.ID,
		"duration_ms",
		pullDuration.Milliseconds(),
	)
	return true
}

func (d *DockerComposeAdapter) executeRestart(deployment *Deployment) {
	start := time.Now()
	upOutput, upErr := d.restart()
	upDuration := time.Since(start)

	upExitCode := 0
	if upErr != nil {
		var exitErr *exec.ExitError
		if errors.As(upErr, &exitErr) {
			upExitCode = exitErr.ExitCode()
		} else {
			upExitCode = 1
		}
	}

	deployment.Restart = &DeploymentResult{
		ExitCode:   upExitCode,
		Output:     string(upOutput),
		DurationMs: upDuration.Milliseconds(),
	}

	if upErr != nil {
		slog.Error(
			"deployment up failed",
			"deployment_id",
			deployment.ID,
			"command",
			d.formatCommand("up", "-d", "--remove-orphans"),
			"exit_code",
			upExitCode,
			"output",
			string(upOutput),
			"error",
			upErr,
		)
	} else {
		slog.Info(
			"deployment complete",
			"deployment_id",
			deployment.ID,
			"pull_duration_ms",
			deployment.Pull.DurationMs,
			"up_duration_ms",
			upDuration.Milliseconds(),
		)
	}
}

func (d *DockerComposeAdapter) pull() ([]byte, error) {
	return d.runDocker("pull")
}

func (d *DockerComposeAdapter) restart() ([]byte, error) {
	args := []string{"up", "-d", "--remove-orphans"}

	if len(d.ExceptServices) > 0 {
		services, err := d.getServices()
		if err != nil {
			return nil, fmt.Errorf("failed to get services: %w", err)
		}

		filtered := d.filterServices(services)
		if len(filtered) > 0 {
			args = append(args, filtered...)
		}
	}

	return d.runDocker(args...)
}

func (d *DockerComposeAdapter) getServices() ([]string, error) {
	output, err := d.runDocker("config", "--services")
	if err != nil {
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	var services []string
	for _, line := range lines {
		if line = strings.TrimSpace(line); line != "" {
			services = append(services, line)
		}
	}
	return services, nil
}

func (d *DockerComposeAdapter) filterServices(services []string) []string {
	exceptMap := make(map[string]bool)
	for _, svc := range d.ExceptServices {
		exceptMap[svc] = true
	}

	var filtered []string
	for _, svc := range services {
		if !exceptMap[svc] {
			filtered = append(filtered, svc)
		}
	}
	return filtered
}

func (d *DockerComposeAdapter) runDocker(commandArgs ...string) ([]byte, error) {
	args := d.buildArgs(commandArgs...)

	//nolint:gosec // parameters do docker compose are validated
	cmd := exec.CommandContext(context.Background(), "docker", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("docker compose command failed: %w", err)
	}
	return output, nil
}

func (d *DockerComposeAdapter) formatCommand(commandArgs ...string) string {
	args := append([]string{"docker"}, d.buildArgs(commandArgs...)...)
	return strings.Join(args, " ")
}

func (d *DockerComposeAdapter) buildArgs(commandArgs ...string) []string {
	args := []string{"compose", "-f", d.ComposeFile}
	if d.ProjectName != "" {
		args = append(args, "-p", d.ProjectName)
	}
	return append(args, commandArgs...)
}
