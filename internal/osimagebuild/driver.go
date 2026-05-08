package osimagebuild

import (
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"syscall"

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
	if buildEntry.PackageManager == "" {
		buildEntry.PackageManager = "apt"
	}
	if opts.Processors <= 0 {
		opts.Processors = 1
	}
	runner := opts.CommandRunner
	if runner == nil {
		runner = execCommandRunner{}
	}
	if strings.TrimSpace(opts.OutDir) == "" {
		opts.OutDir = defaultOutDir()
	}
	if strings.TrimSpace(opts.WorkDir) == "" {
		opts.WorkDir = filepath.Join(os.TempDir(), "gomi-osimage-work", catalogEntry.Name)
	}

	cacheDir := filepath.Join(opts.WorkDir, "cache")
	rootfsDir := filepath.Join(opts.WorkDir, "rootfs")
	if err := os.RemoveAll(opts.WorkDir); err != nil {
		return ImageMetadata{}, err
	}
	for _, dir := range []string{cacheDir, rootfsDir, opts.OutDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return ImageMetadata{}, err
		}
	}

	sourcePath, err := downloadSource(ctx, buildEntry.Source, cacheDir)
	if err != nil {
		return ImageMetadata{}, err
	}
	if err := extractSource(ctx, runner, buildEntry.Source, sourcePath, rootfsDir); err != nil {
		return ImageMetadata{}, err
	}
	if err := configureRootFS(ctx, runner, buildEntry, rootfsDir); err != nil {
		return ImageMetadata{}, err
	}
	if err := cleanupRootFS(buildEntry, rootfsDir); err != nil {
		return ImageMetadata{}, err
	}
	if err := verifyModules(buildEntry.VerifyModules, rootfsDir); err != nil {
		return ImageMetadata{}, err
	}

	artifact := filepath.Join(opts.OutDir, artifactName(catalogEntry))
	compression := buildEntry.SquashFS.Compression
	if compression == "" {
		compression = "xz"
	}
	blockSize := buildEntry.SquashFS.BlockSize
	if blockSize == "" {
		blockSize = "1M"
	}
	if err := runner.Run(ctx, "mksquashfs", rootfsDir, artifact, "-noappend", "-comp", compression, "-b", blockSize, "-processors", fmt.Sprint(opts.Processors), "-all-root"); err != nil {
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

func downloadSource(ctx context.Context, source Source, cacheDir string) (string, error) {
	sourceURL := strings.TrimSpace(source.URL)
	if sourceURL == "" {
		return "", fmt.Errorf("source.url is required")
	}
	parsed, err := url.Parse(sourceURL)
	if err != nil {
		return "", fmt.Errorf("parse source url: %w", err)
	}
	name := filepath.Base(parsed.Path)
	if name == "." || name == "/" || name == "" {
		name = "source"
	}
	dst := filepath.Join(cacheDir, name)
	tmp := dst + ".download"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download source: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return "", fmt.Errorf("download source: status %d", resp.StatusCode)
	}
	out, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(out, resp.Body); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return "", err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	if err := verifyChecksum(ctx, tmp, sourceURL, source.Checksum); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return dst, nil
}

func verifyChecksum(ctx context.Context, path, sourceURL, checksum string) error {
	expected, err := resolveChecksum(ctx, sourceURL, strings.TrimSpace(checksum))
	if err != nil {
		return err
	}
	if expected.algo == "" {
		return nil
	}
	actual, err := fileDigest(path, expected.algo)
	if err != nil {
		return err
	}
	if !strings.EqualFold(actual, expected.digest) {
		return fmt.Errorf("checksum mismatch for %s: expected %s got %s", filepath.Base(path), expected.digest, actual)
	}
	return nil
}

type checksumValue struct {
	algo   string
	digest string
}

