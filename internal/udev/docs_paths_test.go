package udev

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestDocsUseRulesDir keeps every documented path to an intuneme udev rules
// file in step with RulesDir. The rules are runtime-only, so a doc that points
// users at /etc/udev/rules.d makes a working install look broken (#279).
func TestDocsUseRulesDir(t *testing.T) {
	root := filepath.Join("..", "..")
	rulePath := regexp.MustCompile(`(/[A-Za-z0-9._/-]*)/(` + regexp.QuoteMeta(RulesFile) + `|` + regexp.QuoteMeta(VideoRulesFile) + `)`)
	files := []string{filepath.Join(root, "README.md"), filepath.Join(root, "AGENTS.md")}
	for _, dir := range []string{"site", "docs"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && strings.HasSuffix(path, ".md") {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	checked := 0
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			for _, m := range rulePath.FindAllStringSubmatch(line, -1) {
				checked++
				if m[1] != RulesDir {
					t.Errorf("%s:%d: %s is not in %s", file, i+1, m[0], RulesDir)
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("found no documented rules path; the scan is broken")
	}
}
