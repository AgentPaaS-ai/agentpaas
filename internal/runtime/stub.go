package runtime

// NewDockerRuntimeWithDriver returns a DockerRuntime that delegates all
// operations to driver. Intended for unit tests that need to capture
// ContainerSpec without a live Docker daemon.
func NewDockerRuntimeWithDriver(driver RuntimeDriver) *DockerRuntime {
	return &DockerRuntime{driver: driver}
}