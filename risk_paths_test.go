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
	Glob string `json:"glob"`
	Why  string `json:"why"`
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

func TestRiskPathCategories(t *testing.T) {
	paths := readRiskPaths(t)
	for _, p := range paths {
		if !riskCategoryOK(p.Why) {
			t.Errorf("%s: why %q must start with 1 of the risk-paths spec categories and a colon: %s", p.Glob, p.Why, strings.Join(riskCategories, ", "))
		}
	}
	t.Logf("checked %d risk-path categories", len(paths))
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

// TestRiskPathsCoverPrivilegedFiles holds .github/risk-paths.json to the
// files that run something as root or install something: every non-test Go
// file that calls sudo, and every shell script in scripts/.
func TestRiskPathsCoverPrivilegedFiles(t *testing.T) {
	paths := readRiskPaths(t)
	sudoCall := regexp.MustCompile(`"sudo"`)
	var checked []string
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
			if sudoCall.Match(data) {
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
	for _, file := range checked {
		if !riskCovered(paths, file) {
			t.Errorf("%s runs sudo or is an install script, but no glob in .github/risk-paths.json matches it", file)
		}
	}
	t.Logf("checked %d privileged files against %d risk paths", len(checked), len(paths))
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
