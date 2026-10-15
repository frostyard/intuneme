package cmd

import (
	"strings"
	"testing"

	"github.com/frostyard/intuneme/internal/config"
)

func TestValidateOpenAppForImage(t *testing.T) {
	tests := []struct {
		name       string
		himmelblau bool
		command    string
		wantErr    string
	}{
		{name: "Microsoft Edge in Microsoft image", command: "microsoft-edge"},
		{name: "Intune Portal in Microsoft image", command: "intune-portal"},
		{name: "Microsoft Edge unavailable in Himmelblau image", himmelblau: true, command: "microsoft-edge", wantErr: "Microsoft Edge is not included"},
		{name: "Intune Portal unavailable in Himmelblau image", himmelblau: true, command: "intune-portal", wantErr: "Intune Portal is not included"},
		{name: "other Himmelblau application", himmelblau: true, command: "other-app"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateOpenAppForImage(&config.Config{Himmelblau: tt.himmelblau}, tt.command)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("validateOpenAppForImage() error = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("validateOpenAppForImage() error = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}
