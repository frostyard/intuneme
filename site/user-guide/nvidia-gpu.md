# Nvidia GPU Support

intuneme automatically detects Nvidia GPUs at boot and configures the container with host driver libraries. No manual configuration is required — if an Nvidia GPU is present, `intuneme start` handles everything.

## How it works

When `intuneme start` detects `/dev/nvidiactl` on the host, it:

1. **Bind-mounts device nodes** — `/dev/nvidia*` and `/dev/nvidia-caps/*` are forwarded into the container with explicit `DeviceAllow` cgroup rules.
2. **Discovers host libraries** — Parses `ldconfig -p` output to find Nvidia userspace libraries (x86-64).
3. **Bind-mounts library directories** — Host directories containing Nvidia libraries are mounted read-only at `/run/host-nvidia/0/`, `/run/host-nvidia/1/`, etc. inside the container.
4. **Creates library symlinks** — After boot, symlinks are created in `/usr/lib/x86_64-linux-gnu/` pointing into the `/run/host-nvidia/` mounts, and `ldconfig` is run to update the linker cache.
5. **Bind-mounts Vulkan/EGL vendor files** — If Vulkan ICD or EGL vendor JSON files exist on the host, they are mounted at their standard paths inside the container.
6. **Sets environment variables** — `__NV_PRIME_RENDER_OFFLOAD=1` and `__GLX_VENDOR_LIBRARY_NAME=nvidia` are set in both `intuneme open` sessions and the container login profile.

## Stale symlink cleanup

On every `intuneme start`, stale symlinks from a previous Nvidia session are cleaned up — even if the GPU is no longer present. This means switching between Nvidia and non-Nvidia configurations (e.g., docking/undocking a laptop) works without manual intervention.

## Non-Nvidia systems

Systems without Nvidia GPUs are unaffected. The detection check is a simple file existence test (`/dev/nvidiactl`), and all Nvidia-specific steps are skipped when no GPU is found. Standard DRI devices (`/dev/dri/card*`, `/dev/dri/renderD*`) are always forwarded regardless.

## Requirements

- An Nvidia GPU with the proprietary driver installed on the host
- x86-64 architecture (library discovery uses the x86-64 linker path)
- No container-side driver installation needed — the host libraries are forwarded directly
