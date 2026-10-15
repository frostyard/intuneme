package version

import (
	"regexp"
)

// Version is set from main.go at startup via ldflags.
var Version = "dev"

const imageBase = "ghcr.io/frostyard/ubuntu-intune"
const himmelblauImageBase = "ghcr.io/frostyard/ubuntu-himmelblau"

var semverRe = regexp.MustCompile(`^v?(\d+\.\d+\.\d+)$`)

// ImageRef returns the registry image reference for the Microsoft or standard
// Himmelblau image. The terminal-passkey variant is local-build-only and is
// supplied through the local image override instead.
func ImageRef(insiders, himmelblau bool) string {
	base := imageBase
	if himmelblau {
		base = himmelblauImageBase
	} else if insiders {
		return base + ":insiders"
	}
	m := semverRe.FindStringSubmatch(Version)
	if m == nil {
		return base + ":latest"
	}
	return base + ":v" + m[1]
}
