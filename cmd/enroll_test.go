package cmd

import (
	"errors"
	"reflect"
	"testing"
)

func TestPrepareHimmelblauEnrollmentContainerStartsStoppedContainer(t *testing.T) {
	var calls []string
	err := prepareHimmelblauEnrollmentContainer(
		false,
		false,
		func() error { calls = append(calls, "start"); return nil },
		func() error { calls = append(calls, "reboot"); return nil },
		func() error { calls = append(calls, "wait"); return nil },
	)
	if err != nil {
		t.Fatalf("prepareHimmelblauEnrollmentContainer() error: %v", err)
	}
	if want := []string{"start", "wait"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("actions = %v, want %v", calls, want)
	}
}

func TestPrepareHimmelblauEnrollmentContainerRebootsRunningFirstEnrollment(t *testing.T) {
	var calls []string
	err := prepareHimmelblauEnrollmentContainer(
		true,
		false,
		func() error { calls = append(calls, "start"); return nil },
		func() error { calls = append(calls, "reboot"); return nil },
		func() error { calls = append(calls, "wait"); return nil },
	)
	if err != nil {
		t.Fatalf("prepareHimmelblauEnrollmentContainer() error: %v", err)
	}
	if want := []string{"reboot"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("actions = %v, want %v", calls, want)
	}
}

func TestPrepareHimmelblauEnrollmentContainerWaitsForMappedRunningContainer(t *testing.T) {
	var calls []string
	err := prepareHimmelblauEnrollmentContainer(
		true,
		true,
		func() error { calls = append(calls, "start"); return nil },
		func() error { calls = append(calls, "reboot"); return nil },
		func() error { calls = append(calls, "wait"); return nil },
	)
	if err != nil {
		t.Fatalf("prepareHimmelblauEnrollmentContainer() error: %v", err)
	}
	if want := []string{"wait"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("actions = %v, want %v", calls, want)
	}
}

func TestPrepareHimmelblauEnrollmentContainerReturnsStartFailure(t *testing.T) {
	startErr := errors.New("start failed")
	err := prepareHimmelblauEnrollmentContainer(
		false,
		false,
		func() error { return startErr },
		func() error { return nil },
		func() error { return nil },
	)
	if !errors.Is(err, startErr) {
		t.Fatalf("prepareHimmelblauEnrollmentContainer() error = %v, want %v", err, startErr)
	}
}

func TestValidateHimmelblauUPN(t *testing.T) {
	tests := []struct {
		name    string
		upn     string
		wantErr bool
	}{
		{name: "normal UPN", upn: "alice@example.com"},
		{name: "plus tag", upn: "alice+intune@example.com"},
		{name: "empty", upn: "", wantErr: true},
		{name: "missing local part", upn: "@example.com", wantErr: true},
		{name: "missing domain", upn: "alice@", wantErr: true},
		{name: "multiple at signs", upn: "alice@dept@example.com", wantErr: true},
		{name: "whitespace", upn: "alice smith@example.com", wantErr: true},
		{name: "path separator", upn: "alice/../root@example.com", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateHimmelblauUPN(tt.upn)
			if tt.wantErr && err == nil {
				t.Fatal("expected UPN validation error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected UPN validation error: %v", err)
			}
		})
	}
}

func TestValidateHimmelblauUPNDomain(t *testing.T) {
	tests := []struct {
		name    string
		upn     string
		domain  string
		wantErr bool
	}{
		{name: "matching primary domain", upn: "alice@example.com", domain: "example.com"},
		{name: "different tenant suffix", upn: "alice@other.example", domain: "example.com", wantErr: true},
		{name: "missing configured domain", upn: "alice@example.com", domain: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateHimmelblauUPNDomain(tt.upn, tt.domain)
			if tt.wantErr && err == nil {
				t.Fatal("expected domain mismatch error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected domain mismatch error: %v", err)
			}
		})
	}
}
