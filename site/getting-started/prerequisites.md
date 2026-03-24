# Prerequisites

Before installing intuneme, make sure your host system has the following.

## systemd-nspawn and machinectl

intuneme uses `systemd-nspawn` to run the container and `machinectl` to manage it. Both ship in the `systemd-container` package.

=== "Debian/Ubuntu"

    ```bash
    sudo apt install systemd-container
    ```

=== "Fedora/RHEL"

    ```bash
    sudo dnf install systemd-container
    ```

=== "Arch"

    ```bash
    sudo pacman -S systemd
    ```

!!! note
    `systemd-container` is a separate package on most distributions even though systemd itself is already installed.

## Container engine

intuneme pulls the Ubuntu container image from the GitHub Container Registry (GHCR) and extracts it as a rootfs. You need one of:

- **podman** (preferred) — rootless, no daemon required
- **docker** — works, but requires the daemon to be running
- **skopeo + umoci** — lightweight alternative if you prefer not to install a full container engine

=== "Debian/Ubuntu"

    ```bash
    sudo apt install podman
    ```

=== "Fedora/RHEL"

    ```bash
    sudo dnf install podman
    ```

=== "Arch"

    ```bash
    sudo pacman -S podman
    ```

!!! note
    podman is the recommended choice. It runs rootless and is available in the default repositories of all major distributions.

## Graphical desktop session

The container forwards display access from the host, so a running graphical session is required — either X11 or Wayland. Most standard desktop environments (GNOME, KDE, etc.) satisfy this automatically.

## Audio server

Sound in Edge and other container apps is forwarded from the host. You need PipeWire or PulseAudio running on the host. PipeWire (the default on modern distributions) is preferred.

!!! note
    If you're on an immutable distribution (Fedora Silverblue, Bazzite, NixOS), all of the above are typically pre-installed. You likely only need to install `podman` if it's not already present.

## Next step

Once prerequisites are met, proceed to [Installation](installation.md).
