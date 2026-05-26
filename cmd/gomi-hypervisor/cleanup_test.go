package main

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestIsManagedFile(t *testing.T) {
	tests := []struct {
		filename string
		want     bool
	}{
		{"ubuntu.qcow2", true},
		{"debian.vmdk", false},
		{"server.iso", true},
		{"disk.img", true},
		{"UPPER.QCOW2", true},
		{"readme.txt", false},
		{"script.sh", false},
		{"config.yaml", false},
		{"noext", false},
	}
	for _, tt := range tests {
		if got := isManagedFile(tt.filename); got != tt.want {
			t.Errorf("isManagedFile(%q) = %v, want %v", tt.filename, got, tt.want)
		}
	}
}

func TestCleanupStaleFiles(t *testing.T) {
	dir := t.TempDir()

	files := map[string]string{
		"keep.qcow2":    "keep",
		"stale.qcow2":   "remove",
		"old.iso":       "remove",
		"old.squashfs":  "remove",
		"manual.txt":    "keep (not managed)",
		"another.vmdk":  "keep (not managed)",
		"keep-too.img":  "keep",
		"vm-disk.qcow2": "keep (unmarked VM disk)",
	}
	for name, content := range files {
		os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644)
	}
	for _, name := range []string{"stale.qcow2", "old.iso", "old.squashfs"} {
		os.WriteFile(filepath.Join(dir, name+managedMarkerSuffix), []byte("gomi-hypervisor\n"), 0o644)
	}

	expected := map[string]OSImage{
		"keep.qcow2":   {Name: "keep", Format: "qcow2"},
		"keep-too.img": {Name: "keep-too", Format: "img"},
	}

	removed, err := cleanupStaleFiles(dir, expected)
	if err != nil {
		t.Fatalf("cleanupStaleFiles: %v", err)
	}

	sort.Strings(removed)
	wantRemoved := []string{"old.iso", "old.squashfs", "stale.qcow2"}
	if len(removed) != len(wantRemoved) {
		t.Fatalf("removed = %v, want %v", removed, wantRemoved)
	}
	for i, name := range wantRemoved {
		if removed[i] != name {
			t.Errorf("removed[%d] = %q, want %q", i, removed[i], name)
		}
	}

	for _, name := range []string{"keep.qcow2", "manual.txt", "another.vmdk", "keep-too.img", "vm-disk.qcow2"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("expected %q to still exist", name)
		}
	}

	for _, name := range wantRemoved {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("expected %q to be removed", name)
		}
	}
}

func TestCleanupStaleFiles_DoesNotRemoveUnmarkedManagedExtensions(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"kube-1.qcow2", "manual.vmdk", "scratch.img"} {
		os.WriteFile(filepath.Join(dir, name), []byte("vm data"), 0o644)
	}

	removed, err := cleanupStaleFiles(dir, nil)
	if err != nil {
		t.Fatalf("cleanupStaleFiles: %v", err)
	}
	if len(removed) != 0 {
		t.Fatalf("removed = %v, want none", removed)
	}

	for _, name := range []string{"kube-1.qcow2", "manual.vmdk", "scratch.img"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("expected %q to still exist", name)
		}
	}
}

func TestCleanupStaleFiles_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	removed, err := cleanupStaleFiles(dir, nil)
	if err != nil {
		t.Fatalf("cleanupStaleFiles: %v", err)
	}
	if len(removed) != 0 {
		t.Fatalf("unexpected removals: %v", removed)
	}
}
