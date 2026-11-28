# Upgrading

## Upgrading the container image

When a new container image is released, use `recreate` to upgrade without losing your Intune enrollment:

```bash
intuneme recreate
intuneme start
```

`recreate` performs the following steps:

1. Checks the image signature (see below)
2. Stops the running container (if it is running)
3. Backs up the password hash and device enrollment database from `~/Intune/`
4. Pulls the new container image
5. Re-provisions the rootfs (same steps as `intuneme init`)
6. Restores the backed-up enrollment state

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

### Checks the image signature

Before it stops anything, `recreate` checks the cosign signature of the selected registry image against the key in your intuneme release, and then pulls the image by the digest it verified, never by the tag. If the check fails, the running container, old rootfs, enrollment and saved local-image selection are unchanged. See [Troubleshooting → Image signature check failed](../troubleshooting.md#image-signature-check-failed).

Unsigned local Himmelblau images require `--insecure-skip-signature-check` on
every `init` or `recreate`; the bypass is not saved in config. The flag keeps
the local reference and warns that the image is not verified. Use it only for
an image you built and trust yourself; normal registry images remain verified.

### What is preserved

| Data | Preserved by `recreate`? |
|---|---|
| Intune enrollment state | Yes |
| Edge browser profile (Microsoft image only) | Yes |
| Files in `~/Intune/Downloads/` | Yes |
| gnome-keyring / stored credentials | Yes |
| Container rootfs (system packages, config) | No — rebuilt from image |

!!! tip
    `recreate` keeps the channel saved in `config.toml`. Use `intuneme recreate --insiders` to switch to the insiders channel image, and `intuneme recreate --insiders=false` to switch back to stable.

!!! tip "Fedora / tmpfs systems"
    If `recreate` fails with "disk quota exceeded", use `--tmp-dir` to write temporary files to a disk-backed directory:
    ```bash
    intuneme recreate --tmp-dir /var/tmp
    ```

## Signature check (migration note)

From the next release, `intuneme init` and `intuneme recreate` check the container image's cosign signature before they extract it. Existing containers keep running unchanged: no command you do not run starts a check.

A rootfs that an older intuneme pulled was never verified. To replace it with a verified image, run `intuneme recreate`. The check runs before the old rootfs is touched, so a failed check costs you nothing but the wait.

An offline machine could never pull the image, and the check needs the same access to `https://ghcr.io` as the pull does. A proxy or a registry mirror that blocks the signature lookup makes the check fail; the error names the troubleshooting page.

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
from a different commit and running `recreate --insecure-skip-signature-check` updates the recorded source
commit.
