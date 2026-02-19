# Recreate Container Design

## Problem

When a new `ubuntu-intune` container image is released, users need to upgrade their container without losing Intune enrollment state. The current workflow requires `intuneme destroy && intuneme init`, which wipes all state and forces device re-enrollment.

## Solution

A new `intuneme recreate` command that replaces the container rootfs with a fresh image while preserving enrollment-critical state.

## State Analysis

**User-level state (bind-mounted `~/Intune`, already survives destroy):**

- `~/.config/intune/` -- registration.toml, flights.toml
- `~/.local/share/keyrings/` -- login.keyring, user.keystore
- `~/.local/state/microsoft-identity-broker/` -- account-data.db, broker-data.db, cookies.db
- `~/.local/share/intune-portal/` -- hsts-storage, mediakeys, storage

**System-level state (inside rootfs, lost on destroy):**

- `/var/lib/microsoft-identity-device-broker/` -- device broker database (1000.db)
- `/etc/shadow` -- user's password hash (needed for gnome-keyring auto-unlock)

## Command Flow

`intuneme recreate` performs these steps:

1. Load config, verify initialized, validate sudo credentials
2. Auto-stop container and broker proxy (if running/enabled)
3. Backup state from old rootfs:
   - Extract the host user's `/etc/shadow` line (in-memory)
   - Copy `/var/lib/microsoft-identity-device-broker/` to a temp dir (`sudo cp -a`)
4. Remove old rootfs (`sudo rm -rf`)
5. Pull and extract new image (same `ImageRef()` logic as `init`)
6. Re-provision:
   - `CreateContainerUser` -- create/rename user with matching UID
   - `WriteFixups` -- hostname, hosts, systemd services, profile.d, sudoers
7. Restore state:
   - Overwrite the fresh shadow entry with the backed-up line
   - Copy device broker DB back with preserved ownership
8. Install polkit rules on host (idempotent)
9. Print success message

## Design Decisions

**No password prompt.** The shadow hash is copied from the old rootfs. This avoids gnome-keyring breakage -- the keyring is encrypted with the login password, and changing the password without re-keying the keyring would lock out stored credentials.

**Image version matches CLI.** Uses the same `ImageRef()` as `init` -- release builds pull the matching version tag, dev builds pull `:latest`.

**In-place replacement.** The old rootfs is destroyed before the new one is extracted. If the pull fails after destruction, the user recovers with `intuneme init` (user-level state in `~/Intune` is safe regardless). The simplicity outweighs the risk of a side-by-side approach requiring 2x disk space.

## Backup & Restore Details

**Shadow hash:**

- Read `<rootfs>/etc/shadow`, parse to find the line for the host username
- Store the full line in memory (single string)
- After re-provisioning, read the new shadow file, replace the user's line, write back via `sudoWriteFile`

**Device broker DB:**

- `sudo cp -a <rootfs>/var/lib/microsoft-identity-device-broker/ <tempdir>/`
- The `-a` flag preserves numeric UID/GID ownership
- After re-provisioning, `sudo cp -a` it back
- If the directory doesn't exist in the old rootfs (no enrollment yet), skip with a warning

## Error Handling

| Step | Failure | Recovery |
|------|---------|----------|
| Stop container | Won't stop | Error out, user investigates |
| Backup shadow | Can't read file | Error out before destroying rootfs |
| Backup device broker | Dir doesn't exist | Warn, continue (no enrollment to preserve) |
| Remove old rootfs | rm fails | Error out, permissions issue |
| Pull new image | Network/image error | Error out. User runs `intuneme init` to recover |
| Re-provision | User creation fails | Error out. Same recovery as above |
| Restore shadow | Write fails | Error out. Container works but password is wrong |
| Restore device broker | Copy fails | Warn, don't fail. Container works, may need re-enrollment |
| Install polkit | Fails | Warn, don't fail (same as init) |

Backups must succeed before the destructive step (rootfs removal).

## New Code

- `cmd/recreate.go` -- cobra command definition
- `internal/provision/backup.go` -- `BackupShadowEntry`, `RestoreShadowEntry`, `BackupDeviceBrokerState`, `RestoreDeviceBrokerState`
- `internal/provision/backup_test.go` -- unit tests for shadow parsing/restoration

## Testing

- Unit tests for shadow entry backup/restore (fixture files, no sudo needed)
- Device broker backup/restore verified via mock runner (correct commands issued)
- Full integration: manual testing with a running container (same as init/start/stop)
