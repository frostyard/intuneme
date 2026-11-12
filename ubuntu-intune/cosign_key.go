package containerimage

import _ "embed"

// CosignPublicKey is the cosign public key that signs every
// ghcr.io/frostyard/ubuntu-intune tag.
//
// internal/imagesig checks each image signature against this key before
// intuneme extracts the image, so a tag served by a mirror, a proxy or a
// compromised registry cannot reach the rootfs. Pinning the key in the binary
// is what makes the check mean something: a key fetched at run time could be
// replaced together with the image. The file is the same one published for a
// manual `cosign verify --key ubuntu-intune/cosign.pub` run.
//
//go:embed cosign.pub
var CosignPublicKey []byte
