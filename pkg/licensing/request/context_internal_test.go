package request

import (
	"context"
	"testing"
)

func TestFromContext(t *testing.T) {
	t.Parallel()

	if _, ok := FromContext(context.Background()); ok {
		t.Fatal("expected no installation in an empty context")
	}

	inst := Installation{Fingerprint: "fp", MachineID: "m1"}

	if _, ok := FromContext(withInstallation(context.Background(), inst)); !ok {
		t.Fatal("expected the installation in the context")
	}
}

func TestWithInstallation_RoundTrip(t *testing.T) {
	t.Parallel()

	inst := Installation{
		Fingerprint: "fp",
		MachineID:   "m1",
		LicenseID:   "l1",
		LicenseCode: "VALID",
	}

	got, ok := FromContext(withInstallation(context.Background(), inst))
	if !ok {
		t.Fatal("expected the installation in the context")
	}

	if got.Fingerprint != inst.Fingerprint || got.MachineID != inst.MachineID ||
		got.LicenseID != inst.LicenseID || got.LicenseCode != inst.LicenseCode {
		t.Errorf("round trip = %+v, want %+v", got, inst)
	}
}

func TestWithInstallation_DoesNotOverwriteParent(t *testing.T) {
	t.Parallel()

	first := Installation{Fingerprint: "first"}
	second := Installation{Fingerprint: "second"}

	ctx := withInstallation(withInstallation(context.Background(), first), second)

	got, _ := FromContext(ctx)
	if got.Fingerprint != "second" {
		t.Errorf("fingerprint = %q, want the innermost value", got.Fingerprint)
	}
}
