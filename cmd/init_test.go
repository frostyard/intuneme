package cmd

import (
	"testing"
)

func TestValidatePassword(t *testing.T) {
	tests := []struct {
		name     string
		username string
		password string
		wantErr  bool
		errMsg   string
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
			errMsg:   "at least 12 characters",
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
			errMsg:   "at least one digit",
		},
		{
			name:     "missing uppercase",
			username: "alice",
			password: "nouppercase1!aa",
			wantErr:  true,
			errMsg:   "at least one uppercase",
		},
		{
			name:     "missing lowercase",
			username: "alice",
			password: "NOLOWERCASE1!AA",
			wantErr:  true,
			errMsg:   "at least one lowercase",
		},
		{
			name:     "missing special character",
			username: "alice",
			password: "NoSpecialChar1A",
			wantErr:  true,
			errMsg:   "at least one special character",
		},
		{
			name:     "contains username (exact)",
			username: "alice",
			password: "alice-Passw0rd!",
			wantErr:  true,
			errMsg:   "must not contain your username",
		},
		{
			name:     "contains username (case insensitive)",
			username: "alice",
			password: "ALICE-Passw0rd!",
			wantErr:  true,
			errMsg:   "must not contain your username",
		},
		{
			name:     "multiple failures reported together",
			username: "alice",
			password: "short",
			wantErr:  true,
			errMsg:   "at least 12 characters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePassword(tt.username, tt.password)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tt.errMsg != "" && !contains(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing %q, got: %v", tt.errMsg, err)
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
	return len(substr) == 0 || (len(s) >= len(substr) && func() bool {
		for i := 0; i <= len(s)-len(substr); i++ {
			if s[i:i+len(substr)] == substr {
				return true
			}
		}
		return false
	}())
}
