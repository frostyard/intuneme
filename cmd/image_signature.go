package cmd

import (
	"context"
	"fmt"

	"github.com/frostyard/intuneme/internal/imagesig"
)

// checkImageSignature verifies an image reference and returns the digest
// reference to pull. It is a variable so a test can replace it: the real one
// needs the network.
var checkImageSignature = imagesig.Verify

// resolvePullRef returns the reference that init and recreate pull. Without
// skip it is the verified host/repo@sha256:<hex>; with skip it is the image
// reference unchanged, and the run says so.
//
// Both callers run this before they write or delete anything, because the image
// is extracted as root and the container runs without --private-users.
func resolvePullRef(ctx context.Context, image string, skip bool) (string, error) {
	if skip {
		rep.Warning("Skipping the image signature check because of --insecure-skip-signature-check: %s is NOT verified", image)
		return image, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	rep.Message("Checking the signature of %s...", image)
	pullRef, err := checkImageSignature(ctx, image)
	if err != nil {
		return "", fmt.Errorf("image signature check failed for %s: %w\n"+
			"intuneme did not pull or extract the image and changed nothing.\n"+
			"Check that this host can reach https://ghcr.io (a proxy or a registry mirror can block the check).\n"+
			"A new image can be published a few minutes before its signature, so try again later.\n"+
			"See https://frostyard.github.io/intuneme/troubleshooting/#image-signature-check-failed\n"+
			"To install an image that cannot be verified (not recommended), run again with --insecure-skip-signature-check", image, err)
	}
	rep.Message("Signature verified: %s", pullRef)
	return pullRef, nil
}
