package main

import (
	"os"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// The image signature check in internal/imagesig reads only the legacy cosign
// format: a manifest at tag sha256-<hex>.sig in the same repository, signed by
// ubuntu-intune/cosign.pub. The workflow that signs the image runs only on main
// and on tags, so this test is its pull-request gate: it fails when the signing
// step starts using another format, another key or a keyless flow.
func TestImageSigningContract(t *testing.T) {
	data, err := os.ReadFile(".github/workflows/build-container.yml")
	if err != nil {
		t.Fatalf("read the image workflow: %v", err)
	}
	var workflow struct {
		Jobs map[string]struct {
			Steps []struct {
				Name string `yaml:"name"`
				Run  string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatalf("parse the image workflow: %v", err)
	}

	const howToFix = "internal/imagesig reads only the legacy .sig format signed by ubuntu-intune/cosign.pub, so the signing step and the verifier must change in the same pull request"

	imageSigns := 0
	for jobName, job := range workflow.Jobs {
		for _, step := range job.Steps {
			for _, command := range cosignSignCommands(step.Run) {
				if !strings.Contains(command, "--key env://COSIGN_PRIVATE_KEY") {
					t.Errorf("%s > %s: %q must sign with the repository key (--key env://COSIGN_PRIVATE_KEY); %s",
						jobName, step.Name, command, howToFix)
				}
				// A command that signs an image tag writes the legacy .sig
				// manifest the verifier reads. A command that signs a digest
				// (the SBOM artifact, which has no tag) is not read by the
				// verifier, so only the key is pinned there.
				if strings.Contains(command, "@") {
					continue
				}
				imageSigns++
				if !strings.Contains(command, "--new-bundle-format=false") {
					t.Errorf("%s > %s: %q must pass --new-bundle-format=false, or the signature lands in a format the verifier does not read; %s",
						jobName, step.Name, command, howToFix)
				}
			}
		}
	}
	if imageSigns == 0 {
		t.Fatalf("no `cosign sign` command signs an image tag: the image is unsigned, so init and recreate cannot check it. %s", howToFix)
	}
}

// cosignSignCommands returns the `cosign sign` commands of 1 run script, with
// its line continuations joined.
func cosignSignCommands(script string) []string {
	script = strings.ReplaceAll(script, "\\\n", " ")
	var commands []string
	for _, line := range strings.Split(script, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "cosign" || fields[1] != "sign" {
			continue
		}
		commands = append(commands, strings.TrimSpace(line))
	}
	return commands
}
