package osimagebuild

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/sugaf1204/gomi/internal/oscatalog"
)

const defaultMaxReleaseAssetSize int64 = 2147483647

type LoadOptions = oscatalog.LoadOptions

type BuildOptions struct {
	EntryName string
	OutDir    string
	WorkDir   string
	Template  string
	Timeout   string
	MaxSize   int64
}

type Matrix struct {
	Include []MatrixEntry `json:"include"`
}

type MatrixEntry struct {
	Name string `json:"name"`
}

type ImageMetadata struct {
	Name      string   `json:"name"`
	OSFamily  string   `json:"osFamily"`
	OSVersion string   `json:"osVersion"`
	Arch      string   `json:"arch"`
	Variant   string   `json:"variant"`
	Artifact  string   `json:"artifact"`
	SHA256    string   `json:"sha256"`
	SizeBytes int64    `json:"sizeBytes"`
	Packages  []string `json:"packages,omitempty"`
}

func LoadCatalog(ctx context.Context, opts LoadOptions) ([]oscatalog.Entry, error) {
	return oscatalog.Load(ctx, opts)
}

func BuildMatrix(entries []oscatalog.Entry) Matrix {
	buildEntries := oscatalog.BuildEntries(entries)
	matrix := Matrix{Include: make([]MatrixEntry, 0, len(buildEntries))}
	for _, entry := range buildEntries {
		matrix.Include = append(matrix.Include, MatrixEntry{Name: entry.Name})
	}
	return matrix
}

func Build(ctx context.Context, entries []oscatalog.Entry, opts BuildOptions) (ImageMetadata, error) {
	entry, err := findBuildEntry(entries, opts.EntryName)
	if err != nil {
		return ImageMetadata{}, err
	}
	if entry.Build == nil {
		return ImageMetadata{}, fmt.Errorf("%s: catalog entry does not define build recipe", entry.Name)
	}
	if entry.Build.Type != "packer-qemu-cloud-image" {
		return ImageMetadata{}, fmt.Errorf("%s: unsupported build.type %q", entry.Name, entry.Build.Type)
	}
	if opts.Timeout == "" {
		opts.Timeout = "30m"
	}
	if opts.MaxSize == 0 {
		opts.MaxSize = defaultMaxReleaseAssetSize
	}
	repoRoot, err := findRepoRoot()
	if err != nil {
		return ImageMetadata{}, err
	}
	if opts.OutDir == "" {
		opts.OutDir = filepath.Join(repoRoot, "dist", "os-images")
	}
	if opts.WorkDir == "" {
		opts.WorkDir = filepath.Join(repoRoot, "tmp", "osimage-packer", entry.Name)
	}
	if opts.Template == "" {
		opts.Template = entry.Build.PackerTemplate
	}
	if opts.Template == "" {
		opts.Template = filepath.Join(repoRoot, "tools", "osimage", "packer", "cloud-image")
	}
	if !filepath.IsAbs(opts.Template) {
		opts.Template = filepath.Join(repoRoot, opts.Template)
	}
	for _, command := range []string{"cloud-localds", "packer", "qemu-img", "zstd"} {
		if err := requireCommand(command); err != nil {
			return ImageMetadata{}, err
		}
	}
	if err := os.RemoveAll(opts.WorkDir); err != nil {
		return ImageMetadata{}, err
	}
	if err := os.MkdirAll(opts.WorkDir, 0o755); err != nil {
		return ImageMetadata{}, err
	}
	if err := os.MkdirAll(opts.OutDir, 0o755); err != nil {
		return ImageMetadata{}, err
	}
	outputDir := filepath.Join(opts.WorkDir, "output")
	cacheDir := filepath.Join(opts.WorkDir, "packer-cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return ImageMetadata{}, err
	}

	templateDir, err := prepareTemplateDir(opts.Template, opts.WorkDir)
	if err != nil {
		return ImageMetadata{}, err
	}
	seedISO, err := createSeedISO(ctx, opts.WorkDir, templateDir, entry.Name)
	if err != nil {
		return ImageMetadata{}, err
	}
	ovmfCode, ovmfVars, err := copyOVMF(opts.WorkDir)
	if err != nil {
		return ImageMetadata{}, err
	}
	qemuMachine, qemuCPU, err := qemuSettings(entry.Arch)
	if err != nil {
		return ImageMetadata{}, err
	}
	varFile := filepath.Join(opts.WorkDir, "build.pkrvars.hcl")
	if err := writePackerVars(varFile, entry, outputDir, ovmfCode, ovmfVars, seedISO, qemuMachine, qemuCPU, opts.Timeout); err != nil {
		return ImageMetadata{}, err
	}

	if err := run(ctx, cacheDir, "packer", "init", templateDir); err != nil {
		return ImageMetadata{}, err
	}
	if err := run(ctx, cacheDir, "packer", "build", "-var-file", varFile, templateDir); err != nil {
		return ImageMetadata{}, err
	}

	qcow2 := filepath.Join(outputDir, entry.Name+".qcow2")
	if err := requireNonEmptyFile(qcow2); err != nil {
		return ImageMetadata{}, err
	}
	raw := filepath.Join(opts.OutDir, entry.Name+".raw")
	zst := raw + ".zst"
	if err := run(ctx, "", "qemu-img", "convert", "-O", "raw", qcow2, raw); err != nil {
		return ImageMetadata{}, err
	}
	if err := run(ctx, "", "zstd", "-T0", "-19", "-f", raw, "-o", zst); err != nil {
		return ImageMetadata{}, err
	}
	_ = os.Remove(raw)

	meta, err := writeMetadata(entry, zst, opts.OutDir)
	if err != nil {
		return ImageMetadata{}, err
	}
	if meta.SizeBytes > opts.MaxSize {
		return ImageMetadata{}, fmt.Errorf("%s is %d bytes; release assets must be <= %d bytes", filepath.Base(zst), meta.SizeBytes, opts.MaxSize)
	}
	return meta, nil
}