func resolveChecksum(ctx context.Context, sourceURL, checksum string) (checksumValue, error) {
	if checksum == "" {
		return checksumValue{}, nil
	}
	if strings.HasPrefix(checksum, "file:") {
		filename := filepath.Base(mustURLPath(sourceURL))
		checksumURL := strings.TrimSpace(strings.TrimPrefix(checksum, "file:"))
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, checksumURL, nil)
		if err != nil {
			return checksumValue{}, err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return checksumValue{}, fmt.Errorf("download checksum file: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			_, _ = io.Copy(io.Discard, resp.Body)
			return checksumValue{}, fmt.Errorf("download checksum file: status %d", resp.StatusCode)
		}
		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			return checksumValue{}, err
		}
		for _, line := range strings.Split(string(raw), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			candidate := strings.TrimPrefix(strings.TrimPrefix(fields[len(fields)-1], "*"), "./")
			if filepath.Base(candidate) == filename {
				return digestFromValue(fields[0])
			}
		}
		return checksumValue{}, fmt.Errorf("checksum entry not found for %s", filename)
	}
	if algo, digest, ok := strings.Cut(checksum, ":"); ok {
		return parseInlineChecksum(strings.ToLower(strings.TrimSpace(algo)) + ":" + strings.TrimSpace(digest))
	}
	return parseInlineChecksum(checksum)
}

func mustURLPath(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return parsed.Path
}

func parseInlineChecksum(value string) (checksumValue, error) {
	value = strings.TrimSpace(value)
	if algo, digest, ok := strings.Cut(value, ":"); ok {
		algo = strings.ToLower(strings.TrimSpace(algo))
		parsed, err := digestFromValue(digest)
		if err != nil {
			return checksumValue{}, err
		}
		if parsed.algo != algo {
			return checksumValue{}, fmt.Errorf("%s digest length does not match %s", parsed.algo, algo)
		}
		return parsed, nil
	}
	return digestFromValue(value)
}

func digestFromValue(value string) (checksumValue, error) {
	value = strings.TrimSpace(value)
	if _, err := hex.DecodeString(value); err != nil {
		return checksumValue{}, fmt.Errorf("checksum digest must be hex: %w", err)
	}
	switch len(value) {
	case 64:
		return checksumValue{algo: "sha256", digest: value}, nil
	case 128:
		return checksumValue{algo: "sha512", digest: value}, nil
	default:
		return checksumValue{}, fmt.Errorf("unsupported checksum digest length: %d", len(value))
	}
}

