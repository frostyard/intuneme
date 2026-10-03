package main

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// The release workflows run only on tag pushes and after Tests on main, so
// these tests are their pull-request gate.

type workflowStep struct {
	Uses            string            `yaml:"uses"`
	If              string            `yaml:"if"`
	With            map[string]string `yaml:"with"`
	ContinueOnError yaml.Node         `yaml:"continue-on-error"`
}

type workflowJob struct {
	If          string            `yaml:"if"`
	Permissions map[string]string `yaml:"permissions"`
	Steps       []workflowStep    `yaml:"steps"`
}

type workflowFile struct {
	On struct {
		Push struct {
			Tags     []string `yaml:"tags"`
			Branches []string `yaml:"branches"`
		} `yaml:"push"`
	} `yaml:"on"`
	Jobs map[string]workflowJob `yaml:"jobs"`
}

// shaPinned matches an action reference pinned to a full commit SHA
// (frostyard/core ADR-0021).
var shaPinned = regexp.MustCompile(`@[0-9a-f]{40}$`)

func readWorkflow(t *testing.T, path string) workflowFile {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var wf workflowFile
	if err := yaml.Unmarshal(data, &wf); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return wf
}

func releaseJob(t *testing.T) workflowJob {
	t.Helper()
	wf := readWorkflow(t, ".github/workflows/release.yml")
	if len(wf.On.Push.Tags) == 0 {
		t.Fatal("release workflow must run on tag pushes")
	}
	if len(wf.On.Push.Branches) != 0 {
		t.Fatalf("release workflow branch filters = %v, want a tag-only push trigger", wf.On.Push.Branches)
	}
	job, ok := wf.Jobs["goreleaser"]
	if !ok {
		t.Fatal("release workflow is missing the goreleaser job")
	}
	return job
}

// stepIndex returns the index of the first step whose action starts with
// prefix, or -1.
func stepIndex(job workflowJob, prefix string) int {
	for i, step := range job.Steps {
		if strings.HasPrefix(step.Uses, prefix) {
			return i
		}
	}
	return -1
}

// TestReleaseWorkflowRequestsAptPublicationForTags pins the publication
// contract of frostyard/core ADR-0055 and ADR-0056: a tag release asks
// frostyard/apt-publisher to publish its .deb with one unguarded `publish-deb`
// repository_dispatch that may not continue on error (if it fails, nothing
// was published). The publisher, not this workflow, dispatches `build` to
// frostyard/snosi once the package is installable, so a direct snosi dispatch
// or a repogen publish step here is refused.
func TestReleaseWorkflowRequestsAptPublicationForTags(t *testing.T) {
	job := releaseJob(t)
	if job.If != "" {
		t.Fatalf("goreleaser job has guard %q; tag releases must reach the publication request", job.If)
	}

	request := -1
	for i, step := range job.Steps {
		if strings.HasPrefix(step.Uses, "frostyard/repogen/") {
			t.Fatalf("release workflow uses %s; the .deb is published by frostyard/apt-publisher", step.Uses)
		}
		if !strings.HasPrefix(step.Uses, "peter-evans/repository-dispatch@") {
			continue
		}
		switch step.With["repository"] {
		case "frostyard/snosi":
			t.Fatal("release workflow dispatches to frostyard/snosi; frostyard/apt-publisher does after publishing")
		case "frostyard/apt-publisher":
			if request >= 0 {
				t.Fatal("release workflow has more than one request to frostyard/apt-publisher, want 1")
			}
			request = i
			if !shaPinned.MatchString(step.Uses) {
				t.Errorf("publication request uses %q, want it pinned to a full commit SHA", step.Uses)
			}
			if got := step.With["event-type"]; got != "publish-deb" {
				t.Errorf("publication request event-type = %q, want publish-deb", got)
			}
			if got := step.With["token"]; got != "${{ secrets.APT_PUBLISH_TOKEN }}" {
				t.Errorf("publication request token = %q, want ${{ secrets.APT_PUBLISH_TOKEN }}", got)
			}
			if step.If != "" {
				t.Errorf("publication request has guard %q; tag releases must reach it", step.If)
			}
			if !step.ContinueOnError.IsZero() {
				t.Error("publication request sets continue-on-error; a failed request must fail the release")
			}
			payload := step.With["client-payload"]
			for _, want := range []string{`"repo": "${{ github.repository }}"`, `"tag": "${{ github.ref_name }}"`} {
				if !strings.Contains(payload, want) {
					t.Errorf("publication request client-payload %q lacks %s", payload, want)
				}
			}
		}
	}
	if request < 0 {
		t.Fatal("release workflow must send a publish-deb request to frostyard/apt-publisher")
	}

	// apt-publisher downloads the release assets and verifies their
	// attestations as soon as it runs, so both must exist before the request.
	for _, prior := range []string{"goreleaser/goreleaser-action@", "actions/attest-build-provenance@"} {
		if i := stepIndex(job, prior); i < 0 || i > request {
			t.Errorf("%s step must run before the publication request", strings.TrimSuffix(prior, "@"))
		}
	}
}

// TestReleaseWorkflowAttestsDebProvenance pins what apt-publisher's
// registration of intuneme (`attested=yes`) depends on: it refuses any .deb
// without build provenance from this repository's workflow at
// refs/tags/<tag>. The goreleaser job therefore grants id-token and
// attestations write, and runs SHA-pinned actions/attest-build-provenance
// over dist/*.deb.
func TestReleaseWorkflowAttestsDebProvenance(t *testing.T) {
	job := releaseJob(t)
	for _, scope := range []string{"id-token", "attestations"} {
		if got := job.Permissions[scope]; got != "write" {
			t.Errorf("goreleaser job permissions.%s = %q, want write (actions/attest-build-provenance needs it)", scope, got)
		}
	}

	i := stepIndex(job, "actions/attest-build-provenance@")
	if i < 0 {
		t.Fatal("release workflow must run actions/attest-build-provenance over the release assets")
	}
	step := job.Steps[i]
	if !shaPinned.MatchString(step.Uses) {
		t.Errorf("attest step uses %q, want it pinned to a full commit SHA", step.Uses)
	}
	if step.If != "" {
		t.Errorf("attest step has guard %q; every tag release must attest", step.If)
	}
	subjects := strings.Fields(step.With["subject-path"])
	if !slices.Contains(subjects, "dist/*.deb") {
		t.Errorf("attest step subject-path = %v, want it to include dist/*.deb", subjects)
	}
}

// TestSnapshotWorkflowDoesNotPublishPackages keeps the rolling `dev`
// prerelease out of the APT repository. Its .deb is versioned from the next
// major (`incmajor`, for example 1.0.0.dev), which would sort above every real
// release, and its provenance names refs/heads/main rather than a tag.
func TestSnapshotWorkflowDoesNotPublishPackages(t *testing.T) {
	wf := readWorkflow(t, ".github/workflows/snapshot.yml")
	if len(wf.Jobs) == 0 {
		t.Fatal("snapshot workflow has no jobs")
	}
	for name, job := range wf.Jobs {
		for _, step := range job.Steps {
			if strings.HasPrefix(step.Uses, "frostyard/repogen/") {
				t.Errorf("snapshot job %s uses %s; snapshots must not publish packages", name, step.Uses)
			}
			if strings.HasPrefix(step.Uses, "peter-evans/repository-dispatch@") &&
				step.With["repository"] == "frostyard/apt-publisher" {
				t.Errorf("snapshot job %s requests APT publication; only tag releases may", name)
			}
		}
	}
}
