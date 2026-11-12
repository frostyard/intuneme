# 0005 — Verify the container image signature before extraction

- **Status:** Accepted
- **Date:** 2026-11-11

## Context

`intuneme init` and `intuneme recreate` pull
`ghcr.io/frostyard/ubuntu-intune:<tag>` and extract it as root. The CI workflow
`.github/workflows/build-container.yml` has signed every published image with
cosign since the signing step was added: the legacy format, with
`--key env://COSIGN_PRIVATE_KEY --new-bundle-format=false`, so the signature is
a manifest at tag `sha256-<hex>.sig` in the same repository. The public key is
`ubuntu-intune/cosign.pub`.

Nothing in intuneme checked that signature (issue
[frostyard/intuneme#277](https://github.com/frostyard/intuneme/issues/277)). A
tag served by a registry mirror, a corporate proxy or a compromised account
reached the rootfs unchecked. The container runs without `--private-users`, so
container root is host root: an unsigned image installs its content with full
host privilege.

## Decision

`internal/imagesig` verifies the image signature, with the Go standard library
only. No new module goes into `go.mod`: the check is a Registry v2 client
(Bearer challenge, manifest, blob), a PEM and ECDSA check, and 1 JSON parse.

- The public key is embedded in the binary
  (`containerimage.CosignPublicKey`). A key fetched at run time could be
  replaced together with the image.
- The verifier reads the legacy `.sig` format: the manifest at
  `sha256-<hex>.sig`, its simplesigning layers, and the signature annotation.
  The Rekor bundle annotation is ignored, so a signature is trusted only
  because it verifies with the pinned key.
- The identity is the repository. The payload must name
  `critical.type` `cosign container image signature`, the manifest digest that
  the verifier computed from the manifest bytes, and the `docker-reference`
  `host/repo` (no tag, no digest).
- The digest is `sha256:` plus the hash of the manifest bytes that were read.
  `Docker-Content-Digest` is never trusted: the registry chooses it.
- The caller pulls the verified digest `host/repo@sha256:<hex>`, never the tag.
  A tag can be moved to another manifest between the check and the pull.
- `init` checks before it creates the rootfs directory or extracts anything;
  `recreate` checks after the dry-run return and before sudo, before the stop,
  before the backups and before the `sudo rm -rf` of the old rootfs.
- `--insecure-skip-signature-check` on both commands is the only bypass. It
  warns that the image is not verified.
- No Rekor check. The pinned key is the trust anchor; the transparency log
  would add a second network dependency for no gain here.

## Consequences

- `init` and `recreate` need `https://ghcr.io`. They already needed the registry
  to pull the image, so this adds no new host requirement.
- A tag can be published a few minutes before its signature. In that window the
  check fails and the message says to try again later.
- Changing the signing format, the key or the identity in CI needs a change to
  `internal/imagesig` in the same pull request.
  `image_signature_contract_test.go` pins the signing flags, so the pull request
  cannot ship a signer the verifier does not read.
- Rotating the key needs a new intuneme release. The old binary keeps checking
  the old key.
- A rootfs pulled by an older intuneme was never verified. Only
  `intuneme recreate` replaces it with a verified image.
- The check costs 3 to 5 registry requests before the pull.

## Alternatives considered

- **Shell out to the cosign binary:** it is not packaged on the target
  distributions and would add a host dependency plus a key-file path to manage.
- **sigstore-go or the cosign Go API:** they bring large dependency trees, and
  sigstore-go does not read the legacy `.sig` format without a registry client
  of its own.
- **`policy.json` with `sigstoreSigned`:** containers' policy engine covers
  podman and skopeo, not docker, and it changes host-wide configuration.
- **Keep manual verification:** it protects nothing at install time, which is
  the moment the image is extracted as root.

## References

- Shapes: [design/container-lifecycle.md](../design/container-lifecycle.md),
  [design/overview.md](../design/overview.md),
  [specs/container-image.md](../specs/container-image.md)
- Builds on: [ADR-0004](0004-adopt-the-repository-baseline.md) (the risk paths
  and the gate this change extends)
