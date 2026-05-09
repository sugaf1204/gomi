package osimagebuild

import (
	"context"
	"encoding/json"
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
	cfg := Config{Entries: []BuildEntry{{
		Name:       "buildable",
		Backend:    "distrobuilder",
		Definition: "definitions/buildable.yaml",
	}}}
	matrix, err := BuildMatrix(entries, cfg)
	if err != nil {
		t.Fatalf("build matrix: %v", err)
	}
	if !reflect.DeepEqual(matrix.Include, []MatrixEntry{{Name: "buildable"}}) {
		t.Fatalf("matrix = %#v", matrix)
	}
}

func TestValidateConfigRequiresDefinition(t *testing.T) {
	err := validateConfig(Config{Entries: []BuildEntry{{
		Name:    "missing-definition",
		Backend: "distrobuilder",
	}}})
	if err == nil || !strings.Contains(err.Error(), "definition is required") {
		t.Fatalf("validateConfig error = %v", err)
	}
}

func TestValidateConfigRejectsUnsupportedBackend(t *testing.T) {
	err := validateConfig(Config{Entries: []BuildEntry{{
		Name:       "bad-backend",
		Backend:    "shell",
		Definition: "definitions/buildable.yaml",
	}}})
	if err == nil || !strings.Contains(err.Error(), "unsupported backend") {
		t.Fatalf("validateConfig error = %v", err)
	}
}

func TestBuildRunsDistrobuilderAndMKSquashFS(t *testing.T) {
	tempDir := t.TempDir()
	cfgPath := filepath.Join(tempDir, "builds.yaml")
	definitionsDir := filepath.Join(tempDir, "definitions")
	if err := os.MkdirAll(definitionsDir, 0o755); err != nil {
		t.Fatalf("mkdir definitions: %v", err)
	}
	if err := os.WriteFile(filepath.Join(definitionsDir, "buildable.yaml"), []byte("image:\n  distribution: ubuntu\n"), 0o644); err != nil {
		t.Fatalf("write definition: %v", err)
	}
	if err := os.WriteFile(cfgPath, []byte(strings.TrimSpace(`
entries:
  - name: buildable
    backend: distrobuilder
    definition: definitions/buildable.yaml
    squashfs:
      compression: zstd
      blockSize: 512K
`)+"\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	workDir := filepath.Join(tempDir, "work")
	outDir := filepath.Join(tempDir, "out")
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
	}}, cfg, BuildOptions{
		EntryName:     "buildable",
		OutDir:        outDir,
		WorkDir:       workDir,
		Processors:    2,
		CommandRunner: runner,
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	gotCommands := runner.commandLabels()
	wantCommands := []string{
		"distrobuilder build-dir",
		"mksquashfs",
	}
	if !reflect.DeepEqual(gotCommands, wantCommands) {
		t.Fatalf("commands = %#v, want %#v", gotCommands, wantCommands)
	}

	distrobuilder := runner.firstCall("distrobuilder")
	if !containsArgSequence(distrobuilder.args, []string{"build-dir", "--with-post-files"}) {
		t.Fatalf("distrobuilder args = %#v", distrobuilder.args)
	}
	if distrobuilder.args[len(distrobuilder.args)-2] != filepath.Join(tempDir, "definitions", "buildable.yaml") {
		t.Fatalf("definition path = %q", distrobuilder.args[len(distrobuilder.args)-2])
	}
	if distrobuilder.args[len(distrobuilder.args)-1] != filepath.Join(workDir, "rootfs") {
		t.Fatalf("rootfs path = %q", distrobuilder.args[len(distrobuilder.args)-1])
	}

	mksquashfs := runner.firstCall("mksquashfs")
	if !reflect.DeepEqual(mksquashfs.args, []string{
		filepath.Join(workDir, "rootfs"),
		filepath.Join(outDir, "buildable.rootfs.squashfs"),
		"-noappend", "-comp", "zstd", "-b", "512K", "-processors", "2", "-all-root",
	}) {
		t.Fatalf("mksquashfs args = %#v", mksquashfs.args)
	}
	if meta.Artifact != "buildable.rootfs.squashfs" || meta.RootPath != "rootfs.squashfs" || meta.SizeBytes == 0 {
		t.Fatalf("metadata = %#v", meta)
	}
	if len(meta.Packages) != 0 {
		t.Fatalf("metadata packages = %#v", meta.Packages)
	}
	if _, err := os.Stat(filepath.Join(outDir, "manifest-os-images.json")); err != nil {
		t.Fatalf("manifest was not written: %v", err)
	}
}

func TestBuildRejectsMissingDefinitionFile(t *testing.T) {
	cfg := Config{
		Entries: []BuildEntry{{
			Name:       "buildable",
			Definition: "definitions/missing.yaml",
		}},
		baseDir: t.TempDir(),
	}
	_, err := Build(context.Background(), sampleCatalogEntries(), cfg, BuildOptions{
		EntryName:     "buildable",
		OutDir:        filepath.Join(t.TempDir(), "out"),
		WorkDir:       filepath.Join(t.TempDir(), "work"),
		CommandRunner: &fakeCommandRunner{},
	})
	if err == nil || !strings.Contains(err.Error(), "stat definition") {
		t.Fatalf("build error = %v", err)
	}
}

func TestLoadConfigResolvesDefaultDefinition(t *testing.T) {
	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatalf("load default config: %v", err)
	}
	if len(cfg.Entries) == 0 {
		t.Fatal("expected default config entries")
	}
	workDir := t.TempDir()
	definitionPath, err := resolveDefinitionPath(cfg, cfg.Entries[0], workDir)
	if err != nil {
		t.Fatalf("resolve default definition: %v", err)
	}
	if _, err := os.Stat(definitionPath); err != nil {
		t.Fatalf("definition stat: %v", err)
	}
}

