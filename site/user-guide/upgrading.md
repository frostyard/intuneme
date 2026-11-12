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

### Checks the image signature

Before it stops anything, `recreate` checks the cosign signature of the new image against the key in your intuneme release, and then pulls the image by the digest it verified, never by the tag. If the check fails, `recreate` stops with `image signature check failed` and leaves the old rootfs alone: the container keeps running and the enrollment stays where it is. The message names the reason and what to try. See [Troubleshooting → Image signature check failed](../troubleshooting.md#image-signature-check-failed).

`--insecure-skip-signature-check` skips the check and pulls the tag. It warns that the image is not verified; use it only for an image you built and trust yourself.

### What is preserved

| Data | Preserved by `recreate`? |
|---|---|
| Intune enrollment state | Yes |
| Edge browser profile | Yes |
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

