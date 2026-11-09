package main

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// riskCategories are the categories that frostyard/core's risk-paths spec
// (docs/specs/risk-paths.md, section B) allows as the start of a `why`.
var riskCategories = []string{
	"boot chain",
	"signing and keys",
	"update verification",
	"auth",
	"publication",
	"secrets and credentials",
	"rulesets and settings",
	"destructive operations",
	"governance",
}

type riskPath struct {
	Glob  string `json:"glob"`
	Level string `json:"level"`
	Why   string `json:"why"`
}

func readRiskPaths(t *testing.T) []riskPath {
	t.Helper()
	data, err := os.ReadFile(".github/risk-paths.json")
	if err != nil {
		t.Fatal(err)
	}
	var list struct {
		Paths []riskPath `json:"paths"`
	}
	if err := json.Unmarshal(data, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Paths) == 0 {
		t.Fatal("found 0 risk paths: the file is probably wrong")
	}
	return list.Paths
}

// riskGlob turns a risk-path glob into a regular expression: `**` matches any
// path, `*` matches within 1 path segment.
func riskGlob(glob string) *regexp.Regexp {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(glob); i++ {
		switch {
		case strings.HasPrefix(glob[i:], "**"):
			b.WriteString(".*")
			i++
		case glob[i] == '*':
			b.WriteString("[^/]*")
		default:
			b.WriteString(regexp.QuoteMeta(string(glob[i])))
		}
	}
	b.WriteString("$")
	return regexp.MustCompile(b.String())
}

func riskCovered(paths []riskPath, file string) bool {
	for _, p := range paths {
		if riskGlob(p.Glob).MatchString(file) {
			return true
		}
	}
	return false
}

// riskCategoryOK reports whether a `why` starts with an allowed category and
// a colon.
func riskCategoryOK(why string) bool {
	for _, c := range riskCategories {
		if strings.HasPrefix(why, c+": ") {
			return true
		}
	}
	return false
}

// riskWhyOK reports whether an entry's `why` fits its level: an L1 or L2 entry,
// and an entry with no level (which counts as L1), must start its `why` with a
// category of core's risk-paths spec and a colon; L3 and L4 take free text.
func riskWhyOK(p riskPath) bool {
	if p.Level == "L3" || p.Level == "L4" {
		return true
	}
	return riskCategoryOK(p.Why)
}

func TestRiskPathCategories(t *testing.T) {
	paths := readRiskPaths(t)
	for _, p := range paths {
		if !riskWhyOK(p) {
			t.Errorf("%s (%s): why %q must start with 1 of the risk-paths spec categories and a colon: %s", p.Glob, p.Level, p.Why, strings.Join(riskCategories, ", "))
		}
	}
	t.Logf("checked %d risk-path categories", len(paths))
}

func TestRiskWhyOK(t *testing.T) {
	cases := []struct {
		name string
		path riskPath
		want bool
	}{
		{"L3 takes free text", riskPath{Glob: "docs/**", Level: "L3", Why: "needs a careful read"}, true},
		{"L1 with a category", riskPath{Glob: "main.go", Level: "L1", Why: "auth: the root helper"}, true},
		{"L1 without a category", riskPath{Glob: "main.go", Level: "L1", Why: "the root helper"}, false},
		{"no level without a category", riskPath{Glob: "main.go", Why: "the root helper"}, false},
		{"L2 with a category", riskPath{Glob: ".github/workflows/**", Level: "L2", Why: "governance: CI and token permissions"}, true},
		{"L2 without a category", riskPath{Glob: ".github/workflows/**", Level: "L2", Why: "the workflows"}, false},
	}
	for _, tc := range cases {
		if got := riskWhyOK(tc.path); got != tc.want {
			t.Errorf("%s: riskWhyOK(%q at %s) = %v, want %v", tc.name, tc.path.Why, tc.path.Level, got, tc.want)
		}
	}
}

func TestRiskCategoryOK(t *testing.T) {
	cases := map[string]bool{
		"auth: the root helper":            true,
		"governance: the list":             true,
		"supply chain: the tool pins":      false,
		"privileged: the root helper":      false,
		"auth the root helper":             false,
		"authorization: the polkit action": false,
	}
	for why, want := range cases {
		if got := riskCategoryOK(why); got != want {
			t.Errorf("riskCategoryOK(%q) = %v, want %v", why, got, want)
		}
	}
}

