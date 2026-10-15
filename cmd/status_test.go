package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/frostyard/clix"
)

func TestStatusJSON(t *testing.T) {
	// Set JSON mode directly (--json flag is registered by clix.App.Run,
	// which isn't called in tests).
	clix.JSONOutput = true
	defer func() { clix.JSONOutput = false }()

	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	rootDir = t.TempDir()
	if err := os.MkdirAll(filepath.Join(rootDir, "rootfs"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "config.toml"), []byte(
		"himmelblau = true\nhimmelblau_upn = \"alice@example.com\"\n",
	), 0644); err != nil {
		t.Fatal(err)
	}
	rootCmd.SetArgs([]string{"status"})
	_ = rootCmd.Execute()

	_ = w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	if buf.Len() == 0 {
		t.Fatal("expected JSON output, got nothing")
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("expected valid JSON, got: %s", buf.String())
	}

	if _, ok := result["initialized"]; !ok {
		t.Fatal("expected 'initialized' key in JSON output")
	}
	if result["identity"] != "himmelblau-experimental" {
		t.Fatalf("identity = %v, want himmelblau-experimental", result["identity"])
	}
	if result["channel"] != "community-nightly" {
		t.Fatalf("channel = %v, want community-nightly", result["channel"])
	}
	if result["himmelblau_enrolled"] != true {
		t.Fatalf("himmelblau_enrolled = %v, want true", result["himmelblau_enrolled"])
	}
}
