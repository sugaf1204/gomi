package osimagebuild

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/sugaf1204/gomi/internal/oscatalog"
	"github.com/sugaf1204/gomi/internal/osimage"
)

func TestBuildMatrixUsesBuildConfigOnly(t *testing.T) {
	entries := []oscatalog.Entry{
		{
			Name:            "buildable",
			OSFamily:        "custom",
			OSVersion:       "1",
			Arch:            "amd64",
			Variant:         osimage.VariantBareMetal,
			Format:          osimage.FormatSquashFS,
			SourceFormat:    osimage.FormatSquashFS,
			URL:             "https://images.example.test/buildable.rootfs.squashfs",
			BootEnvironment: "ubuntu-minimal-cloud-amd64",
		},
		{
			Name:            "url-only",
			OSFamily:        "custom",
			OSVersion:       "1",
			Arch:            "amd64",
			Variant:         osimage.VariantBareMetal,
			Format:          osimage.FormatSquashFS,
			SourceFormat:    osimage.FormatSquashFS,
			URL:             "https://images.example.test/url-only.rootfs.squashfs",
			BootEnvironment: "ubuntu-minimal-cloud-amd64",
		},
	}
	cfg := Config{Entries: []BuildEntry{{Name: "buildable", Source: Source{URL: "https://source.example.test/root.tar.xz", Format: "root-tar"}}}}
	matrix, err := BuildMatrix(entries, cfg)
	if err != nil {
		t.Fatalf("build matrix: %v", err)
	}
	if !reflect.DeepEqual(matrix.Include, []MatrixEntry{{Name: "buildable"}}) {
		t.Fatalf("matrix = %#v", matrix)
	}
}

