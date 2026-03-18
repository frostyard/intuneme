package version

import (
	"strings"
	"testing"
)

func TestImageRef(t *testing.T) {
	const registry = "ghcr.io/frostyard/ubuntu-intune"

	tests := []struct {
		version  string
		insiders bool
		want     string
	}{
		{"dev", false, registry + ":latest"},
		{"0.4.0", false, registry + ":v0.4.0"},
		{"v0.4.0", false, registry + ":v0.4.0"},
		{"1.0.0", false, registry + ":v1.0.0"},
		{"v1.0.0", false, registry + ":v1.0.0"},
		{"v0.4.0-2-g98e23e6", false, registry + ":latest"},
		{"v0.4.0-dirty", false, registry + ":latest"},
		{"none", false, registry + ":latest"},
		{"", false, registry + ":latest"},
		{"v0.4.0-rc1", false, registry + ":latest"},
		{"0.4.0-beta.1", false, registry + ":latest"},
		// Insiders always returns :insiders regardless of version.
		{"dev", true, registry + ":insiders"},
		{"0.4.0", true, registry + ":insiders"},
		{"v0.4.0", true, registry + ":insiders"},
		{"v0.4.0-2-g98e23e6", true, registry + ":insiders"},
	}

	for _, tt := range tests {
		name := tt.version
		if tt.insiders {
			name += "/insiders"
		}
		t.Run(name, func(t *testing.T) {
			Version = tt.version
			got := ImageRef(tt.insiders)
			if got != tt.want {
				t.Errorf("ImageRef(%v) = %q, want %q", tt.insiders, got, tt.want)
			}
		})
	}
}

func FuzzImageRef(f *testing.F) {
	f.Add("dev", false)
	f.Add("v0.4.0", false)
	f.Add("0.4.0", false)
	f.Add("v0.4.0-2-g98e23e6", false)
	f.Add("v0.4.0-dirty", false)
	f.Add("", false)
	f.Add("v0.4.0-rc1", false)
	f.Add("999.999.999", false)
	f.Add("dev", true)
	f.Add("v0.4.0", true)

	f.Fuzz(func(t *testing.T, version string, insiders bool) {
		Version = version
		ref := ImageRef(insiders)
		// Must always return a valid image reference starting with the base.
		if !strings.HasPrefix(ref, "ghcr.io/frostyard/ubuntu-intune:") {
			t.Errorf("ImageRef(%v) = %q, missing expected prefix", insiders, ref)
		}
	})
}
