package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateProductName(t *testing.T) {
	tests := []struct {
		name, input string
		wantErr     bool
	}{
		{name: "model", input: "ThinkPad X1 Carbon Gen 11"},
		{name: "maximum", input: strings.Repeat("x", 64)},
		{name: "punctuation", input: "Model \"X\" (Gen-1): ~!"},
		{name: "empty", wantErr: true},
		{name: "too long", input: strings.Repeat("x", 65), wantErr: true},
		{name: "embedded newline", input: "bad\nname", wantErr: true},
		{name: "trailing newline", input: "name\n", wantErr: true},
		{name: "tab", input: "bad\tname", wantErr: true},
		{name: "DEL", input: "bad\x7fname", wantErr: true},
		{name: "NUL", input: "bad\x00name", wantErr: true},
		{name: "non ASCII", input: "Café", wantErr: true},
		{name: "leading space", input: " name", wantErr: true},
		{name: "trailing space", input: "name ", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateProductName(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateProductName(%q) = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if err != nil && !strings.Contains(err.Error(), fmt.Sprintf("%q", tt.input)) {
				t.Errorf("error does not quote input: %v", err)
			}
		})
	}
}

func TestContainerProductName(t *testing.T) {
	for _, name := range []string{"", "Test Model", "bad\nname"} {
		t.Run(fmt.Sprintf("%q", name), func(t *testing.T) {
			cfg := &Config{ProductName: name}
			got, err := cfg.ContainerProductName()
			if name == "bad\nname" {
				if err == nil || !strings.Contains(err.Error(), "invalid product_name in config.toml: ") || got != "" {
					t.Fatalf("ContainerProductName = %q, %v", got, err)
				}
				return
			}
			if err != nil || got != name {
				t.Fatalf("ContainerProductName = %q, %v; want %q", got, err, name)
			}
		})
	}
}

func TestLoadProductNameDoesNotValidate(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte("product_name = \"bad\\nname\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ProductName != "bad\nname" {
		t.Fatalf("ProductName = %q", cfg.ProductName)
	}
}

func TestSaveProductName(t *testing.T) {
	for _, name := range []string{"", "Test Model"} {
		t.Run(fmt.Sprintf("%q", name), func(t *testing.T) {
			root := t.TempDir()
			cfg := &Config{ProductName: name}
			if err := cfg.Save(root); err != nil {
				t.Fatal(err)
			}
			loaded, err := Load(root)
			if err != nil || loaded.ProductName != name {
				t.Fatalf("Load = %v, %v", loaded, err)
			}
			data, err := os.ReadFile(filepath.Join(root, "config.toml"))
			if err != nil {
				t.Fatal(err)
			}
			if name == "" && strings.Contains(string(data), "product_name") {
				t.Fatal("empty product_name was not omitted")
			}
		})
	}
}
