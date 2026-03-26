package cmd

import (
	"fmt"
	"strings"
	"testing"

	"github.com/frostyard/clix"
)

// messageRecorder captures Message calls for test assertions.
type messageRecorder struct {
	messages []string
}

func (r *messageRecorder) Step(int, int, string)       {}
func (r *messageRecorder) Progress(int, string)        {}
func (r *messageRecorder) MessagePlain(string, ...any) {}
func (r *messageRecorder) Warning(string, ...any)      {}
func (r *messageRecorder) Error(error, string)         {}
func (r *messageRecorder) Complete(string, any)        {}
func (r *messageRecorder) IsJSON() bool                { return false }
func (r *messageRecorder) Message(format string, args ...any) {
	if len(args) > 0 {
		r.messages = append(r.messages, fmt.Sprintf(format, args...))
	} else {
		r.messages = append(r.messages, format)
	}
}

func TestDestroyDryRun(t *testing.T) {
	rec := &messageRecorder{}
	origRep := rep
	rep = rec
	defer func() { rep = origRep }()

	origDryRun := clix.DryRun
	clix.DryRun = true
	defer func() { clix.DryRun = origDryRun }()

	origAll := destroyAll
	destroyAll = false
	defer func() { destroyAll = origAll }()

	origRoot := rootDir
	rootDir = t.TempDir()
	defer func() { rootDir = origRoot }()

	if err := destroyCmd.RunE(destroyCmd, nil); err != nil {
		t.Fatalf("destroy --dry-run failed: %v", err)
	}

	joined := strings.Join(rec.messages, "\n")

	for _, want := range []string{
		"Would remove udev rules",
		"Would remove polkit rule",
		"Would remove rootfs at",
		"Would clean Intune state from ~/Intune",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("dry-run output missing %q\ngot:\n%s", want, joined)
		}
	}

	// Default dry-run must NOT include --all messages.
	for _, notWant := range []string{
		"Would disable and remove GNOME extension",
		"Would remove polkit policy action",
		"Would remove D-Bus broker service file",
		"Would remove ~/Intune entirely",
	} {
		if strings.Contains(joined, notWant) {
			t.Errorf("dry-run output should not contain %q without --all\ngot:\n%s", notWant, joined)
		}
	}
}

func TestDestroyDryRunAll(t *testing.T) {
	rec := &messageRecorder{}
	origRep := rep
	rep = rec
	defer func() { rep = origRep }()

	origDryRun := clix.DryRun
	clix.DryRun = true
	defer func() { clix.DryRun = origDryRun }()

	origAll := destroyAll
	destroyAll = true
	defer func() { destroyAll = origAll }()

	origRoot := rootDir
	rootDir = t.TempDir()
	defer func() { rootDir = origRoot }()

	if err := destroyCmd.RunE(destroyCmd, nil); err != nil {
		t.Fatalf("destroy --all --dry-run failed: %v", err)
	}

	joined := strings.Join(rec.messages, "\n")

	for _, want := range []string{
		"Would remove udev rules",
		"Would remove polkit rule",
		"Would remove rootfs at",
		"Would disable and remove GNOME extension",
		"Would remove polkit policy action",
		"Would remove D-Bus broker service file",
		"Would remove ~/Intune entirely",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("--all dry-run output missing %q\ngot:\n%s", want, joined)
		}
	}

	if strings.Contains(joined, "Would clean Intune state from ~/Intune") {
		t.Errorf("--all dry-run should not contain default cleanup message\ngot:\n%s", joined)
	}
}

func TestDestroyHelpShowsAllFlag(t *testing.T) {
	usage := destroyCmd.UsageString()
	if !strings.Contains(usage, "--all") {
		t.Errorf("destroy --help should show --all flag\ngot:\n%s", usage)
	}
}
