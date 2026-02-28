package puller

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
	// Clean up any leftover extract container from a previous failed run
	_, _ = r.Run("podman", "rm", "intuneme-extract")

	// Pull the image. For locally-built images (localhost/ prefix) use
	// --policy=missing so podman doesn't try to reach a registry that doesn't
	// exist. For registry images use the default (always) so stale cached
	// images are refreshed.
	pullArgs := []string{"pull"}
	if strings.HasPrefix(image, "localhost/") {
		pullArgs = append(pullArgs, "--policy=missing")
	}
	pullArgs = append(pullArgs, image)
	out, err := r.Run("podman", pullArgs...)
	if err != nil {
		return fmt.Errorf("podman pull failed: %w\n%s", err, out)
	}

	// Create a temporary container to export
	out, err = r.Run("podman", "create", "--name", "intuneme-extract", image)
	if err != nil {
		return fmt.Errorf("podman create failed: %w\n%s", err, out)
	}

	// Export to tar, then extract with sudo to preserve container-internal UIDs
	tmpTar := filepath.Join(os.TempDir(), "intuneme-rootfs.tar")
	out, err = r.Run("podman", "export", "-o", tmpTar, "intuneme-extract")
	if err != nil {
		_, _ = r.Run("podman", "rm", "intuneme-extract")
		return fmt.Errorf("podman export failed: %w\n%s", err, out)
	}
	defer func() { _ = os.Remove(tmpTar) }()

	// RunAttached so sudo can prompt for password
	if err := r.RunAttached("sudo", "tar", "-xf", tmpTar, "-C", rootfsPath); err != nil {
		_, _ = r.Run("podman", "rm", "intuneme-extract")
		return fmt.Errorf("extract rootfs failed: %w", err)
	}

	// Remove temporary container
	out, err = r.Run("podman", "rm", "intuneme-extract")
	if err != nil {
		return fmt.Errorf("podman rm failed: %w\n%s", err, out)
	}
	return nil
}

// SkopeoPuller pulls and extracts using skopeo + umoci.
type SkopeoPuller struct{}

func (p *SkopeoPuller) Name() string { return "skopeo+umoci" }

func (p *SkopeoPuller) PullAndExtract(r runner.Runner, image string, rootfsPath string) error {
	// Create a temp directory for the OCI layout
	tmpDir, err := os.MkdirTemp("", "intuneme-oci-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	ociDest := tmpDir + ":latest"

	// Pull image to OCI layout
	out, err := r.Run("skopeo", "copy", "docker://"+image, "oci:"+ociDest)
	if err != nil {
		return fmt.Errorf("skopeo copy failed: %w\n%s", err, out)
	}

	// Unpack OCI layout to rootfs with sudo to preserve UIDs
	if err := r.RunAttached("sudo", "umoci", "raw", "unpack", "--image", ociDest, rootfsPath); err != nil {
		return fmt.Errorf("umoci unpack failed: %w", err)
	}

	return nil
}

// DockerPuller pulls and extracts using docker.
type DockerPuller struct{}

func (p *DockerPuller) Name() string { return "docker" }

func (p *DockerPuller) PullAndExtract(r runner.Runner, image string, rootfsPath string) error {
	// Clean up any leftover extract container from a previous failed run
	_, _ = r.Run("docker", "rm", "intuneme-extract")

	// Pull the image
	out, err := r.Run("docker", "pull", image)
	if err != nil {
		return fmt.Errorf("docker pull failed: %w\n%s", err, out)
	}

	// Create a temporary container to export
	out, err = r.Run("docker", "create", "--name", "intuneme-extract", image)
	if err != nil {
		return fmt.Errorf("docker create failed: %w\n%s", err, out)
	}

	// Export to tar, then extract with sudo to preserve container-internal UIDs
	tmpTar := filepath.Join(os.TempDir(), "intuneme-rootfs.tar")
	out, err = r.Run("docker", "export", "-o", tmpTar, "intuneme-extract")
	if err != nil {
		_, _ = r.Run("docker", "rm", "intuneme-extract")
		return fmt.Errorf("docker export failed: %w\n%s", err, out)
	}
	defer func() { _ = os.Remove(tmpTar) }()

	// RunAttached so sudo can prompt for password
	if err := r.RunAttached("sudo", "tar", "-xf", tmpTar, "-C", rootfsPath); err != nil {
		_, _ = r.Run("docker", "rm", "intuneme-extract")
		return fmt.Errorf("extract rootfs failed: %w", err)
	}

	// Remove temporary container
	out, err = r.Run("docker", "rm", "intuneme-extract")
	if err != nil {
		return fmt.Errorf("docker rm failed: %w\n%s", err, out)
	}
	return nil
}
