package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func bumpTestEnv() []string {
	var env []string
	for _, entry := range os.Environ() {
		// Do not inherit Git repository selection, configuration, or identity.
		if !strings.HasPrefix(entry, "GIT_") && !strings.HasPrefix(entry, "MAKEFLAGS=") && !strings.HasPrefix(entry, "MFLAGS=") && !strings.HasPrefix(entry, "MAKEFILES=") {
			env = append(env, entry)
		}
	}
	return append(env,
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=Bump Test", "GIT_AUTHOR_EMAIL=bump@example.invalid",
		"GIT_COMMITTER_NAME=Bump Test", "GIT_COMMITTER_EMAIL=bump@example.invalid",
	)
}

func requireBumpTools(t *testing.T) {
	t.Helper()
	for _, tool := range []string{"make", "git"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not on PATH: %v", tool, err)
		}
	}
}

func TestBumpGuard(t *testing.T) {
	requireBumpTools(t)
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		want string
	}{
		{name: "main at origin"},
		{name: "feature", want: "only on main"},
		{name: "detached", want: "only on main"},
		{name: "ahead", want: "HEAD is not origin/main"},
		{name: "behind", want: "HEAD is not origin/main"},
		// HEAD still equals the stale origin/main ref, so only the fetch
		// line stops this run; its error names the missing origin.
		{name: "fetch fails", want: "missing.git"},
		// snapshot.yml moves the rolling dev tag on every main push; a clone
		// that holds the older dev tag must still pass the guard.
		{name: "dev tag moved"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			origin := filepath.Join(dir, "origin.git")
			seed := filepath.Join(dir, "seed")
			work := filepath.Join(dir, "work")
			git := func(dir string, args ...string) {
				t.Helper()
				cmd := exec.Command("git", args...)
				cmd.Dir = dir
				cmd.Env = bumpTestEnv()
				if output, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("git %v: %v\n%s", args, err, output)
				}
			}
			commit := func(dir, content string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(dir, "file"), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
				git(dir, "add", "file")
				git(dir, "commit", "-m", content)
			}
			git(dir, "init", "--bare", "-b", "main", origin)
			// Git before 2.31 names the unborn branch of an empty clone from
			// init.defaultBranch, not from origin. Force that behaviour on
			// every Git (origin hides its unborn HEAD, the clone defaults to
			// master), so the fixture must set the seed branch itself (#302).
			git(origin, "config", "lsrefs.unborn", "ignore")
			git(dir, "-c", "init.defaultBranch=master", "clone", origin, seed)
			git(seed, "checkout", "-B", "main")
			commit(seed, "initial")
			git(seed, "tag", "dev")
			git(seed, "push", "origin", "main", "dev")
			git(dir, "clone", origin, work)
			switch tc.name {
			case "feature":
				git(work, "switch", "-c", "feature")
			case "detached":
				git(work, "checkout", "--detach")
			case "ahead":
				commit(work, "local")
			case "behind":
				commit(seed, "remote")
				git(seed, "push", "origin", "main")
			case "fetch fails":
				git(work, "remote", "set-url", "origin", filepath.Join(dir, "missing.git"))
			case "dev tag moved":
				commit(seed, "snapshot")
				git(seed, "tag", "-f", "dev")
				git(seed, "push", "origin", "main")
				git(seed, "push", "--force", "origin", "dev")
				// A pull follows main but keeps the local dev tag.
				git(work, "pull", "--ff-only", "origin", "main")
			}
			cmd := exec.Command("make", "-f", filepath.Join(root, "Makefile"), "bump-guard")
			cmd.Dir = root
			cmd.Env = append(bumpTestEnv(), "GIT_DIR="+filepath.Join(work, ".git"), "GIT_WORK_TREE="+work)
			output, err := cmd.CombinedOutput()
			if tc.want == "" {
				if err != nil {
					t.Fatalf("bump-guard: %v\n%s", err, output)
				}
			} else if err == nil || !strings.Contains(string(output), tc.want) {
				t.Fatalf("bump-guard error = %v, output = %s; want failure containing %q", err, output, tc.want)
			}
		})
	}
}

func TestBumpRunsGuardBeforeTag(t *testing.T) {
	data, err := os.ReadFile("Makefile")
	if err != nil {
		t.Fatal(err)
	}
	var recipe []string
	found := false
	for _, line := range strings.Split(string(data), "\n") {
		if !found {
			if strings.HasPrefix(line, "bump:") {
				found = true
			}
			continue
		}
		if !strings.HasPrefix(line, "\t") {
			break
		}
		recipe = append(recipe, strings.TrimPrefix(line, "\t"))
	}
	if len(recipe) == 0 || recipe[0] != "@$(MAKE) bump-guard" {
		t.Fatalf("first bump recipe line must be @$(MAKE) bump-guard; got %q", recipe)
	}
	for i, line := range recipe {
		if strings.Contains(line, "svu next") {
			if i == 0 {
				t.Fatal("tag creation must follow bump-guard")
			}
			return
		}
	}
	t.Fatal("bump recipe has no svu next line")
}