func WriteManifest(dir string) error {
	if dir == "" {
		dir = filepath.Join("dist", "os-images")
	}
	metadataPaths, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return err
	}
	entries := make([]ImageMetadata, 0)
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

	zstPaths, err := filepath.Glob(filepath.Join(dir, "*.raw.zst"))
	if err != nil {
		return err
	}
	sort.Strings(zstPaths)
	var checksums strings.Builder
	for _, path := range zstPaths {
		sum, err := sha256File(path)
		if err != nil {
			return err
		}
		checksums.WriteString(sum)
		checksums.WriteString("  ")
		checksums.WriteString(filepath.Base(path))
		checksums.WriteByte('\n')
	}
	return os.WriteFile(filepath.Join(dir, "checksums-os-images.txt"), []byte(checksums.String()), 0o644)
}

func findBuildEntry(entries []oscatalog.Entry, name string) (oscatalog.Entry, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return oscatalog.Entry{}, errors.New("entry name is required")
	}
	for _, entry := range entries {
		if entry.Name == name {
			return entry, nil
		}
	}
	return oscatalog.Entry{}, fmt.Errorf("catalog entry not found: %s", name)
}

func requireCommand(name string) error {
	if _, err := exec.LookPath(name); err != nil {
		return fmt.Errorf("required command not found: %s", name)
	}
	return nil
}

func createSeedISO(ctx context.Context, workDir, templateDir, imageName string) (string, error) {
	seedDir := filepath.Join(workDir, "seed")
	if err := os.MkdirAll(seedDir, 0o755); err != nil {
		return "", err
	}
	metaData := filepath.Join(seedDir, "meta-data")
	if err := os.WriteFile(metaData, []byte("instance-id: "+imageName+"\nlocal-hostname: "+imageName+"\n"), 0o644); err != nil {
		return "", err
	}
	userData := filepath.Join(templateDir, "seed", "user-data.yaml")
	if err := requireNonEmptyFile(userData); err != nil {
		return "", err
	}
	seedISO := filepath.Join(workDir, "seed.iso")
	if err := run(ctx, "", "cloud-localds", seedISO, userData, metaData); err != nil {
		return "", err
	}
	return seedISO, nil
}

func prepareTemplateDir(src, workDir string) (string, error) {
	st, err := os.Stat(src)
	if err != nil {
		return "", err
	}
	if !st.IsDir() {
		return "", fmt.Errorf("expected template directory: %s", src)
	}

	dst := filepath.Join(workDir, "template")
	if err := os.RemoveAll(dst); err != nil {
		return "", err
	}
	err = filepath.WalkDir(src, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if shouldSkipTemplateEntry(d.Name()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		dstPath := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(dstPath, 0o755)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		return copyFileMode(path, dstPath, info.Mode().Perm())
	})
	if err != nil {
		return "", err
	}
	return dst, nil
}

func shouldSkipTemplateEntry(name string) bool {
	return name == ".DS_Store" || name == "__MACOSX" || strings.HasPrefix(name, "._")
}

