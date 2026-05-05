package bootenv

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestManagerInstallsPrebuiltBootEnvironment(t *testing.T) {
	sourceDir := t.TempDir()
	artifacts := map[string][]byte{
		"boot-kernel":     []byte("kernel"),
		"boot-initrd":     []byte("initrd"),
		"rootfs.squashfs": []byte("rootfs"),
	}
	manifestArtifacts := map[string]prebuiltArtifact{}
	for key, name := range map[string]string{
		"kernel": "boot-kernel",
		"initrd": "boot-initrd",
		"rootfs": "rootfs.squashfs",
	} {
		body := artifacts[name]
		sum := sha256.Sum256(body)
		if err := os.WriteFile(filepath.Join(sourceDir, name), body, 0o644); err != nil {
			t.Fatal(err)
		}
		manifestArtifacts[key] = prebuiltArtifact{
			Path:   name,
			SHA256: fmt.Sprintf("%x", sum),
			Size:   int64(len(body)),
		}
	}
	manifest := prebuiltManifest{
		SchemaVersion: "gomi.bootenv/v1",
		Name:          "ubuntu-minimal-cloud-amd64",
		Version:       "test",
		Arch:          "amd64",
		Artifacts:     manifestArtifacts,
	}
	rawManifest, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "manifest.json"), rawManifest, 0o644); err != nil {
		t.Fatal(err)
	}

	dataDir := t.TempDir()
	filesDir := filepath.Join(t.TempDir(), "files")
	mgr := NewManager(Config{
		DataDir:   dataDir,
		FilesDir:  filesDir,
		SourceURL: sourceDir,
	})

	st, err := mgr.Ensure(context.Background(), "ubuntu-minimal-cloud-amd64")
	if err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	if st.Phase != PhaseReady {
		t.Fatalf("Ensure() phase = %s, want %s", st.Phase, PhaseReady)
	}
	for name, want := range artifacts {
		got, err := os.ReadFile(filepath.Join(st.ArtifactDir, name))
		if err != nil {
			t.Fatalf("ReadFile(%s) error = %v", name, err)
		}
		if string(got) != string(want) {
			t.Fatalf("%s = %q, want %q", name, got, want)
		}
		if _, err := os.Lstat(filepath.Join(filesDir, "linux", name)); err != nil {
			t.Fatalf("expected PXE compatibility link %s: %v", name, err)
		}
	}
}
