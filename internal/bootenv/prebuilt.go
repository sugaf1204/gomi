package bootenv

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type prebuiltManifest struct {
	Name         string                      `json:"name"`
	Version      string                      `json:"version,omitempty"`
	Distribution string                      `json:"distribution,omitempty"`
	Release      string                      `json:"release,omitempty"`
	Arch         string                      `json:"arch,omitempty"`
	Source       map[string]any              `json:"source,omitempty"`
	Artifacts    map[string]prebuiltArtifact `json:"artifacts"`
	Build        map[string]any              `json:"build,omitempty"`
}

type prebuiltArtifact struct {
	Path   string `json:"path"`
	URL    string `json:"url,omitempty"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size,omitempty"`
}

func (m *Manager) fetchPrebuilt(ctx context.Context, def Definition, buildID, buildDir, logPath string, logFile io.Writer, st Status) (Status, error) {
	manifestLocation, baseLocation := resolvePrebuiltManifestLocation(m.sourceURL)
	st.Message = "downloading boot environment manifest"
	st.UpdatedAt = m.now()
	m.setStatus(st)
	fmt.Fprintf(logFile, time.Now().UTC().Format(time.RFC3339)+" downloading boot environment manifest from %s\n", manifestLocation)

	rawManifest, err := m.readLocation(ctx, manifestLocation)
	if err != nil {
		return st, err
	}
	var manifest prebuiltManifest
	if err := json.Unmarshal(rawManifest, &manifest); err != nil {
		return st, fmt.Errorf("parse boot environment manifest: %w", err)
	}
	if err := validatePrebuiltManifest(def, manifest); err != nil {
		return st, err
	}

	artifactDir := filepath.Join(m.dataDir, "artifacts", def.Name, buildID)
	artifactPublished := false
	defer func() {
		if !artifactPublished {
			_ = os.RemoveAll(artifactDir)
		}
	}()
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		return st, err
	}

	artifactMap := map[string]string{
		"kernel": "boot-kernel",
		"initrd": "boot-initrd",
		"rootfs": "rootfs.squashfs",
	}
	for key, dstName := range artifactMap {
		artifact := manifest.Artifacts[key]
		st.Message = "downloading boot environment artifact: " + key
		st.UpdatedAt = m.now()
		m.setStatus(st)
		src := resolvePrebuiltArtifactLocation(baseLocation, artifact)
		dst := filepath.Join(artifactDir, dstName)
		fmt.Fprintf(logFile, time.Now().UTC().Format(time.RFC3339)+" downloading %s from %s\n", key, src)
		if err := m.copyLocation(ctx, src, dst, 0o644); err != nil {
			return st, err
		}
		if err := verifySHA256(dst, artifact.SHA256); err != nil {
			return st, err
		}
		if artifact.Size > 0 {
			info, err := os.Stat(dst)
			if err != nil {
				return st, err
			}
			if info.Size() != artifact.Size {
				return st, fmt.Errorf("size mismatch for %s: expected %d got %d", dstName, artifact.Size, info.Size())
			}
		}
	}
	if err := os.WriteFile(filepath.Join(artifactDir, "manifest.json"), append(rawManifest, '\n'), 0o644); err != nil {
		return st, err
	}
	if err := m.publishPXECompatibilityFiles(artifactDir); err != nil {
		return st, err
	}
	if err := m.publishCurrent(def.Name, buildID, artifactDir, logPath); err != nil {
		return st, err
	}
	artifactPublished = true
	_ = os.WriteFile(filepath.Join(buildDir, ".prebuilt"), []byte(manifestLocation+"\n"), 0o644)
	return Status{
		Name:        def.Name,
		Phase:       PhaseReady,
		ArtifactDir: artifactDir,
		LogPath:     logPath,
		KernelPath:  filepath.Join(artifactDir, "boot-kernel"),
		InitrdPath:  filepath.Join(artifactDir, "boot-initrd"),
		RootFSPath:  filepath.Join(artifactDir, "rootfs.squashfs"),
		UpdatedAt:   m.now(),
	}, nil
}

func validatePrebuiltManifest(def Definition, manifest prebuiltManifest) error {
	if strings.TrimSpace(manifest.Name) != def.Name {
		return fmt.Errorf("boot environment manifest name mismatch: expected %s got %s", def.Name, manifest.Name)
	}
	for _, key := range []string{"kernel", "initrd", "rootfs"} {
		artifact, ok := manifest.Artifacts[key]
		if !ok {
			return fmt.Errorf("boot environment manifest missing artifact %q", key)
		}
		if strings.TrimSpace(artifact.Path) == "" && strings.TrimSpace(artifact.URL) == "" {
			return fmt.Errorf("boot environment artifact %q requires path or url", key)
		}
		if strings.TrimSpace(artifact.SHA256) == "" {
			return fmt.Errorf("boot environment artifact %q requires sha256", key)
		}
	}
	return nil
}

func resolvePrebuiltManifestLocation(raw string) (manifestLocation, baseLocation string) {
	raw = strings.TrimSpace(raw)
	if isHTTPLocation(raw) {
		trimmed := strings.TrimRight(raw, "/")
		if strings.HasSuffix(trimmed, ".json") {
			return trimmed, trimmed[:strings.LastIndex(trimmed, "/")]
		}
		return trimmed + "/manifest.json", trimmed
	}
	path := localPathFromLocation(raw)
	if strings.HasSuffix(path, ".json") {
		return path, filepath.Dir(path)
	}
	return filepath.Join(path, "manifest.json"), path
}

func resolvePrebuiltArtifactLocation(base string, artifact prebuiltArtifact) string {
	if strings.TrimSpace(artifact.URL) != "" {
		return strings.TrimSpace(artifact.URL)
	}
	path := strings.TrimSpace(artifact.Path)
	if isHTTPLocation(base) {
		return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(path, "/")
	}
	return filepath.Join(base, path)
}
