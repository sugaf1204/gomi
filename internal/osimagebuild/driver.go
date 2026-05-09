package osimagebuild

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sugaf1204/gomi/internal/oscatalog"
	"github.com/sugaf1204/gomi/internal/osimage"
)

const defaultRootPath = "rootfs.squashfs"

type BuildOptions struct {
	EntryName     string
	OutDir        string
	WorkDir       string
	Processors    int
	CommandRunner CommandRunner
}

type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) error
}

type execCommandRunner struct{}

func (execCommandRunner) Run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

type rootFSBuilder interface {
	BuildRootFS(ctx context.Context, req BuildRootFSRequest) (string, error)
}

type BuildRootFSRequest struct {
	DefinitionPath string
	RootFSDir      string
	WorkDir        string
}

func Build(ctx context.Context, entries []oscatalog.Entry, cfg Config, opts BuildOptions) (ImageMetadata, error) {
	catalogEntry, err := findCatalogEntry(entries, opts.EntryName)
	if err != nil {
		return ImageMetadata{}, err
	}
	if catalogEntry.Format != osimage.FormatSquashFS {
		return ImageMetadata{}, fmt.Errorf("%s: catalog format must be squashfs", catalogEntry.Name)
	}
	buildEntry, err := findBuildEntry(cfg.Entries, catalogEntry.Name)
	if err != nil {
		return ImageMetadata{}, err
	}
	if err := validateBuildEntry(catalogEntry.Name, buildEntry); err != nil {
		return ImageMetadata{}, err
	}
	if opts.Processors <= 0 {
		opts.Processors = 1
	}
	if strings.TrimSpace(opts.OutDir) == "" {
		opts.OutDir = defaultOutDir()
	}
	if strings.TrimSpace(opts.WorkDir) == "" {
		opts.WorkDir = filepath.Join(os.TempDir(), "gomi-osimage-work", catalogEntry.Name)
	}
	runner := opts.CommandRunner
	if runner == nil {
		runner = execCommandRunner{}
	}

	if err := os.RemoveAll(opts.WorkDir); err != nil {
		return ImageMetadata{}, err
	}
	for _, dir := range []string{opts.WorkDir, opts.OutDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return ImageMetadata{}, err
		}
	}

	definitionPath, err := resolveDefinitionPath(cfg, buildEntry, opts.WorkDir)
	if err != nil {
		return ImageMetadata{}, err
	}
	rootfsDir := filepath.Join(opts.WorkDir, "rootfs")
	builder, err := newRootFSBuilder(buildEntry, runner)
	if err != nil {
		return ImageMetadata{}, err
	}
	rootfsDir, err = builder.BuildRootFS(ctx, BuildRootFSRequest{
		DefinitionPath: definitionPath,
		RootFSDir:      rootfsDir,
		WorkDir:        opts.WorkDir,
	})
	if err != nil {
		return ImageMetadata{}, err
	}
	if info, err := os.Stat(rootfsDir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ImageMetadata{}, fmt.Errorf("rootfs build did not produce %s", rootfsDir)
		}
		return ImageMetadata{}, err
	} else if !info.IsDir() {
		return ImageMetadata{}, fmt.Errorf("rootfs build output is not a directory: %s", rootfsDir)
	}

	artifact := filepath.Join(opts.OutDir, artifactName(catalogEntry))
	if err := runMKSquashFS(ctx, runner, rootfsDir, artifact, buildEntry.SquashFS, opts.Processors); err != nil {
		return ImageMetadata{}, err
	}
	if _, err := os.Stat(artifact); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ImageMetadata{}, fmt.Errorf("mksquashfs did not produce %s", artifact)
		}
		return ImageMetadata{}, err
	}

	meta, err := writeMetadata(catalogEntry, buildEntry, artifact, opts.OutDir)
	if err != nil {
		return ImageMetadata{}, err
	}
	if err := WriteManifest(opts.OutDir); err != nil {
		return ImageMetadata{}, err
	}
	return meta, nil
}

func runMKSquashFS(ctx context.Context, runner CommandRunner, rootfsDir, artifact string, cfg SquashFS, processors int) error {
	compression := cfg.Compression
	if compression == "" {
		compression = "xz"
	}
	blockSize := cfg.BlockSize
	if blockSize == "" {
		blockSize = "1M"
	}
	if err := runner.Run(ctx, "mksquashfs", rootfsDir, artifact, "-noappend", "-comp", compression, "-b", blockSize, "-processors", fmt.Sprint(processors), "-all-root"); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return fmt.Errorf("mksquashfs is required on the build host: %w", err)
		}
		return fmt.Errorf("build squashfs artifact: %w", err)
	}
	return nil
}

func defaultOutDir() string {
	if root, err := findRepoRoot(); err == nil {
		return filepath.Join(root, "dist", "os-images")
	}
	return filepath.Join("/var/lib/gomi", "os-images")
}

func findCatalogEntry(entries []oscatalog.Entry, name string) (oscatalog.Entry, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return oscatalog.Entry{}, fmt.Errorf("entry name is required")
	}
	for _, entry := range entries {
		if entry.Name == name {
			return entry, nil
		}
	}
	return oscatalog.Entry{}, fmt.Errorf("catalog entry not found: %s", name)
}

func artifactName(entry oscatalog.Entry) string {
	if name := filepath.Base(mustURLPath(entry.URL)); name != "." && name != "/" && name != "" {
		return name
	}
	return entry.Name + ".rootfs.squashfs"
}

func findRepoRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for dir := wd; ; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("repository root not found")
		}
	}
}