// privilegedRequired are the entry points that install or remove host files,
// or dispatch the root helpers, without a "sudo" literal of their own. Each
// must stay covered even if the scan below stops finding it.
var privilegedRequired = []string{
	"main.go",        // dispatches nsenter-exec and machine-exec, which run as root
	"cmd/init.go",    // sudoers.Install with an untrusted source, provision.Install*
	"cmd/udev.go",    // udev.Install and udev.Remove
	"cmd/stop.go",    // udev.Remove
	"cmd/start.go",   // refreshes helpers, sudoers and polkit files
	"cmd/destroy.go", // deletes the rootfs and the host privilege files
	"cmd/recreate.go",
	"cmd/extension.go",
}

// privilegedCall matches a call of a function that installs or removes host
// privilege files, or runs a root helper, in the packages that do that.
var privilegedCall = regexp.MustCompile(`\b(sudoers|udev|nshelper|provision)\.(Install\w*|Remove\w*|Main|MachineMain)\(`)

func TestPrivilegedCallPattern(t *testing.T) {
	cases := map[string]bool{
		`if err := sudoers.Install(r, opts); err != nil {`: true,
		`os.Exit(nshelper.MachineMain(os.Args[1:]))`:       true,
		`return provision.InstallSELinuxPolicy(r, p)`:      true,
		`if err := udev.Remove(r); err != nil {`:           true,
		`devs := udev.DetectVideoDevices()`:                false,
		`cfg, err := config.Load(root)`:                    false,
	}
	for line, want := range cases {
		if got := privilegedCall.MatchString(line); got != want {
			t.Errorf("privilegedCall on %q = %v, want %v", line, got, want)
		}
	}
}

// TestRiskPathsCoverPrivilegedFiles holds .github/risk-paths.json to the
// files that run something as root or install something: the required entry
// points above, every non-test Go file that calls sudo or a privileged
// install, remove or helper function, and every shell script in scripts/.
func TestRiskPathsCoverPrivilegedFiles(t *testing.T) {
	paths := readRiskPaths(t)
	for _, file := range privilegedRequired {
		if _, err := os.Stat(file); err != nil {
			t.Errorf("required privileged entry point %s: %v (update privilegedRequired)", file, err)
		}
	}
	checked := append([]string(nil), privilegedRequired...)
	sudoCall := regexp.MustCompile(`"sudo"`)
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".agents", "node_modules", "dist", "_site":
				return filepath.SkipDir
			}
			return nil
		}
		switch {
		case strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go"):
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if sudoCall.Match(data) || privilegedCall.Match(data) {
				checked = append(checked, path)
			}
		case strings.HasPrefix(path, "scripts/") && strings.HasSuffix(path, ".sh"):
			checked = append(checked, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(checked) == 0 {
		t.Fatal("found 0 privileged files: the walk is probably wrong")
	}
	seen := map[string]bool{}
	for _, file := range checked {
		if seen[file] {
			continue
		}
		seen[file] = true
		if !riskCovered(paths, file) {
			t.Errorf("%s runs sudo, installs or removes host privilege files, or is a script, but no glob in .github/risk-paths.json matches it", file)
		}
	}
	t.Logf("checked %d privileged files against %d risk paths", len(seen), len(paths))
}

func TestRiskGlob(t *testing.T) {
	cases := []struct {
		glob, file string
		want       bool
	}{
		{"internal/sudo/**", "internal/sudo/sudo.go", true},
		{"internal/sudo/**", "internal/sudoers/sudoers.go", false},
		{"cmd/extension/*.policy", "cmd/extension/org.frostyard.intuneme.policy", true},
		{"cmd/extension/*.policy", "cmd/extension/sub/x.policy", false},
		{"cmd/start.go", "cmd/start_go", false},
		{".github/workflows/**", ".github/workflows/ci.yml", true},
	}
	for _, tc := range cases {
		if got := riskGlob(tc.glob).MatchString(tc.file); got != tc.want {
			t.Errorf("riskGlob(%q) on %q = %v, want %v", tc.glob, tc.file, got, tc.want)
		}
	}
}
