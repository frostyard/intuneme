package main

import (
	"os"
	"slices"
	"testing"

	"go.yaml.in/yaml/v3"
)

type dependabotConfig struct {
	Updates []struct {
		Ecosystem string `yaml:"package-ecosystem"`
		Directory string `yaml:"directory"`
		Ignore    []struct {
			DependencyName string   `yaml:"dependency-name"`
			UpdateTypes    []string `yaml:"update-types"`
		} `yaml:"ignore"`
	} `yaml:"updates"`
}

// TestDependabotKeepsUbuntuLTS pins the ubuntu-intune base image to its LTS
// release. Microsoft supports Intune only on Ubuntu LTS releases, and
// Dependabot reads an interim release (24.04 to 24.10) as a minor update, so
// ignoring majors alone does not stop it. Digest updates must stay allowed,
// so patch updates are not ignored.
func TestDependabotKeepsUbuntuLTS(t *testing.T) {
	data, err := os.ReadFile(".github/dependabot.yml")
	if err != nil {
		t.Fatal(err)
	}
	var cfg dependabotConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	for _, u := range cfg.Updates {
		if u.Ecosystem != "docker" || u.Directory != "/ubuntu-intune" {
			continue
		}
		for _, ig := range u.Ignore {
			if ig.DependencyName != "library/ubuntu" {
				continue
			}
			for _, want := range []string{"version-update:semver-major", "version-update:semver-minor"} {
				if !slices.Contains(ig.UpdateTypes, want) {
					t.Errorf("library/ubuntu ignore lacks %q: an interim Ubuntu release would be proposed", want)
				}
			}
			if slices.Contains(ig.UpdateTypes, "version-update:semver-patch") {
				t.Error("library/ubuntu ignore must not ignore semver-patch: keep digest updates of the LTS tag")
			}
			return
		}
		t.Fatal("docker /ubuntu-intune has no ignore for library/ubuntu")
	}
	t.Fatal("dependabot.yml has no docker update for /ubuntu-intune")
}
