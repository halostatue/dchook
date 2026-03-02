package main

// MockAdapter implements ContainerAdapter for testing.
type MockAdapter struct {
	AvailableErr  error
	PullOutput    []byte
	PullErr       error
	RestartOutput []byte
	RestartErr    error
}

func (m *MockAdapter) Available() error {
	return m.AvailableErr
}

func (m *MockAdapter) Deploy(deployment *Deployment, history *DeploymentHistory) {
	deployment.Pull = &DeploymentResult{
		ExitCode:   0,
		Output:     string(m.PullOutput),
		DurationMs: 1,
	}
	if m.PullErr != nil {
		deployment.Pull.ExitCode = 1
		history.Add(*deployment)
		return
	}

	deployment.Restart = &DeploymentResult{
		ExitCode:   0,
		Output:     string(m.RestartOutput),
		DurationMs: 1,
	}
	if m.RestartErr != nil {
		deployment.Restart.ExitCode = 1
	}

	history.Add(*deployment)
}
