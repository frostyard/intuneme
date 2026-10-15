# Upgrading

## Upgrading the container image

When a new container image is released, use `recreate` to upgrade without losing your Intune enrollment:

```bash
intuneme recreate
intuneme start
```

`recreate` performs the following steps:

1. Stops the running container (if it is running)
2. Backs up the password hash and device enrollment database from `~/Intune/`
3. Pulls the new container image
4. Re-provisions the rootfs (same steps as `intuneme init`)
5. Restores the backed-up enrollment state

No re-enrollment is needed after `recreate`. Your existing enrollment, browser profiles, downloads, and other files in `~/Intune/` are preserved.

For the experimental Himmelblau image, `recreate` also backs up and restores
the Himmelblau daemon, HSM PIN, policy and NSS caches, and
`/var/lib/systemd/credential.secret` needed to decrypt the HSM PIN. DynamicUser
daemon state is restored under the systemd-managed private backing paths. If an
encrypted HSM PIN exists but its credential key is missing, `recreate` stops
before deleting the rootfs rather than installing an unusable credential; see
[Himmelblau troubleshooting](../troubleshooting.md#himmelblau-fails-with-failed-to-determine-local-credential-key).
The Himmelblau package source is Community Nightly, so a rebuild can contain a
newer upstream package snapshot. The identity itself cannot be switched with
`recreate`; use `destroy` and a new `init` to change image families, which
requires enrolling again.

### What is preserved

| Data | Preserved by `recreate`? |
|---|---|
| Intune enrollment state | Yes |
| Edge browser profile (Microsoft image only) | Yes |
| Files in `~/Intune/Downloads/` | Yes |
| gnome-keyring / stored credentials | Yes |
| Container rootfs (system packages, config) | No — rebuilt from image |

!!! tip
    Use `--insiders` to switch to (or stay on) the insiders channel image: `intuneme recreate --insiders`

!!! tip "Fedora / tmpfs systems"
    If `recreate` fails with "disk quota exceeded", use `--tmp-dir` to write temporary files to a disk-backed directory:
    ```bash
    intuneme recreate --tmp-dir /var/tmp
    ```

## Re-enrollment

If you need to start completely fresh with Intune, destroy and re-initialize the container:

```bash
intuneme destroy
intuneme init
intuneme start
intuneme shell
# Inside the container:
intune-portal
```

`destroy` removes the rootfs, udev rules, polkit rule, and sudoers rule. It also cleans Intune enrollment state, keyrings, and broker state from `~/Intune/`. Other files in `~/Intune/` — Downloads, Edge profile, and so on — are preserved.

To fully uninstall all intuneme artifacts from the host, including the GNOME extension, D-Bus service file, and the entire `~/Intune/` directory:

```bash
intuneme destroy --all
```

### When to re-enroll vs. upgrade

| Scenario | Recommended action |
|---|---|
| New container image released | `intuneme recreate` |
| Enrollment is broken or expired | `intuneme destroy` + `intuneme init` |
| Container rootfs is corrupted | `intuneme recreate` (or `destroy` + `init`) |
| Switching to a different Intune tenant | `intuneme destroy` + `intuneme init` |
| Full uninstall (removing all artifacts) | `intuneme destroy --all` |

!!! warning
    Re-enrollment (`destroy` + `init`) will require you to go through the full Intune enrollment process again. Your IT administrator may need to approve the device before you regain access to corporate resources.

For the experimental Himmelblau image, initialize with `intuneme init
--himmelblau`, then enroll with `intuneme enroll --user user@example.com`.
To use terminal QR/Bluetooth passkeys, pass the locally built image with
`--himmelblau-local-image`; its source marker automatically selects the
variant. The optional `--himmelblau-terminal-passkey` switch asserts that the
marked variant is intended. There is no hosted terminal-passkey image; the
selected local tag and source commit are preserved by `recreate`.
Enrollment starts a stopped container automatically. Switching identity
families (Microsoft versus Himmelblau) requires `destroy` followed by a new
`init`. To switch an existing Himmelblau installation to a locally built
terminal-passkey image while preserving Himmelblau enrollment state, update
`himmelblau_local_image` in `config.toml` to the local image tag or pass
`--himmelblau-local-image` to `intuneme recreate`. The image's embedded source
marker is detected and persisted automatically. Rebuilding that same local tag
from a different commit and running `recreate` updates the recorded source
commit.
