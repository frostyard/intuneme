package version

import "regexp"

// Version is set from main.go at startup via ldflags.
var Version = "dev"

const imageBase = "ghcr.io/frostyard/ubuntu-intune"

var semverRe = regexp.MustCompile(`^v?(\d+\.\d+\.\d+)$`)

// ImageRef returns the full OCI image reference for the container.
// Release versions (clean semver) get a pinned tag; everything else gets latest.
// When insiders is true, the tag is always "insiders".
func ImageRef(insiders bool) string {
	if insiders {
		return imageBase + ":insiders"
	}
	m := semverRe.FindStringSubmatch(Version)
	if m == nil {
		return imageBase + ":latest"
	}
	return imageBase + ":v" + m[1]
}