func TestWriteManifestWritesChecksums(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "buildable.rootfs.squashfs")
	if err := os.WriteFile(artifactPath, []byte("rootfs"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	meta := ImageMetadata{
		Name:      "buildable",
		OSFamily:  "ubuntu",
		OSVersion: "22.04",
		Arch:      "amd64",
		Variant:   string(osimage.VariantBareMetal),
		Format:    string(osimage.FormatSquashFS),
		Artifact:  filepath.Base(artifactPath),
		RootPath:  defaultRootPath,
		SHA256:    "ignored",
		SizeBytes: 6,
	}
	raw, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "buildable.json"), append(raw, '\n'), 0o644); err != nil {
		t.Fatalf("write metadata: %v", err)
	}

	if err := WriteManifest(dir); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	manifest, err := os.ReadFile(filepath.Join(dir, "manifest-os-images.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if !strings.Contains(string(manifest), `"name": "buildable"`) {
		t.Fatalf("manifest = %s", manifest)
	}
	checksums, err := os.ReadFile(filepath.Join(dir, "checksums-os-images.txt"))
	if err != nil {
		t.Fatalf("read checksums: %v", err)
	}
	if !strings.Contains(string(checksums), "buildable.rootfs.squashfs") {
		t.Fatalf("checksums = %s", checksums)
	}
}

func TestPackageMetadataIncludesDefinitions(t *testing.T) {
	root, err := findRepoRoot()
	if err != nil {
		t.Fatalf("find repo root: %v", err)
	}
	for _, path := range []string{
		filepath.Join(root, "packages", "debian", "gomi.install"),
		filepath.Join(root, "packages", "nfpm.yaml"),
	} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if !strings.Contains(string(raw), "definitions") {
			t.Fatalf("%s does not mention definitions", path)
		}
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
	case "distrobuilder":
		rootfsDir := args[len(args)-1]
		if err := os.MkdirAll(filepath.Join(rootfsDir, "etc"), 0o755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(rootfsDir, "etc", "os-release"), []byte("NAME=Ubuntu\n"), 0o644)
	case "mksquashfs":
		return os.WriteFile(args[1], []byte("squashfs artifact"), 0o644)
	default:
		return nil
	}
}

func (r *fakeCommandRunner) commandLabels() []string {
	labels := make([]string, 0, len(r.calls))
	for _, call := range r.calls {
		switch call.name {
		case "distrobuilder":
			labels = append(labels, "distrobuilder build-dir")
		default:
			labels = append(labels, call.name)
		}
	}
	return labels
}

func (r *fakeCommandRunner) firstCall(name string) commandCall {
	for _, call := range r.calls {
		if call.name == name {
			return call
		}
	}
	return commandCall{}
}

func containsArgSequence(args, want []string) bool {
	for i := 0; i+len(want) <= len(args); i++ {
		if reflect.DeepEqual(args[i:i+len(want)], want) {
			return true
		}
	}
	return false
}

func sampleCatalogEntries() []oscatalog.Entry {
	return []oscatalog.Entry{{
		Name:            "buildable",
		OSFamily:        "ubuntu",
		OSVersion:       "22.04",
		Arch:            "amd64",
		Variant:         osimage.VariantBareMetal,
		Format:          osimage.FormatSquashFS,
		SourceFormat:    osimage.FormatSquashFS,
		URL:             "https://images.example.test/buildable.rootfs.squashfs",
		BootEnvironment: "ubuntu-minimal-cloud-amd64",
	}}
}
