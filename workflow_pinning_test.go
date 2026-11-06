package main

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var (
	usesLine       = regexp.MustCompile(`^\s*(?:-\s+)?uses:\s*(\S+)(.*)$`)
	pinnedRef      = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+(/[A-Za-z0-9_./-]+)?@[0-9a-f]{40}$`)
	versionComment = regexp.MustCompile(`^\s+#\s*v\S*`)
	// frostyard/core publishes no release tags, so a call of one of its
	// reusable workflows names the source in its comment instead of a version.
	coreWorkflowRef = regexp.MustCompile(`^frostyard/core/\.github/workflows/[A-Za-z0-9_.-]+\.ya?ml@[0-9a-f]{40}$`)
	anyComment      = regexp.MustCompile(`^\s+#\s*\S`)
)

// usesPinned reports whether a `uses:` ref and the rest of its line follow
// ADR-0021: a 40-char commit SHA and a "# v..." comment, or, for a frostyard/core
// reusable workflow (core has no release tags), a SHA and any comment.
func usesPinned(ref, rest string) bool {
	if coreWorkflowRef.MatchString(ref) {
		return anyComment.MatchString(rest)
	}
	return pinnedRef.MatchString(ref) && versionComment.MatchString(rest)
}

func TestUsesPinned(t *testing.T) {
	sha := "1b02a129026329cef2b46d1fbd0465a863c1cd81"
	cases := []struct {
		name, ref, rest string
		want            bool
	}{
		{"action with version", "actions/checkout@" + sha, " # v7.0.1", true},
		{"action without comment", "actions/checkout@" + sha, "", false},
		{"action with other comment", "actions/checkout@" + sha, " # main", false},
		{"action on a tag", "actions/checkout@v7", " # v7", false},
		{"core workflow with source comment", "frostyard/core/.github/workflows/risk-paths.yml@" + sha, " # frostyard/core main", true},
		{"core workflow without comment", "frostyard/core/.github/workflows/risk-paths.yml@" + sha, "", false},
		{"core workflow on a branch", "frostyard/core/.github/workflows/risk-paths.yml@main", " # main", false},
		{"other repository workflow with source comment", "frostyard/other/.github/workflows/x.yml@" + sha, " # main", false},
	}
	for _, tc := range cases {
		if got := usesPinned(tc.ref, tc.rest); got != tc.want {
			t.Errorf("%s: usesPinned(%q, %q) = %v, want %v", tc.name, tc.ref, tc.rest, got, tc.want)
		}
	}
}

// TestActionsPinnedToCommitSHA enforces ADR-0021: every external action is
// pinned to a full commit SHA with a "# v..." version comment (see usesPinned
// for frostyard/core reusable workflows).
func TestActionsPinnedToCommitSHA(t *testing.T) {
	var files []string
	for _, dir := range []string{".github/workflows", ".github/actions"} {
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			ext := filepath.Ext(path)
			if !d.IsDir() && (ext == ".yml" || ext == ".yaml") {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(files) == 0 {
		t.Fatal("found 0 workflow files: the path is probably wrong")
	}
	for _, file := range files {
		f, err := os.Open(file)
		if err != nil {
			t.Fatal(err)
		}
		sc := bufio.NewScanner(f)
		n := 0
		for sc.Scan() {
			n++
			line := sc.Text()
			m := usesLine.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			ref := strings.Trim(m[1], `"'`)
			if strings.HasPrefix(ref, "./") || strings.HasPrefix(ref, "docker://") {
				continue
			}
			if !usesPinned(ref, m[2]) {
				t.Errorf("%s:%d: action must be pinned to a 40-char commit SHA with a `# v...` comment: %s", file, n, strings.TrimSpace(line))
			}
		}
		_ = f.Close()
		if err := sc.Err(); err != nil {
			t.Fatal(err)
		}
	}
}
