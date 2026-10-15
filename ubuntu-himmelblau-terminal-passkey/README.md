# ubuntu-himmelblau-terminal-passkey

This is an opt-in experimental Himmelblau image variant. It builds the
`himmelblau` daemon and `pam-himmelblau` from the pinned
`mPyKen/himmelblau` terminal-passkey commit in
[`internal/version/himmelblau_terminal_passkey_source.txt`](../internal/version/himmelblau_terminal_passkey_source.txt). The regular
`ubuntu-himmelblau` image continues to use signed upstream packages.
Both images are based on Ubuntu 26.04; the fork packages use Himmelblau's
matching Ubuntu 26.04 packaging target. The build compiles both the daemon and
PAM module from the pinned fork.
Like the standard Himmelblau image, this variant does not install Microsoft
Edge or Intune Portal.
The pinned commit `dae50ca7cb84e959fa76b4e3a4ee682431a48b57` from
`feat/terminal-passkey-auth-test` includes the cached-PRT client fallback.
Intuneme builds that source directly and applies no local patches.
After PIN creation, a decoupled local Hello key validates the cached PRT
online. If that exchange fails with an Edge-client token-acquisition error
(including `AADSTS530003`), the daemon retries with the default client, just
as initial enrollment does, before entering its existing error handling.
This avoids a redundant second QR/Bluetooth authentication when the retry
succeeds. Consent errors retain their existing scope fallback; network errors
and failures of the default-client retry are not treated as successful
authentication. The same retry helper serves the registered Hello-key path.
Intuneme defaults `apply_policy = true` for both variants to enable Intune
enrollment and policy application; set it to `false` in `[global]` to opt out.
Intuneme also defaults `enable_passwordless_qr_bluetooth = true`; set it to
`false` in `[global]` to disable the QR/Bluetooth passkey flow.

The fork allows Himmelblau PAM to render its QR code in a local terminal. It
starts USB security-key authentication while probing Bluetooth; when Bluetooth
is powered, it can offer QR/Bluetooth authentication alongside USB. If
Bluetooth is unavailable or powered off, the existing USB fallback remains.
The QR still only appears when authentication reaches the FIDO/passkey step
and does not skip an Entra password prompt. The image itself does not run
BlueZ or receive the host system bus. During `intuneme enroll`, when
`enable_passwordless_qr_bluetooth = true`, the CLI starts `xdg-dbus-proxy` on
the host, filters access to `org.bluez`, and bind-mounts only that proxy
directory into the running container. The bind lasts until container shutdown;
the proxy socket itself exists only for the duration of PAM authentication.

If terminal QR/Bluetooth is enabled, the host must have `xdg-dbus-proxy`, an
active `bluetooth.service`, and a Bluetooth adapter visible to BlueZ. Run
enrollment from an interactive terminal. The adapter should be powered on; if
it is off, Himmelblau may ask you to enable Bluetooth before continuing.

Build the pinned fork image from the repository root. Local builds support
Podman, Buildah, or Docker Buildx. Docker Buildx loads the unchunked `builder`
target into the local Docker image store; Podman/Buildah use the same target.
The local helper uses this target directly; intuneme does not pull or publish
a hosted terminal-passkey image.

```bash
./ubuntu-himmelblau-terminal-passkey/build_files/build-local-image \
  localhost/ubuntu-himmelblau-terminal-passkey:dev
```

For an existing container, rebuild the local image and install it with
`intuneme recreate --himmelblau-local-image localhost/ubuntu-himmelblau-terminal-passkey:dev`.
Recreate preserves Himmelblau enrollment state; a CLI rebuild or container
restart alone does not install rebuilt daemon/PAM packages. The source marker
records the exact fork commit.

To build from another repository/ref, pass the source when creating the image.
The builder resolves the ref to a commit and records it in the image marker:

```bash
./ubuntu-himmelblau-terminal-passkey/build_files/build-local-image \
  --source-repo https://github.com/mPyKen/himmelblau.git \
  --source-ref feat/terminal-passkey-auth-test \
  localhost/ubuntu-himmelblau-terminal-passkey:dev
```

Load either local image using intuneme's existing local-image override:

```bash
intuneme init --himmelblau \
  --himmelblau-local-image localhost/ubuntu-himmelblau-terminal-passkey:dev
intuneme edit --himmelblau
intuneme enroll --user user@example.com
```

The QR/Bluetooth setting defaults to true; set
`enable_passwordless_qr_bluetooth = false` in the Himmelblau `[global]` config
to opt out. Init detects the image marker, stores the selected source commit,
and makes `recreate` require a matching local image.

This is an unmerged, experimental fork with terminal PAM QR support and the
cached-PRT fallback. It has not been validated upstream on Ubuntu or with
physical FIDO devices. The default local build uses the commit in
`internal/version/himmelblau_terminal_passkey_source.txt`; custom local builds
resolve the requested Git ref to a commit and embed that commit in the image.
Review the source before selecting a different branch or updating the pin.

The pinned fork includes Rust regression tests. In a checkout of that commit, run
`cargo test -p himmelblau_unix_common retry_edge_prt_exchange`.
They cover `530003` and structured token errors, successful retries,
non-Edge clients, consent/network handling, and default-client rejection.
