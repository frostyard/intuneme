package version

import "testing"

func TestImageRef(t *testing.T) {
	const registry = "ghcr.io/frostyard/ubuntu-intune"

	tests := []struct {
		version string
		want    string
	}{
		{"dev", registry + ":latest"},
		{"0.4.0", registry + ":v0.4.0"},
		{"v0.4.0", registry + ":v0.4.0"},
		{"1.0.0", registry + ":v1.0.0"},
		{"v1.0.0", registry + ":v1.0.0"},
		{"v0.4.0-2-g98e23e6", registry + ":latest"},
		{"v0.4.0-dirty", registry + ":latest"},
		{"none", registry + ":latest"},
		{"", registry + ":latest"},
		{"v0.4.0-rc1", registry + ":latest"},
		{"0.4.0-beta.1", registry + ":latest"},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			Version = tt.version
			got := ImageRef()
			if got != tt.want {
				t.Errorf("ImageRef() = %q, want %q", got, tt.want)
			}
		})
	}
}
