package cmd

import (
	"strings"
	"testing"
)

func TestValidatePassword(t *testing.T) {
	tests := []struct {
		name     string
		username string
		password string
		wantErr  bool
		errMsgs  []string
	}{
		{
			name:     "valid password",
			username: "alice",
			password: "Correct3Horse!",
			wantErr:  false,
		},
		{
			name:     "too short",
			username: "alice",
			password: "Short1!A",
			wantErr:  true,
			errMsgs:  []string{"at least 12 characters"},
		},
		{
			name:     "exactly 12 chars valid",
			username: "alice",
			password: "Aa1!Aa1!Aa1!",
			wantErr:  false,
		},
		{
			name:     "missing digit",
			username: "alice",
			password: "NoDigitsHere!A",
			wantErr:  true,
			errMsgs:  []string{"at least one digit"},
		},
		{
			name:     "missing uppercase",
			username: "alice",
			password: "nouppercase1!aa",
			wantErr:  true,
			errMsgs:  []string{"at least one uppercase"},
		},
		{
			name:     "missing lowercase",
			username: "alice",
			password: "NOLOWERCASE1!AA",
			wantErr:  true,
			errMsgs:  []string{"at least one lowercase"},
		},
		{
			name:     "missing special character",
			username: "alice",
			password: "NoSpecialChar1A",
			wantErr:  true,
			errMsgs:  []string{"at least one special character"},
		},
		{
			name:     "contains username (exact)",
			username: "alice",
			password: "alice-Passw0rd!",
			wantErr:  true,
			errMsgs:  []string{"must not contain your username"},
		},
		{
			name:     "contains username (case insensitive)",
			username: "alice",
			password: "ALICE-Passw0rd!",
			wantErr:  true,
			errMsgs:  []string{"must not contain your username"},
		},
		{
			name:     "multiple failures reported together",
			username: "alice",
			password: "short",
			wantErr:  true,
			errMsgs:  []string{"at least 12 characters", "at least one digit", "at least one uppercase"},
		},
		{
			name:     "empty username skips usercheck",
			username: "",
			password: "Correct3Horse!",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePassword(tt.username, tt.password)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				for _, msg := range tt.errMsgs {
					if !contains(err.Error(), msg) {
						t.Errorf("expected error containing %q, got: %v", msg, err)
					}
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}

// contains is a helper to check substring membership.
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