func fileDigest(path, algo string) (string, error) {
	var h hash.Hash
	switch strings.ToLower(algo) {
	case "sha256":
		h = sha256.New()
	case "sha512":
		h = sha512.New()
	default:
		return "", fmt.Errorf("unsupported checksum algorithm: %s", algo)
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func extractSource(ctx context.Context, runner CommandRunner, source Source, sourcePath, rootfsDir string) error {
	switch source.Format {
	case "root-tar":
		args := []string{"-xf", sourcePath, "-C", rootfsDir, "--numeric-owner"}
		switch strings.ToLower(source.Compression) {
		case "xz":
			args[0] = "-xJf"
		case "gz", "gzip":
			args[0] = "-xzf"
		case "", "none":
		default:
			return fmt.Errorf("unsupported root-tar compression: %s", source.Compression)
		}
		return runner.Run(ctx, "tar", args...)
	case "squashfs":
		return runner.Run(ctx, "unsquashfs", "-f", "-d", rootfsDir, sourcePath)
	default:
		return fmt.Errorf("unsupported source.format: %s", source.Format)
	}
}

func configureRootFS(ctx context.Context, runner CommandRunner, entry BuildEntry, rootfsDir string) error {
	if len(entry.Packages) == 0 {
		return nil
	}
	if entry.PackageManager != "apt" {
		return fmt.Errorf("unsupported packageManager: %s", entry.PackageManager)
	}
	return withPreparedChroot(ctx, runner, rootfsDir, func() error {
		if err := runner.Run(ctx, "chroot", rootfsDir, "env", "DEBIAN_FRONTEND=noninteractive", "apt-get", "update"); err != nil {
			return err
		}
		args := append([]string{rootfsDir, "env", "DEBIAN_FRONTEND=noninteractive", "apt-get", "install", "-y", "--no-install-recommends"}, entry.Packages...)
		return runner.Run(ctx, "chroot", args...)
	})
}

func withPreparedChroot(ctx context.Context, runner CommandRunner, rootfsDir string, fn func() error) (err error) {
	restoreResolver, err := installResolver(rootfsDir)
	if err != nil {
		return err
	}
	defer func() {
		if restoreErr := restoreResolver(); err == nil && restoreErr != nil {
			err = restoreErr
		}
	}()

	type chrootMount struct {
		target string
		args   []string
	}
	mounts := []chrootMount{}
	for _, bind := range [][2]string{
		{"/proc", filepath.Join(rootfsDir, "proc")},
		{"/sys", filepath.Join(rootfsDir, "sys")},
		{"/dev", filepath.Join(rootfsDir, "dev")},
		{"/run", filepath.Join(rootfsDir, "run")},
	} {
		mounts = append(mounts, chrootMount{
			target: bind[1],
			args:   []string{"--bind", bind[0], bind[1]},
		})
	}
	devpts := filepath.Join(rootfsDir, "dev", "pts")
	mounts = append(mounts, chrootMount{
		target: devpts,
		args:   []string{"-t", "devpts", "devpts", devpts},
	})

	mounted := make([]string, 0, len(mounts))
	defer func() {
		for i := len(mounted) - 1; i >= 0; i-- {
			_ = runner.Run(context.Background(), "umount", mounted[i])
		}
	}()
	for _, mount := range mounts {
		if err := prepareChrootMountTarget(rootfsDir, mount.target); err != nil {
			return err
		}
		if err := runner.Run(ctx, "mount", mount.args...); err != nil {
			return err
		}
		mounted = append(mounted, mount.target)
	}
	return fn()
}

func prepareChrootMountTarget(rootfsDir, target string) error {
	if err := ensureNoSymlinkAncestors(rootfsDir, filepath.Dir(target)); err != nil {
		return err
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return err
	}
	return ensureNoSymlinkAncestors(rootfsDir, target)
}

func installResolver(rootfsDir string) (func() error, error) {
	dst := filepath.Join(rootfsDir, "etc", "resolv.conf")
	if err := ensureNoSymlinkAncestors(rootfsDir, filepath.Dir(dst)); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return nil, err
	}
	if err := ensureNoSymlinkAncestors(rootfsDir, filepath.Dir(dst)); err != nil {
		return nil, err
	}

	backup := dst + ".gomi-build-backup"
	_ = os.Remove(backup)

	existed := false
	if _, err := os.Lstat(dst); err == nil {
		existed = true
		if err := os.Rename(dst, backup); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	restore := func() error {
		_ = os.Remove(dst)
		if !existed {
			return nil
		}
		return os.Rename(backup, dst)
	}
	if err := copyFile("/etc/resolv.conf", dst); err != nil {
		if restoreErr := restore(); restoreErr != nil {
			return nil, fmt.Errorf("%w; restore resolver: %v", err, restoreErr)
		}
		return nil, err
	}
	return restore, nil
}

func cleanupRootFS(entry BuildEntry, rootfsDir string) error {
	paths := append([]string{
		"/var/log/cloud-init.log",
		"/var/log/cloud-init-output.log",
		"/etc/netplan/50-cloud-init.yaml",
		"/etc/ssh/ssh_host_rsa_key",
		"/etc/ssh/ssh_host_rsa_key.pub",
		"/etc/ssh/ssh_host_ecdsa_key",
		"/etc/ssh/ssh_host_ecdsa_key.pub",
		"/etc/ssh/ssh_host_ed25519_key",
		"/etc/ssh/ssh_host_ed25519_key.pub",
	}, entry.CleanupPaths...)
	for _, path := range paths {
		full, err := rootfsPath(rootfsDir, path)
		if err != nil {
			return err
		}
		if err := ensureNoSymlinkAncestors(rootfsDir, filepath.Dir(full)); err != nil {
			return err
		}
		if err := os.RemoveAll(full); err != nil {
			return err
		}
	}
	for _, path := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
		full, err := rootfsPath(rootfsDir, path)
		if err != nil {
			return err
		}
		if err := writeEmptyRegularFile(rootfsDir, full); err != nil {
			return err
		}
	}
	globs := append([]string{"/var/cache/apt/archives/*", "/var/lib/apt/lists/*"}, entry.CleanupGlobs...)
	for _, pattern := range globs {
		globPath, err := rootfsPath(rootfsDir, pattern)
		if err != nil {
			return err
		}
		matches, err := filepath.Glob(globPath)
		if err != nil {
			return err
		}
		for _, match := range matches {
			if err := ensureUnderRoot(rootfsDir, match); err != nil {
				return err
			}
			if err := ensureNoSymlinkAncestors(rootfsDir, filepath.Dir(match)); err != nil {
				return err
			}
			if err := os.RemoveAll(match); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeEmptyRegularFile(rootfsDir, full string) error {
	if err := ensureNoSymlinkAncestors(rootfsDir, filepath.Dir(full)); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	if info, err := os.Lstat(full); err == nil && info.Mode()&os.ModeSymlink != 0 {
		if err := os.Remove(full); err != nil {
			return err
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	f, err := os.OpenFile(full, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|syscall.O_NOFOLLOW, 0o644)
	if err != nil {
		return err
	}
	return f.Close()
}

func ensureNoSymlinkAncestors(rootfsDir, full string) error {
	rootAbs, err := filepath.Abs(rootfsDir)
	if err != nil {
		return err
	}
	fullAbs, err := filepath.Abs(full)
	if err != nil {
		return err
	}
	if err := ensureUnderRoot(rootAbs, fullAbs); err != nil {
		return err
	}
	rel, err := filepath.Rel(rootAbs, fullAbs)
	if err != nil {
		return err
	}
	if rel == "." {
		return nil
	}
	current := rootAbs
	for _, part := range strings.Split(rel, string(os.PathSeparator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("rootfs path contains symlink ancestor: %s", current)
		}
	}
	return nil
}

func rootfsPath(rootfsDir, rel string) (string, error) {
	raw := strings.TrimSpace(rel)
	if raw == "" || strings.Contains(raw, "\\") {
		return "", fmt.Errorf("invalid rootfs path: %s", rel)
	}
	clean := path.Clean(strings.TrimPrefix(raw, "/"))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("invalid rootfs path: %s", rel)
	}
	full := filepath.Join(rootfsDir, filepath.FromSlash(clean))
	if err := ensureUnderRoot(rootfsDir, full); err != nil {
		return "", err
	}
	return full, nil
}

func ensureUnderRoot(rootfsDir, full string) error {
	rootAbs, err := filepath.Abs(rootfsDir)
	if err != nil {
		return err
	}
	fullAbs, err := filepath.Abs(full)
	if err != nil {
		return err
	}
	prefix := rootAbs + string(os.PathSeparator)
	if fullAbs != rootAbs && !strings.HasPrefix(fullAbs, prefix) {
		return fmt.Errorf("rootfs path escapes rootfs: %s", full)
	}
	return nil
}

func verifyModules(modules []string, rootfsDir string) error {
	for _, module := range modules {
		module = strings.TrimSpace(module)
		if module == "" {
			continue
		}
		found := false
		root := filepath.Join(rootfsDir, "lib", "modules")
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			name := filepath.Base(path)
			if strings.HasPrefix(name, module+".ko") {
				found = true
				return filepath.SkipAll
			}
			return nil
		})
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("kernel module not found in rootfs: %s", module)
		}
	}
	return nil
}

func artifactName(entry oscatalog.Entry) string {
	if name := filepath.Base(mustURLPath(entry.URL)); name != "." && name != "/" && name != "" {
		return name
	}
	return entry.Name + ".rootfs.squashfs"
}

func writeMetadata(entry oscatalog.Entry, build BuildEntry, artifact, outDir string) (ImageMetadata, error) {
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
		Packages:  append([]string{}, build.Packages...),
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
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
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
