# VPN Plumbing Design

## Goal

Enable the azure-vpnclient (already installed in the container image) to create VPN tunnel connections through the host's network stack.

## Context

The nspawn container shares the host's network namespace (no `--private-network`). The azure-vpnclient needs two things the container doesn't currently have:

1. **`CAP_NET_ADMIN`** - capability to create/configure network interfaces and modify routing tables
2. **`/dev/net/tun`** - character device for creating TUN (layer 3) virtual interfaces

## Design

Add two flags to the static nspawn boot args in `BuildBootArgs`:

- `--capability=CAP_NET_ADMIN`
- `--bind=/dev/net/tun`

These are always-on (not config-gated). The container already has broad host access (home directory, X11, GPU, audio, webcam), so the incremental risk of `CAP_NET_ADMIN` is minimal.

## Changes

- `internal/nspawn/nspawn.go` - add two args to `BuildBootArgs`
- `internal/nspawn/nspawn_test.go` - update `TestBuildBootArgs` to expect the new args
