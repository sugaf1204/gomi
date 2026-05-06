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
)

func TestMatrixUsesBuildRecipesOnly(t *testing.T) {
	catalog := writeCatalog(t, `
entries:
  - name: buildable
    osFamily: custom
    osVersion: "1"
    arch: amd64
    variant: baremetal
    format: raw
    sourceFormat: raw
    sourceCompression: zstd
    url: buildable.raw.zst
    bootEnvironment: ubuntu-minimal-cloud-amd64
    build:
      type: packer-qemu-cloud-image
      source:
        url: https://images.example.test/source.qcow2
        checksum: sha256:abc
        format: qcow2
  - name: url-only
    osFamily: custom
    osVersion: "1"
    arch: amd64
    variant: baremetal
    format: raw
    sourceFormat: raw
    sourceCompression: zstd
    url: https://images.example.test/root.raw.zst
    bootEnvironment: ubuntu-minimal-cloud-amd64
`)
	entries, err := LoadCatalog(context.Background(), LoadOptions{
		CatalogFile:     catalog,
		ReplaceExternal: true,
	})
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	matrix := BuildMatrix(entries)
	raw, err := json.Marshal(matrix)
	if err != nil {
		t.Fatalf("marshal matrix: %v", err)
	}
	got := string(raw)
	if !strings.Contains(got, "buildable") {
		t.Fatalf("expected buildable entry in matrix, got %s", got)
	}
	if strings.Contains(got, "url-only") {
		t.Fatalf("expected URL-only entry to be excluded, got %s", got)
	}
}

