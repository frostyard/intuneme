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

// TestDependabotFollowsBaselineB13 holds .github/dependabot.yml to row B13 of
// core's repository baseline (frostyard/core ADR-0075, ADR-0089): each
// entry's schedule is only `interval: monthly`, each ecosystem has exactly 1
// entry, and each entry has exactly 1 group, with `patterns: ["*"]`, no
// `group-by` and no `applies-to: security-updates`, so security updates stay
// ungrouped and immediate.
func TestDependabotFollowsBaselineB13(t *testing.T) {
	data, err := os.ReadFile(".github/dependabot.yml")
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Updates []struct {
			Ecosystem string                    `yaml:"package-ecosystem"`
			Schedule  map[string]any            `yaml:"schedule"`
			Groups    map[string]map[string]any `yaml:"groups"`
		} `yaml:"updates"`
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Updates) == 0 {
		t.Fatal("dependabot.yml has no updates")
	}
	seen := map[string]bool{}
	for _, u := range cfg.Updates {
		if u.Ecosystem == "" {
			t.Error("an entry has no package-ecosystem")
			continue
		}
		if seen[u.Ecosystem] {
			t.Errorf("%s: the ecosystem is in 2 or more entries; use 1 entry with a directories list", u.Ecosystem)
		}
		seen[u.Ecosystem] = true
		if len(u.Schedule) != 1 || u.Schedule["interval"] != "monthly" {
			t.Errorf("%s: schedule is %v, want only interval: monthly", u.Ecosystem, u.Schedule)
		}
		if len(u.Groups) != 1 {
			t.Errorf("%s: %d groups, want exactly 1", u.Ecosystem, len(u.Groups))
			continue
		}
		for name, g := range u.Groups {
			p, ok := g["patterns"].([]any)
			if !ok || len(p) != 1 || p[0] != "*" {
				t.Errorf("%s: group %s patterns are %v, want [\"*\"]", u.Ecosystem, name, g["patterns"])
			}
			if _, ok := g["group-by"]; ok {
				t.Errorf("%s: group %s has group-by", u.Ecosystem, name)
			}
			if g["applies-to"] == "security-updates" {
				t.Errorf("%s: group %s applies to security updates; they must stay ungrouped", u.Ecosystem, name)
			}
		}
	}
	for _, want := range []string{"gomod", "github-actions", "docker"} {
		if !seen[want] {
			t.Errorf("dependabot.yml has no %s entry", want)
		}
	}
}
