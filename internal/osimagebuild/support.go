package osimagebuild

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/sugaf1204/gomi/internal/oscatalog"
	"github.com/sugaf1204/gomi/internal/osimage"
)

func resolveDefinitionPath(cfg Config, entry BuildEntry, workDir string) (string, error) {
	definition := strings.TrimSpace(entry.Definition)
	if definition == "" {
		return "", fmt.Errorf("%s: definition is required", entry.Name)
	}
	if filepath.IsAbs(definition) {
		if _, err := os.Stat(definition); err != nil {
			return "", fmt.Errorf("stat definition %s: %w", definition, err)
		}
		return definition, nil
	}
	if cfg.useEmbeddedDefinitions {
		raw, err := embeddedDefinitions.ReadFile(filepath.ToSlash(definition))
		if err != nil {
			return "", fmt.Errorf("read embedded definition %s: %w", definition, err)
		}
		target := filepath.Join(workDir, "embedded-definitions", filepath.FromSlash(definition))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(target, raw, 0o644); err != nil {
			return "", err
		}
		return target, nil
	}
	baseDir := cfg.baseDir
	if baseDir == "" {
		baseDir = "."
	}
	target := filepath.Clean(filepath.Join(baseDir, filepath.FromSlash(definition)))
	if _, err := os.Stat(target); err != nil {
		return "", fmt.Errorf("stat definition %s: %w", target, err)
	}
	return target, nil
}

func mustURLPath(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return parsed.Path
}

func writeMetadata(entry oscatalog.Entry, _ BuildEntry, artifact, outDir string) (ImageMetadata, error) {
	sum, err := sha256File(artifact)
	if err != nil {
		return ImageMetadata{}, err
	}
	info, err := os.Stat(artifact)
	if err != nil {
		return ImageMetadata{}, err
	}
	meta := ImageMetadata{
		Name:      entry.Name,
		OSFamily:  entry.OSFamily,
		OSVersion: entry.OSVersion,
		Arch:      entry.Arch,
		Variant:   string(entry.Variant),
		Format:    string(osimage.FormatSquashFS),
		Artifact:  filepath.Base(artifact),
		RootPath:  defaultRootPath,
		SHA256:    sum,
		SizeBytes: info.Size(),
	}
	raw, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return ImageMetadata{}, err
	}
	if err := os.WriteFile(filepath.Join(outDir, entry.Name+".json"), append(raw, '\n'), 0o644); err != nil {
		return ImageMetadata{}, err
	}
	return meta, nil
}
