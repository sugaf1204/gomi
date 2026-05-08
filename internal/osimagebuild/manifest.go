package osimagebuild

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func WriteManifest(dir string) error {
	if strings.TrimSpace(dir) == "" {
		dir = filepath.Join("dist", "os-images")
	}
	metadataPaths, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return err
	}
	entries := make([]ImageMetadata, 0, len(metadataPaths))
	for _, path := range metadataPaths {
		if filepath.Base(path) == "manifest-os-images.json" {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var meta ImageMetadata
		if err := json.Unmarshal(raw, &meta); err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		entries = append(entries, meta)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	manifest, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest-os-images.json"), append(manifest, '\n'), 0o644); err != nil {
		return err
	}

	var checksums strings.Builder
	seenArtifacts := map[string]struct{}{}
	for _, entry := range entries {
		artifact := strings.TrimSpace(entry.Artifact)
		if artifact == "" {
			return fmt.Errorf("%s: artifact is required in metadata", entry.Name)
		}
		if filepath.Base(artifact) != artifact {
			return fmt.Errorf("%s: artifact must be a file name, got %q", entry.Name, artifact)
		}
		if _, ok := seenArtifacts[artifact]; ok {
			continue
		}
		seenArtifacts[artifact] = struct{}{}
		path := filepath.Join(dir, artifact)
		sum, err := sha256File(path)
		if err != nil {
			return err
		}
		checksums.WriteString(sum)
		checksums.WriteString("  ")
		checksums.WriteString(artifact)
		checksums.WriteByte('\n')
	}
	return os.WriteFile(filepath.Join(dir, "checksums-os-images.txt"), []byte(checksums.String()), 0o644)
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
