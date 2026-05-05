package resource

import "testing"

func TestGenerateProvisioningToken(t *testing.T) {
	token, err := GenerateProvisioningToken()
	if err != nil {
		t.Fatalf("GenerateProvisioningToken: %v", err)
	}
	if len(token) != 64 {
		t.Fatalf("expected 64-char hex token, got %d chars: %s", len(token), token)
	}
	// Uniqueness check.
	token2, _ := GenerateProvisioningToken()
	if token == token2 {
		t.Fatal("expected unique tokens")
	}
}

func TestGenerateProvisioningAttemptID(t *testing.T) {
	attemptID, err := GenerateProvisioningAttemptID()
	if err != nil {
		t.Fatalf("GenerateProvisioningAttemptID: %v", err)
	}
	if len(attemptID) != len("attempt-")+32 {
		t.Fatalf("expected attempt id prefix plus 32 hex chars, got %q", attemptID)
	}
	if attemptID[:len("attempt-")] != "attempt-" {
		t.Fatalf("expected attempt- prefix, got %q", attemptID)
	}
	attemptID2, _ := GenerateProvisioningAttemptID()
	if attemptID == attemptID2 {
		t.Fatal("expected unique attempt ids")
	}
}

func TestNormalizeCloudInitRefs(t *testing.T) {
	tests := []struct {
		name      string
		legacy    string
		refs      []string
		wantLen   int
		wantFirst string
	}{
		{"empty", "", nil, 0, ""},
		{"legacy only", "ci-01", nil, 1, "ci-01"},
		{"refs only", "", []string{"ci-01", "ci-02"}, 2, "ci-01"},
		{"legacy+refs dedup", "ci-01", []string{"ci-01", "ci-02"}, 2, "ci-01"},
		{"trims whitespace", "  ci-01  ", []string{"", "  ci-02  "}, 2, "ci-01"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeCloudInitRefs(tt.legacy, tt.refs)
			if len(got) != tt.wantLen {
				t.Fatalf("expected %d refs, got %d: %v", tt.wantLen, len(got), got)
			}
			if tt.wantFirst != "" && (len(got) == 0 || got[0] != tt.wantFirst) {
				first := ""
				if len(got) > 0 {
					first = got[0]
				}
				t.Fatalf("expected first ref %q, got %q", tt.wantFirst, first)
			}
		})
	}
}

func TestResolveCloudInitRef(t *testing.T) {
	tests := []struct {
		name         string
		lastDeployed string
		legacy       string
		refs         []string
		want         string
	}{
		{"lastDeployed wins", "ci-deployed", "ci-legacy", []string{"ci-01"}, "ci-deployed"},
		{"refs fallback", "", "ci-legacy", []string{"ci-01"}, "ci-01"},
		{"legacy fallback", "", "ci-legacy", nil, "ci-legacy"},
		{"all empty", "", "", nil, ""},
		{"trims whitespace", "  ci-deployed  ", "", nil, "ci-deployed"},
		{"skips empty refs", "", "", []string{"", "  ci-02  "}, "ci-02"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveCloudInitRef(tt.lastDeployed, tt.legacy, tt.refs)
			if got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}
