# ubuntu-himmelblau

Experimental Ubuntu 26.04 container image for intuneme, using the Himmelblau
Entra authentication, broker, and compliance stack in place of Microsoft's
`microsoft-identity-broker` and `intune-portal`.
Neither this image nor the terminal-passkey variant installs Microsoft Edge
or Intune Portal.

The image installs Himmelblau Community Nightly packages from the project's
signed Ubuntu 26.04 APT repository. It has no upstream-supported
systemd-nspawn deployment contract. Authentication, Intune compliance,
headless broker activation, and enrollment preservation across image rebuilds
must be treated as experimental.

The shared build installs `binutils` and verifies `/usr/bin/strings` is
available for Secure Boot database custom-compliance scripts. In Himmelblau
mode, intuneme also binds the host's `/run/udev/data` read-only at runtime when
present; it does not change the task worker's device isolation.

The package's commented configuration at
`/usr/lib/himmelblau/himmelblau.conf` is retained as the source template.
`intuneme init --himmelblau` derives `/etc/himmelblau/himmelblau.conf` from
that template; `intuneme edit --himmelblau` opens it for optional
customization at setup time or later. The editor comes from the host
(`$VISUAL`/`$EDITOR` or the host desktop's default text application); no editor
is installed in the container for this feature.
Intuneme preserves the comments and unrelated defaults, while enforcing the
configured domain and account-mapping options. It defaults `apply_policy =
true` when the setting is missing or commented, enabling Himmelblau's Intune
enrollment and policy application; an explicit `apply_policy = false` opts out.
It also defaults `enable_passwordless_qr_bluetooth = true` unless explicitly
disabled. This does not add terminal QR support to the standard image; its
upstream PAM handler remains GDM-gated.

Himmelblau performs device enrollment during PAM authentication; it does not
provide a separate join command. In intuneme, run `intuneme enroll --user
<UPN>` from a terminal after starting the container. The image includes
`pamtester` so intuneme can explicitly invoke the fail-closed PAM service;
`machinectl shell` alone can launch a shell without authenticating the user.

intuneme writes a line in `/etc/himmelblau/user-map` in the upstream
`local_username:upn@domain` format, mapping the host-matched local account to
the supplied Entra UPN. The map resolves UPN lookups through the local
account's UID/GID; intuneme continues PAM login and app execution as the local
username, preserving its existing home and `~/Intune` bind. No RFC2307
`uidNumber` is required. The image disables `cn_name_mapping`, which
does not remap an existing local `/etc/passwd` account.

The `machine-shell` PAM service has a rootfs allowlist for PAM callers. On first
setup it includes root and the host-matched local user; during enrollment,
intuneme removes the local user and verifies that only root remains. It then
runs `pamtester machine-shell <host-user> authenticate` through the foreground
nsenter helper as the mapped local user, preserving PAM's actual exit status.
This authenticates through Himmelblau rather than the allowlist. Only after PAM
succeeds does intuneme open the usual polkit-authorized shell. This distinction
matters because `machinectl shell` can open a shell without invoking PAM
authentication and reports success even when its remote command fails. The
mapping keeps the local account's host-matched UID and the mapped account
retains intuneme's normal local groups and sudo rule. Enrollment reboots the
container after writing the map; this closes container apps and shells.

The upstream project documents first-party package installation and account
mapping in its [README](https://github.com/himmelblau-idm/himmelblau/blob/master/README.md)
and [`himmelblau.conf(5)`](https://github.com/himmelblau-idm/himmelblau/blob/master/man/man5/himmelblau.conf.5).

The image is signed with the intuneme cosign key:

```bash
cosign verify --key ../ubuntu-intune/cosign.pub ghcr.io/frostyard/ubuntu-himmelblau:latest
```
