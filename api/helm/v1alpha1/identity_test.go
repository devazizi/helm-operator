package v1alpha1

import "testing"

func TestCreatorIdentityRoundTrip(t *testing.T) {
	want := CreatorIdentity{Username: "john.anderson", Groups: []string{"developers", "system:authenticated"}}
	annotations, err := SetCreatorIdentity(map[string]string{"example": "value"}, want)
	if err != nil {
		t.Fatalf("SetCreatorIdentity returned an error: %v", err)
	}
	got, err := IdentityFromAnnotations(annotations)
	if err != nil {
		t.Fatalf("IdentityFromAnnotations returned an error: %v", err)
	}
	if got.Username != want.Username || len(got.Groups) != len(want.Groups) || got.Groups[0] != want.Groups[0] || got.Groups[1] != want.Groups[1] {
		t.Fatalf("identity = %#v, want %#v", got, want)
	}
}

func TestCreatorIdentityRequiresTrustedAnnotations(t *testing.T) {
	if _, err := IdentityFromAnnotations(nil); err == nil {
		t.Fatal("IdentityFromAnnotations should reject missing annotations")
	}
}
