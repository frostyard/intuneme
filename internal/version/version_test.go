package version

import (
	"strings"
	"testing"
)

func TestImageRef(t *testing.T) {
	const (
		registry           = "ghcr.io/frostyard/ubuntu-intune"
		himmelblauRegistry = "ghcr.io/frostyard/ubuntu-himmelblau"
	)

	tests := []struct {
		version    string
		insiders   bool
		himmelblau bool
		want       string
	}{
		{"dev", false, false, registry + ":latest"},
		{"0.4.0", false, false, registry + ":v0.4.0"},
		{"v0.4.0", false, false, registry + ":v0.4.0"},
		{"1.0.0", false, false, registry + ":v1.0.0"},
		{"v1.0.0", false, false, registry + ":v1.0.0"},
		{"v0.4.0-2-g98e23e6", false, false, registry + ":latest"},
		{"v0.4.0-dirty", false, false, registry + ":latest"},
		{"none", false, false, registry + ":latest"},
		{"", false, false, registry + ":latest"},
		{"v0.4.0-rc1", false, false, registry + ":latest"},
		{"0.4.0-beta.1", false, false, registry + ":latest"},
		{"dev", false, true, himmelblauRegistry + ":latest"},
		{"0.4.0", false, true, himmelblauRegistry + ":v0.4.0"},
		// Insiders always returns :insiders regardless of version.
		{"dev", true, false, registry + ":insiders"},
		{"0.4.0", true, false, registry + ":insiders"},
		{"v0.4.0", true, false, registry + ":insiders"},
		{"v0.4.0-2-g98e23e6", true, false, registry + ":insiders"},
		{"dev", true, true, himmelblauRegistry + ":latest"},
	}

	for _, tt := range tests {
		name := tt.version
		if tt.insiders {
			name += "/insiders"
		}
		if tt.himmelblau {
			name += "/himmelblau"
		}
		t.Run(name, func(t *testing.T) {
			Version = tt.version
			got := ImageRef(tt.insiders, tt.himmelblau)
			if got != tt.want {
				t.Errorf("ImageRef(%v, %v) = %q, want %q", tt.insiders, tt.himmelblau, got, tt.want)
			}
		})
	}
}

func FuzzImageRef(f *testing.F) {
	f.Add("dev", false, false)
	f.Add("v0.4.0", false, false)
	f.Add("0.4.0", false, false)
	f.Add("v0.4.0-2-g98e23e6", false, false)
	f.Add("v0.4.0-dirty", false, false)
	f.Add("", false, false)
	f.Add("v0.4.0-rc1", false, false)
	f.Add("999.999.999", false, false)
	f.Add("dev", true, false)
	f.Add("v0.4.0", true, false)
	f.Add("dev", false, true)

	f.Fuzz(func(t *testing.T, version string, insiders, himmelblau bool) {
		Version = version
		ref := ImageRef(insiders, himmelblau)
		// Must always return a valid image reference starting with the base.
		base := registryForTest(insiders, himmelblau)
		if !strings.HasPrefix(ref, base+":") {
			t.Errorf("ImageRef(%v, %v) = %q, missing expected prefix %q", insiders, himmelblau, ref, base+":")
		}
	})
}

func registryForTest(_ bool, himmelblau bool) string {
	if himmelblau {
		return "ghcr.io/frostyard/ubuntu-himmelblau"
	}
	return "ghcr.io/frostyard/ubuntu-intune"
}
