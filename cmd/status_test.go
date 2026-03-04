package cmd

import (
	"bytes"
	"encoding/json"
	"os"
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
}
