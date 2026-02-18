package puller

import (
	"fmt"

	"github.com/frostyard/intuneme/internal/runner"
)

// Puller pulls a container image from a registry and extracts it to a rootfs directory.
type Puller interface {
	// Name returns a human-readable name for the backend (e.g. "podman").
	Name() string
	// PullAndExtract pulls image from a registry and extracts it to rootfsPath.
	PullAndExtract(r runner.Runner, image string, rootfsPath string) error
}

// Detect returns the first available Puller in preference order:
// podman, skopeo+umoci, docker. Returns an error if none are available.
func Detect(r runner.Runner) (Puller, error) {
	if _, err := r.LookPath("podman"); err == nil {
		return &PodmanPuller{}, nil
	}
	if _, err := r.LookPath("skopeo"); err == nil {
		if _, err := r.LookPath("umoci"); err == nil {
			return &SkopeoPuller{}, nil
		}
	}
	if _, err := r.LookPath("docker"); err == nil {
		return &DockerPuller{}, nil
	}
	return nil, fmt.Errorf("no container tool found; install podman, skopeo+umoci, or docker")
}

// PodmanPuller pulls and extracts using podman.
type PodmanPuller struct{}

func (p *PodmanPuller) Name() string { return "podman" }

func (p *PodmanPuller) PullAndExtract(r runner.Runner, image string, rootfsPath string) error {
	return fmt.Errorf("not implemented")
}

// SkopeoPuller pulls and extracts using skopeo + umoci.
type SkopeoPuller struct{}

func (p *SkopeoPuller) Name() string { return "skopeo+umoci" }

func (p *SkopeoPuller) PullAndExtract(r runner.Runner, image string, rootfsPath string) error {
	return fmt.Errorf("not implemented")
}

// DockerPuller pulls and extracts using docker.
type DockerPuller struct{}

func (p *DockerPuller) Name() string { return "docker" }

func (p *DockerPuller) PullAndExtract(r runner.Runner, image string, rootfsPath string) error {
	return fmt.Errorf("not implemented")
}
