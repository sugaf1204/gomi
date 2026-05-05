package resource

import (
	"fmt"
	"strings"
)

func ValidateCloudInitRefs(legacyRef string, refs []string) error {
	seen := map[string]struct{}{}
	if ref := strings.TrimSpace(legacyRef); ref != "" {
		seen[ref] = struct{}{}
	}
	for _, raw := range refs {
		ref := strings.TrimSpace(raw)
		if ref == "" {
			continue
		}
		if _, exists := seen[ref]; exists {
			return fmt.Errorf("spec.cloudInitRef/spec.cloudInitRefs must not contain duplicates: %s", ref)
		}
		seen[ref] = struct{}{}
	}
	return nil
}