func TestBuildRunsRootFSSquashFSSteps(t *testing.T) {
	sourceBytes := []byte("tarball")
	sourceSum := sha256.Sum256(sourceBytes)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/root.tar.xz" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(sourceBytes)
	}))
	defer server.Close()

	workDir := filepath.Join(t.TempDir(), "work")
	outDir := filepath.Join(t.TempDir(), "out")
	runner := &fakeCommandRunner{}
	meta, err := Build(context.Background(), []oscatalog.Entry{{
		Name:            "buildable",
		OSFamily:        "ubuntu",
		OSVersion:       "22.04",
		Arch:            "amd64",
		Variant:         osimage.VariantBareMetal,
		Format:          osimage.FormatSquashFS,
		SourceFormat:    osimage.FormatSquashFS,
		URL:             "https://images.example.test/buildable.rootfs.squashfs",
		BootEnvironment: "ubuntu-minimal-cloud-amd64",
	}}, Config{Entries: []BuildEntry{{
		Name: "buildable",
		Source: Source{
			URL:         server.URL + "/root.tar.xz",
			Checksum:    "sha256:" + hex.EncodeToString(sourceSum[:]),
			Format:      "root-tar",
			Compression: "xz",
		},
		PackageManager: "apt",
		Packages:       []string{"linux-image-generic", "grub-pc"},
		VerifyModules:  []string{"igc", "r8169"},
		SquashFS:       SquashFS{Compression: "xz", BlockSize: "1M"},
	}}}, BuildOptions{
		EntryName:     "buildable",
		OutDir:        outDir,
		WorkDir:       workDir,
		Processors:    1,
		CommandRunner: runner,
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if meta.Artifact != "buildable.rootfs.squashfs" || meta.RootPath != "rootfs.squashfs" || meta.SizeBytes == 0 {
		t.Fatalf("metadata = %#v", meta)
	}

	gotCommands := runner.commandLabels()
	wantCommands := []string{
		"tar -xJf",
		"mount --bind",
		"mount --bind",
		"mount --bind",
		"mount --bind",
		"mount -t",
		"chroot apt-get update",
		"chroot apt-get install",
		"umount",
		"umount",
		"umount",
		"umount",
		"umount",
		"mksquashfs",
	}
	if !reflect.DeepEqual(gotCommands, wantCommands) {
		t.Fatalf("commands = %#v, want %#v", gotCommands, wantCommands)
	}

	mksquashfs := runner.lastCall("mksquashfs")
	if !reflect.DeepEqual(mksquashfs.args, []string{
		filepath.Join(workDir, "rootfs"),
		filepath.Join(outDir, "buildable.rootfs.squashfs"),
		"-noappend", "-comp", "xz", "-b", "1M", "-processors", "1", "-all-root",
	}) {
		t.Fatalf("mksquashfs args = %#v", mksquashfs.args)
	}
	if _, err := os.Stat(filepath.Join(outDir, "manifest-os-images.json")); err != nil {
		t.Fatalf("manifest was not written: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(workDir, "rootfs", "etc", "machine-id")); err != nil || len(got) != 0 {
		t.Fatalf("machine-id cleanup failed: len=%d err=%v", len(got), err)
	}
	if _, err := os.Stat(filepath.Join(workDir, "rootfs", "etc", "ssh", "ssh_host_ed25519_key")); !os.IsNotExist(err) {
		t.Fatalf("ssh host key cleanup failed: %v", err)
	}
}

func TestCleanupRootFSRejectsEscapingCleanupPaths(t *testing.T) {
	rootfsDir := filepath.Join(t.TempDir(), "rootfs")
	outsideDir := filepath.Join(t.TempDir(), "outside")
	if err := os.MkdirAll(rootfsDir, 0o755); err != nil {
		t.Fatalf("create rootfs: %v", err)
	}
	if err := os.MkdirAll(outsideDir, 0o755); err != nil {
		t.Fatalf("create outside dir: %v", err)
	}
	outsideFile := filepath.Join(outsideDir, "keep")
	if err := os.WriteFile(outsideFile, []byte("keep"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	err := cleanupRootFS(BuildEntry{CleanupPaths: []string{"../" + filepath.Base(outsideDir)}}, rootfsDir)
	if err == nil {
		t.Fatal("expected escaping cleanup path to be rejected")
	}
	if _, statErr := os.Stat(outsideFile); statErr != nil {
		t.Fatalf("outside file should remain: %v", statErr)
	}
}

func TestCleanupRootFSRejectsEscapingCleanupGlobs(t *testing.T) {
	rootfsDir := filepath.Join(t.TempDir(), "rootfs")
	if err := os.MkdirAll(rootfsDir, 0o755); err != nil {
		t.Fatalf("create rootfs: %v", err)
	}

	err := cleanupRootFS(BuildEntry{CleanupGlobs: []string{"../outside/*"}}, rootfsDir)
	if err == nil {
		t.Fatal("expected escaping cleanup glob to be rejected")
	}
}

func TestWriteManifestIncludesSquashFSArtifacts(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sample.rootfs.squashfs"), []byte("squashfs-bytes"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sample.json"), []byte(`{
  "name": "sample",
  "osFamily": "custom",
  "osVersion": "1",
  "arch": "amd64",
  "variant": "baremetal",
  "format": "squashfs",
  "artifact": "sample.rootfs.squashfs",
  "rootPath": "rootfs.squashfs",
  "sha256": "old",
  "sizeBytes": 14
}`), 0o644); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
	if err := WriteManifest(dir); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	manifest, err := os.ReadFile(filepath.Join(dir, "manifest-os-images.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var entries []ImageMetadata
	if err := json.Unmarshal(manifest, &entries); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	if len(entries) != 1 || entries[0].Artifact != "sample.rootfs.squashfs" || entries[0].RootPath != "rootfs.squashfs" {
		t.Fatalf("manifest entries = %#v", entries)
	}
	checksums, err := os.ReadFile(filepath.Join(dir, "checksums-os-images.txt"))
	if err != nil {
		t.Fatalf("read checksums: %v", err)
	}
	if !strings.Contains(string(checksums), "sample.rootfs.squashfs") {
		t.Fatalf("checksums do not include artifact: %s", checksums)
	}
}

type fakeCommandRunner struct {
	calls []commandCall
}

type commandCall struct {
	name string
	args []string
}

func (r *fakeCommandRunner) Run(_ context.Context, name string, args ...string) error {
	r.calls = append(r.calls, commandCall{name: name, args: append([]string{}, args...)})
	switch name {
	case "tar":
		rootfs := argAfter(args, "-C")
		if err := os.MkdirAll(filepath.Join(rootfs, "lib", "modules", "test", "kernel", "drivers", "net"), 0o755); err != nil {
			return err
		}
		for _, module := range []string{"igc", "r8169"} {
			if err := os.WriteFile(filepath.Join(rootfs, "lib", "modules", "test", "kernel", "drivers", "net", module+".ko"), []byte("module"), 0o644); err != nil {
				return err
			}
		}
		if err := os.MkdirAll(filepath.Join(rootfs, "etc", "ssh"), 0o755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(rootfs, "etc", "ssh", "ssh_host_ed25519_key"), []byte("key"), 0o600)
	case "mksquashfs":
		return os.WriteFile(args[1], []byte("squashfs"), 0o644)
	}
	return nil
}

func (r *fakeCommandRunner) commandLabels() []string {
	out := make([]string, 0, len(r.calls))
	for _, call := range r.calls {
		switch call.name {
		case "tar":
			out = append(out, call.name+" "+call.args[0])
		case "mount":
			out = append(out, call.name+" "+call.args[0])
		case "chroot":
			out = append(out, "chroot "+call.args[3]+" "+call.args[4])
		default:
			out = append(out, call.name)
		}
	}
	return out
}

func (r *fakeCommandRunner) lastCall(name string) commandCall {
	for i := len(r.calls) - 1; i >= 0; i-- {
		if r.calls[i].name == name {
			return r.calls[i]
		}
	}
	return commandCall{}
}

func argAfter(args []string, flag string) string {
	for i, arg := range args {
		if arg == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}
