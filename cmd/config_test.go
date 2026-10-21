package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDBusServiceContentMakesRootAbsolute(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	got, err := dbusServiceContent("/usr/bin/intuneme", "./x")
	if err != nil {
		t.Fatal(err)
	}
	want := "broker-proxy --root " + filepath.Join(wd, "x") + "\n"
	if !strings.Contains(got, want) {
		t.Errorf("content %q does not contain %q", got, want)
	}
}

func TestDBusServiceContentKeepsAbsoluteRoot(t *testing.T) {
	got, err := dbusServiceContent("/usr/bin/intuneme", "/srv/in")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "broker-proxy --root /srv/in\n") {
		t.Errorf("unexpected content %q", got)
	}
}