func TestWriteManifest(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sample.raw.zst"), []byte("zstd-bytes"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sample.json"), []byte(`{
  "name": "sample",
  "osFamily": "custom",
  "osVersion": "1",
  "arch": "amd64",
  "variant": "baremetal",
  "artifact": "sample.raw.zst",
  "sha256": "old",
  "sizeBytes": 10
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
	if !strings.Contains(string(manifest), `"name": "sample"`) {
		t.Fatalf("manifest does not include sample metadata: %s", manifest)
	}
	checksums, err := os.ReadFile(filepath.Join(dir, "checksums-os-images.txt"))
	if err != nil {
		t.Fatalf("read checksums: %v", err)
	}
	if !strings.Contains(string(checksums), "sample.raw.zst") {
		t.Fatalf("checksums do not include artifact: %s", checksums)
	}
}

func TestBuildRunsCommandsAndWritesPackerVarJSON(t *testing.T) {
	workDir := filepath.Join(t.TempDir(), "work")
	outDir := filepath.Join(t.TempDir(), "out")
	templateDir := filepath.Join(t.TempDir(), "template")
	writeTemplate(t, templateDir)
	ovmfCode := filepath.Join(t.TempDir(), "OVMF_CODE.fd")
	ovmfVars := filepath.Join(t.TempDir(), "OVMF_VARS.fd")
	if err := os.WriteFile(ovmfCode, []byte("code"), 0o644); err != nil {
		t.Fatalf("write ovmf code: %v", err)
	}
	if err := os.WriteFile(ovmfVars, []byte("vars"), 0o644); err != nil {
		t.Fatalf("write ovmf vars: %v", err)
	}
	t.Setenv("PACKER_OVMF_CODE", ovmfCode)
	t.Setenv("PACKER_OVMF_VARS", ovmfVars)

	runner := &fakeCommandRunner{}
	meta, err := Build(context.Background(), loadBuildTestCatalog(t), BuildOptions{
		EntryName:     "buildable",
		OutDir:        outDir,
		WorkDir:       workDir,
		Template:      templateDir,
		CommandRunner: runner,
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if meta.Artifact != "buildable.raw.zst" || meta.SizeBytes == 0 {
		t.Fatalf("metadata = %#v", meta)
	}
	gotCommands := runner.commandLabels()
	wantCommands := []string{"cloud-localds", "packer init", "packer build", "qemu-img convert", "zstd"}
	if !reflect.DeepEqual(gotCommands, wantCommands) {
		t.Fatalf("commands = %#v, want %#v", gotCommands, wantCommands)
	}

	var vars packerVars
	varPath := filepath.Join(workDir, "build.pkrvars.json")
	raw, err := os.ReadFile(varPath)
	if err != nil {
		t.Fatalf("read var file: %v", err)
	}
	if err := json.Unmarshal(raw, &vars); err != nil {
		t.Fatalf("decode var file: %v", err)
	}
	if vars.ImageName != "buildable" || vars.OutputDirectory == "" || vars.SeedISO == "" {
		t.Fatalf("vars = %#v", vars)
	}
	if !reflect.DeepEqual(vars.AptPackages, []string{"linux-image-generic"}) {
		t.Fatalf("apt packages = %#v", vars.AptPackages)
	}
}

func TestBuildWritesEmptyAptPackagesArray(t *testing.T) {
	workDir := filepath.Join(t.TempDir(), "work")
	outDir := filepath.Join(t.TempDir(), "out")
	templateDir := filepath.Join(t.TempDir(), "template")
	writeTemplate(t, templateDir)
	ovmfCode := filepath.Join(t.TempDir(), "OVMF_CODE.fd")
	ovmfVars := filepath.Join(t.TempDir(), "OVMF_VARS.fd")
	if err := os.WriteFile(ovmfCode, []byte("code"), 0o644); err != nil {
		t.Fatalf("write ovmf code: %v", err)
	}
	if err := os.WriteFile(ovmfVars, []byte("vars"), 0o644); err != nil {
		t.Fatalf("write ovmf vars: %v", err)
	}
	t.Setenv("PACKER_OVMF_CODE", ovmfCode)
	t.Setenv("PACKER_OVMF_VARS", ovmfVars)

	catalog := writeCatalog(t, `
entries:
  - name: cloud
    osFamily: custom
    osVersion: "1"
    arch: amd64
    variant: cloud
    format: raw
    sourceFormat: raw
    sourceCompression: zstd
    url: cloud.raw.zst
    bootEnvironment: ubuntu-minimal-cloud-amd64
    build:
      type: packer-qemu-cloud-image
      source:
        url: https://images.example.test/source.qcow2
        checksum: sha256:abc
        format: qcow2
`)
	entries, err := LoadCatalog(context.Background(), LoadOptions{
		CatalogFile:     catalog,
		ReplaceExternal: true,
	})
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	_, err = Build(context.Background(), entries, BuildOptions{
		EntryName:     "cloud",
		OutDir:        outDir,
		WorkDir:       workDir,
		Template:      templateDir,
		CommandRunner: &fakeCommandRunner{},
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	var vars packerVars
	raw, err := os.ReadFile(filepath.Join(workDir, "build.pkrvars.json"))
	if err != nil {
		t.Fatalf("read var file: %v", err)
	}
	if err := json.Unmarshal(raw, &vars); err != nil {
		t.Fatalf("decode var file: %v", err)
	}
	if vars.AptPackages == nil {
		t.Fatalf("apt packages must be an empty array, got nil")
	}
	if len(vars.AptPackages) != 0 {
		t.Fatalf("apt packages = %#v", vars.AptPackages)
	}
	if strings.Contains(string(raw), `"apt_packages": null`) {
		t.Fatalf("var file must not encode apt_packages as null: %s", raw)
	}
}

func loadBuildTestCatalog(t *testing.T) []oscatalog.Entry {
	t.Helper()
	catalog := writeCatalog(t, `
entries:
  - name: buildable
    osFamily: custom
    osVersion: "1"
    arch: amd64
    variant: baremetal
    format: raw
    sourceFormat: raw
    sourceCompression: zstd
    url: buildable.raw.zst
    bootEnvironment: ubuntu-minimal-cloud-amd64
    build:
      type: packer-qemu-cloud-image
      source:
        url: https://images.example.test/source.qcow2
        checksum: sha256:abc
        format: qcow2
      aptPackages:
        - linux-image-generic
      curtinKernelPackage: linux-image-generic
`)
	entries, err := LoadCatalog(context.Background(), LoadOptions{
		CatalogFile:     catalog,
		ReplaceExternal: true,
	})
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	return entries
}

func writeTemplate(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "seed"), 0o755); err != nil {
		t.Fatalf("create seed dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "seed", "user-data.yaml"), []byte("#cloud-config\n"), 0o644); err != nil {
		t.Fatalf("write user-data: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cloud-image.pkr.hcl"), []byte("packer {}\n"), 0o644); err != nil {
		t.Fatalf("write template: %v", err)
	}
}

type fakeCommandRunner struct {
	calls []commandCall
}

type commandCall struct {
	env  []string
	name string
	args []string
}

func (r *fakeCommandRunner) Run(_ context.Context, env []string, name string, args ...string) error {
	r.calls = append(r.calls, commandCall{env: append([]string{}, env...), name: name, args: append([]string{}, args...)})
	switch name {
	case "cloud-localds":
		return os.WriteFile(args[0], []byte("seed"), 0o644)
	case "packer":
		if len(args) > 0 && args[0] == "build" {
			varFile := argAfter(args, "-var-file")
			raw, err := os.ReadFile(varFile)
			if err != nil {
				return err
			}
			var vars packerVars
			if err := json.Unmarshal(raw, &vars); err != nil {
				return err
			}
			if err := os.MkdirAll(vars.OutputDirectory, 0o755); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(vars.OutputDirectory, vars.ImageName+".qcow2"), []byte("qcow2"), 0o644)
		}
	case "qemu-img":
		return os.WriteFile(args[len(args)-1], []byte("raw"), 0o644)
	case "zstd":
		return os.WriteFile(argAfter(args, "-o"), []byte("zstd"), 0o644)
	}
	return nil
}

func (r *fakeCommandRunner) commandLabels() []string {
	out := make([]string, 0, len(r.calls))
	for _, call := range r.calls {
		label := call.name
		if (call.name == "packer" || call.name == "qemu-img") && len(call.args) > 0 {
			label += " " + call.args[0]
		}
		out = append(out, label)
	}
	return out
}

func argAfter(args []string, flag string) string {
	for i, arg := range args {
		if arg == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func writeCatalog(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "catalog.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write catalog: %v", err)
	}
	return path
}
