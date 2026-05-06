package osimagebuild

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sugaf1204/gomi/internal/oscatalog"
)

const defaultMaxReleaseAssetSize int64 = 2147483647

type LoadOptions = oscatalog.LoadOptions

type BuildOptions struct {
	EntryName     string
	OutDir        string
	WorkDir       string
	Template      string
	Timeout       string
	MaxSize       int64
	CommandRunner CommandRunner
}

type CommandRunner interface {
	Run(ctx context.Context, env []string, name string, args ...string) error
}

type execCommandRunner struct{}

func (execCommandRunner) Run(ctx context.Context, env []string, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Env = append(os.Environ(), env...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

func Build(ctx context.Context, entries []oscatalog.Entry, opts BuildOptions) (ImageMetadata, error) {
	entry, err := findBuildEntry(entries, opts.EntryName)
	if err != nil {
		return ImageMetadata{}, err
	}
	if entry.Build == nil {
		return ImageMetadata{}, fmt.Errorf("%s: catalog entry does not define build recipe", entry.Name)
	}
	runner := opts.CommandRunner
	if runner == nil {
		runner = execCommandRunner{}
	}
	timeout := opts.Timeout
	if timeout == "" {
		timeout = "30m"
	}
	maxSize := opts.MaxSize
	if maxSize == 0 {
		maxSize = defaultMaxReleaseAssetSize
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

	if err := os.RemoveAll(opts.WorkDir); err != nil {
		return ImageMetadata{}, err
	}
	outputDir := filepath.Join(opts.WorkDir, "output")
	cacheDir := filepath.Join(opts.WorkDir, "packer-cache")
	for _, dir := range []string{opts.WorkDir, opts.OutDir, cacheDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return ImageMetadata{}, err
		}
	}

	seedISO, err := createSeedISO(ctx, runner, opts.WorkDir, opts.Template, entry.Name)
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
	varFile := filepath.Join(opts.WorkDir, "build.pkrvars.json")
	if err := writePackerVarsJSON(varFile, entry, outputDir, ovmfCode, ovmfVars, seedISO, qemuMachine, qemuCPU, timeout); err != nil {
		return ImageMetadata{}, err
	}

	packerEnv := []string{"PACKER_CACHE_DIR=" + cacheDir}
	if err := runner.Run(ctx, packerEnv, "packer", "init", opts.Template); err != nil {
		return ImageMetadata{}, err
	}
	if err := runner.Run(ctx, packerEnv, "packer", "build", "-var-file", varFile, opts.Template); err != nil {
		return ImageMetadata{}, err
	}

	qcow2 := filepath.Join(outputDir, entry.Name+".qcow2")
	raw := filepath.Join(opts.OutDir, entry.Name+".raw")
	zst := raw + ".zst"
	if err := runner.Run(ctx, nil, "qemu-img", "convert", "-O", "raw", qcow2, raw); err != nil {
		return ImageMetadata{}, err
	}
	if err := runner.Run(ctx, nil, "zstd", "-T0", "-19", "-f", raw, "-o", zst); err != nil {
		return ImageMetadata{}, err
	}
	_ = os.Remove(raw)

	meta, err := writeMetadata(entry, zst, opts.OutDir)
	if err != nil {
		return ImageMetadata{}, err
	}
	if meta.SizeBytes > maxSize {
		return ImageMetadata{}, fmt.Errorf("%s is %d bytes; release assets must be <= %d bytes", filepath.Base(zst), meta.SizeBytes, maxSize)
	}
	return meta, nil
}

type packerVars struct {
	ImageName           string   `json:"image_name"`
	Architecture        string   `json:"architecture"`
	DiskSize            string   `json:"disk_size"`
	OutputDirectory     string   `json:"output_directory"`
	OVMFCode            string   `json:"ovmf_code"`
	OVMFVars            string   `json:"ovmf_vars"`
	QemuCPU             string   `json:"qemu_cpu"`
	QemuMachine         string   `json:"qemu_machine"`
	SeedISO             string   `json:"seed_iso"`
	SourceURL           string   `json:"source_url"`
	SourceChecksum      string   `json:"source_checksum"`
	SourceFormat        string   `json:"source_format"`
	SSHUsername         string   `json:"ssh_username"`
	SSHPassword         string   `json:"ssh_password"`
	Timeout             string   `json:"timeout"`
	CurtinKernelPackage string   `json:"curtin_kernel_package"`
	AptPackages         []string `json:"apt_packages"`
}

func createSeedISO(ctx context.Context, runner CommandRunner, workDir, templateDir, imageName string) (string, error) {
	seedDir := filepath.Join(workDir, "seed")
	if err := os.MkdirAll(seedDir, 0o755); err != nil {
		return "", err
	}
	metaData := filepath.Join(seedDir, "meta-data")
	if err := os.WriteFile(metaData, []byte("instance-id: "+imageName+"\nlocal-hostname: "+imageName+"\n"), 0o644); err != nil {
		return "", err
	}
	seedISO := filepath.Join(workDir, "seed.iso")
	if err := runner.Run(ctx, nil, "cloud-localds", seedISO, filepath.Join(templateDir, "seed", "user-data.yaml"), metaData); err != nil {
		return "", err
	}
	return seedISO, nil
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

func writePackerVarsJSON(path string, entry oscatalog.Entry, outputDir, ovmfCode, ovmfVars, seedISO, qemuMachine, qemuCPU, timeout string) error {
	diskSize := entry.Build.DiskSize
	if diskSize == "" {
		diskSize = "8G"
	}
	raw, err := json.MarshalIndent(packerVars{
		ImageName:           entry.Name,
		Architecture:        entry.Arch,
		DiskSize:            diskSize,
		OutputDirectory:     outputDir,
		OVMFCode:            ovmfCode,
		OVMFVars:            ovmfVars,
		QemuCPU:             qemuCPU,
		QemuMachine:         qemuMachine,
		SeedISO:             seedISO,
		SourceURL:           entry.Build.Source.URL,
		SourceChecksum:      entry.Build.Source.Checksum,
		SourceFormat:        string(entry.Build.Source.Format),
		SSHUsername:         "root",
		SSHPassword:         "packer",
		Timeout:             timeout,
		CurtinKernelPackage: entry.Build.CurtinKernelPackage,
		AptPackages:         entry.Build.AptPackages,
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o644)
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

func findOVMF(envName string, candidates []string) (string, error) {
	if value := strings.TrimSpace(os.Getenv(envName)); value != "" {
		return value, nil
	}
	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("%s not found", envName)
}

func qemuSettings(arch string) (string, string, error) {
	switch arch {
	case "amd64":
		if defaultCanUseKVM() {
			return "accel=kvm", "host", nil
		}
		return "accel=tcg", "max", nil
	case "arm64":
		return "virt", "max", nil
	default:
		return "", "", fmt.Errorf("unsupported qemu architecture: %s", arch)
	}
}

func defaultCanUseKVM() bool {
	f, err := os.OpenFile("/dev/kvm", os.O_RDWR, 0)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
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

func copyFile(src, dst string) error {
	raw, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, raw, 0o644)
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