func copyOVMF(workDir string) (string, string, error) {
	code, err := findOVMF("PACKER_OVMF_CODE", []string{
		"/usr/share/OVMF/OVMF_CODE_4M.fd",
		"/usr/share/OVMF/OVMF_CODE.fd",
	})
	if err != nil {
		return "", "", err
	}
	vars, err := findOVMF("PACKER_OVMF_VARS", []string{
		"/usr/share/OVMF/OVMF_VARS_4M.fd",
		"/usr/share/OVMF/OVMF_VARS.fd",
	})
	if err != nil {
		return "", "", err
	}
	codeDst := filepath.Join(workDir, "OVMF_CODE.fd")
	varsDst := filepath.Join(workDir, "OVMF_VARS.fd")
	if err := copyFile(code, codeDst); err != nil {
		return "", "", err
	}
	if err := copyFile(vars, varsDst); err != nil {
		return "", "", err
	}
	return codeDst, varsDst, nil
}

func findOVMF(envName string, candidates []string) (string, error) {
	if value := strings.TrimSpace(os.Getenv(envName)); value != "" {
		if err := requireNonEmptyFile(value); err != nil {
			return "", err
		}
		return value, nil
	}
	for _, path := range candidates {
		if err := requireNonEmptyFile(path); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("%s not found", envName)
}

func qemuSettings(arch string) (string, string, error) {
	switch arch {
	case "amd64":
		if canUseKVM() {
			return "accel=kvm", "host", nil
		}
		return "accel=tcg", "max", nil
	case "arm64":
		return "virt", "max", nil
	default:
		return "", "", fmt.Errorf("unsupported qemu architecture: %s", arch)
	}
}

func canUseKVM() bool {
	f, err := os.OpenFile("/dev/kvm", os.O_RDWR, 0)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

func writePackerVars(path string, entry oscatalog.Entry, outputDir, ovmfCode, ovmfVars, seedISO, qemuMachine, qemuCPU, timeout string) error {
	diskSize := entry.Build.DiskSize
	if diskSize == "" {
		diskSize = "8G"
	}
	var b strings.Builder
	writeVar(&b, "image_name", entry.Name)
	writeVar(&b, "architecture", entry.Arch)
	writeVar(&b, "disk_size", diskSize)
	writeVar(&b, "output_directory", outputDir)
	writeVar(&b, "ovmf_code", ovmfCode)
	writeVar(&b, "ovmf_vars", ovmfVars)
	writeVar(&b, "qemu_cpu", qemuCPU)
	writeVar(&b, "qemu_machine", qemuMachine)
	writeVar(&b, "seed_iso", seedISO)
	writeVar(&b, "source_url", entry.Build.Source.URL)
	writeVar(&b, "source_checksum", entry.Build.Source.Checksum)
	writeVar(&b, "source_format", string(entry.Build.Source.Format))
	writeVar(&b, "ssh_username", "root")
	writeVar(&b, "ssh_password", "packer")
	writeVar(&b, "timeout", timeout)
	writeVar(&b, "curtin_kernel_package", entry.Build.CurtinKernelPackage)
	writeListVar(&b, "apt_packages", entry.Build.AptPackages)
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func writeVar(b *strings.Builder, name, value string) {
	b.WriteString(name)
	b.WriteString(" = ")
	b.WriteString(strconv.Quote(value))
	b.WriteByte('\n')
}

func writeListVar(b *strings.Builder, name string, values []string) {
	b.WriteString(name)
	b.WriteString(" = [")
	for i, value := range values {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(strconv.Quote(value))
	}
	b.WriteString("]\n")
}

func writeMetadata(entry oscatalog.Entry, zstPath, outDir string) (ImageMetadata, error) {
	st, err := os.Stat(zstPath)
	if err != nil {
		return ImageMetadata{}, err
	}
	sum, err := sha256File(zstPath)
	if err != nil {
		return ImageMetadata{}, err
	}
	meta := ImageMetadata{
		Name:      entry.Name,
		OSFamily:  entry.OSFamily,
		OSVersion: entry.OSVersion,
		Arch:      entry.Arch,
		Variant:   string(entry.Variant),
		Artifact:  filepath.Base(zstPath),
		SHA256:    sum,
		SizeBytes: st.Size(),
	}
	if entry.Build != nil {
		meta.Packages = append([]string{}, entry.Build.AptPackages...)
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

func run(ctx context.Context, packerCacheDir, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Env = os.Environ()
	if packerCacheDir != "" {
		cmd.Env = append(cmd.Env, "PACKER_CACHE_DIR="+packerCacheDir)
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

func requireNonEmptyFile(path string) error {
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	if st.IsDir() || st.Size() == 0 {
		return fmt.Errorf("expected non-empty file: %s", path)
	}
	return nil
}

func copyFile(src, dst string) error {
	return copyFileMode(src, dst, 0o644)
}

func copyFileMode(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chmod(dst, mode)
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

func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		next := filepath.Dir(dir)
		if next == dir {
			return "", errors.New("could not find repository root")
		}
		dir = next
	}
}
