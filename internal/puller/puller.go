package puller

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/frostyard/intuneme/internal/runner"
)

// VerifyRootOwned checks that an extracted rootfs really is owned by uid 0.
//
// Extraction runs `sudo tar`/`umoci` and faithfully preserves whatever UIDs
// the archive carries. If the pull ran inside a nested user namespace — a
// rootless container, `podman unshare`, distrobox/toolbox, a sandboxed shell
// — then "root" in that namespace is the invoking host user, and every
// container-root file lands owned by that user instead of 0. Nothing later in
// provisioning notices: the container still boots, but `sudo` inside it is
// broken (`/usr/bin/sudo must be owned by uid 0`), and since the container
// runs without --private-users, container root IS host root — so the host
// user can write code that later executes as real root.
//
// Fail loudly here rather than hand back a silently poisoned rootfs.
func VerifyRootOwned(rootfsPath string) error {
	sentinel := filepath.Join(rootfsPath, "etc", "passwd")
	fi, err := os.Stat(sentinel)
	if err != nil {
		return fmt.Errorf("extracted rootfs looks incomplete: %w", err)
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}
	if st.Uid != 0 || st.Gid != 0 {
		return fmt.Errorf(
			"extracted rootfs is owned by %d:%d, not root: the image was unpacked inside a user namespace. "+
				"Re-run from a plain host shell (not distrobox/toolbox, `podman unshare`, a rootless container, "+
				"or a sandboxed agent shell); `cat /proc/self/uid_map` must read \"0 0 4294967295\"",
			st.Uid, st.Gid)
	}
	return nil
}

// Puller pulls a container image from a registry and extracts it to a rootfs directory.
type Puller interface {
	// Name returns a human-readable name for the backend (e.g. "podman").
	Name() string
	// PullAndExtract pulls image from a registry and extracts it to rootfsPath.
	// tmpDir overrides the directory used for intermediate files (e.g. exported
	// tars). When empty, os.TempDir() is used.
	PullAndExtract(r runner.Runner, image string, rootfsPath string, tmpDir string) error
}

// resolveTmpDir returns tmpDir when non-empty, otherwise os.TempDir().
func resolveTmpDir(tmpDir string) string {
	if tmpDir != "" {
		return tmpDir
	}
	return os.TempDir()
}

// Detect returns the first available Puller in preference order:
// podman, skopeo+umoci, docker. Returns an error if none are available.
func Detect(r runner.Runner) (Puller, error) {
	if _, err := r.LookPath("podman"); err == nil {
		return NewPodmanPuller(), nil
	}
	if _, err := r.LookPath("skopeo"); err == nil {
		if _, err := r.LookPath("umoci"); err == nil {
			return &SkopeoPuller{}, nil
		}
	}
	if _, err := r.LookPath("docker"); err == nil {
		return NewDockerPuller(), nil
	}
	return nil, fmt.Errorf("no container tool found; install podman, skopeo+umoci, or docker")
}

// containerToolPuller implements the create→export→tar-extract→rm workflow
// shared by podman and docker. It accepts the tool name and an optional
// function that returns extra flags for the pull command.
type containerToolPuller struct {
	tool      string
	pullFlags func(image string) []string
}

func (c *containerToolPuller) Name() string { return c.tool }

func (c *containerToolPuller) PullAndExtract(r runner.Runner, image string, rootfsPath string, tmpDir string) error {
	// Clean up any leftover extract container from a previous failed run
	_, _ = r.Run(c.tool, "rm", "intuneme-extract")

	// Pull the image
	pullArgs := []string{"pull"}
	if c.pullFlags != nil {
		pullArgs = append(pullArgs, c.pullFlags(image)...)
	}
	pullArgs = append(pullArgs, image)
	out, err := r.Run(c.tool, pullArgs...)
	if err != nil {
		return fmt.Errorf("%s pull failed: %w\n%s", c.tool, err, out)
	}

	// Create a temporary container to export (command is required but never
	// run — the container is only created so we can export its filesystem)
	out, err = r.Run(c.tool, "create", "--name", "intuneme-extract", image, "/bin/true")
	if err != nil {
		return fmt.Errorf("%s create failed: %w\n%s", c.tool, err, out)
	}

	// Export to tar, then extract with sudo to preserve container-internal UIDs
	tmpTar := filepath.Join(resolveTmpDir(tmpDir), "intuneme-rootfs.tar")
	out, err = r.Run(c.tool, "export", "-o", tmpTar, "intuneme-extract")
	if err != nil {
		_, _ = r.Run(c.tool, "rm", "intuneme-extract")
		return fmt.Errorf("%s export failed: %w\n%s", c.tool, err, out)
	}
	defer func() { _ = os.Remove(tmpTar) }()

	// RunAttached so sudo can prompt for password
	if err := r.RunAttached("sudo", "tar", "-xf", tmpTar, "-C", rootfsPath); err != nil {
		_, _ = r.Run(c.tool, "rm", "intuneme-extract")
		return fmt.Errorf("extract rootfs failed: %w", err)
	}

	// Remove temporary container
	out, err = r.Run(c.tool, "rm", "intuneme-extract")
	if err != nil {
		return fmt.Errorf("%s rm failed: %w\n%s", c.tool, err, out)
	}
	return nil
}

// PodmanPuller pulls and extracts using podman.
// For locally-built images (localhost/ prefix) it uses --policy=missing so
// podman doesn't try to reach a registry that doesn't exist.
type PodmanPuller struct{ containerToolPuller }

func NewPodmanPuller() *PodmanPuller {
	return &PodmanPuller{containerToolPuller{
		tool: "podman",
		pullFlags: func(image string) []string {
			if strings.HasPrefix(image, "localhost/") {
				return []string{"--policy=missing"}
			}
			return nil
		},
	}}
}

// SkopeoPuller pulls and extracts using skopeo + umoci.
type SkopeoPuller struct{}

func (p *SkopeoPuller) Name() string { return "skopeo+umoci" }

func (p *SkopeoPuller) PullAndExtract(r runner.Runner, image string, rootfsPath string, tmpDir string) error {
	// Create a temp directory for the OCI layout
	ociTmpDir, err := os.MkdirTemp(resolveTmpDir(tmpDir), "intuneme-oci-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(ociTmpDir) }()

	ociDest := ociTmpDir + ":latest"

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
type DockerPuller struct{ containerToolPuller }

func NewDockerPuller() *DockerPuller {
	return &DockerPuller{containerToolPuller{tool: "docker"}}
}
