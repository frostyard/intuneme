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

### What is preserved

| Data | Preserved by `recreate`? |
|---|---|
| Intune enrollment state | Yes |
| Edge browser profile | Yes |
| Files in `~/Intune/Downloads/` | Yes |
| gnome-keyring / stored credentials | Yes |
| Container rootfs (system packages, config) | No — rebuilt from image |

!!! tip
    Use `--insiders` to switch to (or stay on) the insiders channel image: `intuneme recreate --insiders`

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

`destroy` removes the rootfs and cleans Intune enrollment state from `~/Intune/`. Other files in `~/Intune/` — Downloads, Edge profile, and so on — are preserved.

### When to re-enroll vs. upgrade

| Scenario | Recommended action |
|---|---|
| New container image released | `intuneme recreate` |
| Enrollment is broken or expired | `intuneme destroy` + `intuneme init` |
| Container rootfs is corrupted | `intuneme recreate` (or `destroy` + `init`) |
| Switching to a different Intune tenant | `intuneme destroy` + `intuneme init` |

!!! warning
    Re-enrollment (`destroy` + `init`) will require you to go through the full Intune enrollment process again. Your IT administrator may need to approve the device before you regain access to corporate resources.
