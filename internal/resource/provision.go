package resource

import (
	cryptorand "crypto/rand"
	"encoding/hex"
	"strings"
)

type InstallType string

const (
	InstallPreseed InstallType = "preseed"
	InstallCurtin  InstallType = "curtin"
)

// GenerateProvisioningToken creates a cryptographically random 64-character
// hex token for provisioning completion verification.
func GenerateProvisioningToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := cryptorand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// GenerateProvisioningAttemptID creates a stable identifier for one install
// attempt. It is stored when provisioning starts and must be echoed by PXE
// callbacks to reject stale initrd requests.
func GenerateProvisioningAttemptID() (string, error) {
	buf := make([]byte, 16)
	if _, err := cryptorand.Read(buf); err != nil {
		return "", err
	}
	return "attempt-" + hex.EncodeToString(buf), nil
}

// NormalizeCloudInitRefs deduplicates and trims cloud-init references,
// merging legacyRef into refs while preserving order.
func NormalizeCloudInitRefs(legacyRef string, refs []string) []string {
	out := make([]string, 0, len(refs)+1)
	seen := map[string]struct{}{}
	appendRef := func(raw string) {
		ref := strings.TrimSpace(raw)
		if ref == "" {
			return
		}
		if _, exists := seen[ref]; exists {
			return
		}
		seen[ref] = struct{}{}
		out = append(out, ref)
	}
	appendRef(legacyRef)
	for _, raw := range refs {
		appendRef(raw)
	}
	return out
}

// ResolveCloudInitRef returns the first non-empty cloud-init reference,
// checking lastDeployed first, then refs, then legacyRef.
func ResolveCloudInitRef(lastDeployed, legacyRef string, refs []string) string {
	if ref := strings.TrimSpace(lastDeployed); ref != "" {
		return ref
	}
	for _, raw := range refs {
		if ref := strings.TrimSpace(raw); ref != "" {
			return ref
		}
	}
	return strings.TrimSpace(legacyRef)
}
