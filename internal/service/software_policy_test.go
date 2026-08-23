package service

import (
	"testing"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// ValidateRepositorySignature lives in the service layer rather than in
// the cli so both front-ends get the same rules — an MCP caller must
// not be able to submit a combination the cli would have rejected.
func TestValidateRepositorySignature(t *testing.T) {
	valid := []models.RepositorySignature{
		{Type: "fingerprints", Fingerprints: []models.RepositoryFingerprint{{Function: "sha256", Fingerprint: "ab"}}},
		{Type: "pubkey", Pubkey: "-----BEGIN PUBLIC KEY-----"},
		{Type: "none"},
	}
	for _, sig := range valid {
		if err := ValidateRepositorySignature(sig); err != nil {
			t.Errorf("ValidateRepositorySignature(%+v) = %v, want nil", sig, err)
		}
	}

	invalid := []struct {
		name string
		sig  models.RepositorySignature
	}{
		{"fingerprints with none supplied", models.RepositorySignature{Type: "fingerprints"}},
		{"pubkey with none supplied", models.RepositorySignature{Type: "pubkey"}},
		{"empty fingerprint value", models.RepositorySignature{
			Type:         "fingerprints",
			Fingerprints: []models.RepositoryFingerprint{{Function: "sha256"}},
		}},
		{"contradictory material", models.RepositorySignature{
			Type:         "fingerprints",
			Fingerprints: []models.RepositoryFingerprint{{Function: "sha256", Fingerprint: "ab"}},
			Pubkey:       "-----BEGIN PUBLIC KEY-----",
		}},
		{"none carrying material", models.RepositorySignature{Type: "none", Pubkey: "x"}},
		{"unknown type", models.RepositorySignature{Type: "magic"}},
	}
	for _, tc := range invalid {
		if err := ValidateRepositorySignature(tc.sig); err == nil {
			t.Errorf("%s: expected an error, got nil", tc.name)
		}
	}
}

// The service must refuse an obviously-empty patch field before the
// round-trip rather than storing a blank URL.
func TestSetRepository_RejectsEmptyFields(t *testing.T) {
	svc := &Service{}
	empty := ""

	if _, _, err := svc.SoftwarePolicySetRepository(t.Context(), "org", "policy", models.RepositoryPatch{}); err == nil {
		t.Error("a patch with no repository name should be refused")
	}
	if _, _, err := svc.SoftwarePolicySetRepository(t.Context(), "org", "policy",
		models.RepositoryPatch{Name: "r", URL: &empty}); err == nil {
		t.Error("an explicitly empty URL should be refused")
	}
	if _, _, err := svc.SoftwarePolicySetExternal(t.Context(), "org", "policy",
		models.ExternalPatch{Name: "e", Version: &empty}); err == nil {
		t.Error("an explicitly empty version should be refused")
	}
	if _, _, err := svc.SoftwarePolicySetExternal(t.Context(), "org", "policy", models.ExternalPatch{}); err == nil {
		t.Error("a patch with no package name should be refused")
	}
}
