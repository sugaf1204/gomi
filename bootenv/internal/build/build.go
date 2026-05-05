package build

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sugaf1204/gomi/bootenv/internal/render"
	"github.com/sugaf1204/gomi/bootenv/internal/spec"
)

type Options struct {
	OutputDir     string
	CacheDir      string
	WorkDir       string
	RunnerPath    string
	Version       string
	KeepWork      bool
	HTTPClient    *http.Client
	CommandRunner CommandRunner
	Now           func() time.Time
}

type Result struct {
	OutputDir string
	Manifest  string
}

type CommandRunner interface {
	Run(ctx context.Context, log io.Writer, name string, args ...string) error
}

type execCommandRunner struct{}

func (execCommandRunner) Run(ctx context.Context, log io.Writer, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = log
	cmd.Stderr = log
	return cmd.Run()
}

type bootSource struct {
	Type   string
	ISO    artifactSource
	RootFS artifactSource
}

type artifactSource struct {
	URL    string
	SHA256 string
}

type liveArtifacts struct {
	Kernel liveArtifact
	Initrd liveArtifact
	RootFS liveArtifact
}

type liveArtifact struct {
	Path       string
	SourcePath string
}

type releaseManifest struct {
	SchemaVersion string                         `json:"schemaVersion"`
	Name          string                         `json:"name"`
	Version       string                         `json:"version,omitempty"`
	Distribution  string                         `json:"distribution,omitempty"`
	Release       string                         `json:"release,omitempty"`
	Arch          string                         `json:"arch,omitempty"`
	Source        map[string]string              `json:"source,omitempty"`
	Artifacts     map[string]releaseArtifactInfo `json:"artifacts"`
	Build         map[string]string              `json:"build,omitempty"`
}

type releaseArtifactInfo struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

func Build(ctx context.Context, doc spec.Document, opts Options) (Result, error) {
	if opts.HTTPClient == nil {
		opts.HTTPClient = http.DefaultClient
	}
	if opts.CommandRunner == nil {
		opts.CommandRunner = execCommandRunner{}
	}
	if opts.Now == nil {
		opts.Now = func() time.Time { return time.Now().UTC() }
	}
	if opts.OutputDir == "" {
		opts.OutputDir = filepath.Join("dist", doc.Metadata.Name)
	}
	if opts.CacheDir == "" {
		opts.CacheDir = ".cache"
	}
	if err := requireTools(ctx, opts.CommandRunner); err != nil {
		return Result{}, err
	}
	workDir, cleanup, err := prepareWorkDir(opts.WorkDir, opts.KeepWork)
	if err != nil {
		return Result{}, err
	}
	defer cleanup()
	log := os.Stdout

	rootfsDir := filepath.Join(workDir, "rootfs")
	var source bootSource
	var live liveArtifacts
	switch doc.Spec.RootFS.Source.Type {
	case "debian-live-iso":
		source, live, err = prepareDebianLiveRootFS(ctx, log, doc, opts, workDir, rootfsDir)
	case "ubuntu-cloud-squashfs":
		source, live, err = prepareUbuntuCloudRootFS(ctx, log, doc, opts, rootfsDir)
	default:
		err = fmt.Errorf("unsupported rootfs source %q", doc.Spec.RootFS.Source.Type)
	}
	if err != nil {
		return Result{}, err
	}
	if opts.RunnerPath != "" {
		if err := installDeployRunner(rootfsDir, opts.RunnerPath); err != nil {
			return Result{}, err
		}
	}
	packages := buildRootFSPackages(doc)
	if len(packages) > 0 {
		fmt.Fprintln(log, "installing rootfs packages")
		if err := installRootFSPackages(ctx, log, opts.CommandRunner, rootfsDir, packages); err != nil {
			return Result{}, err
		}
		if err := makeRootFSWritable(ctx, log, opts.CommandRunner, rootfsDir); err != nil {
			return Result{}, err
		}
	}
	if err := applyFiles(rootfsDir, doc.Spec.RootFS.Files); err != nil {
		return Result{}, err
	}
	if err := applyServices(rootfsDir, doc.Spec.RootFS.Services); err != nil {
		return Result{}, err
	}

	if err := os.RemoveAll(opts.OutputDir); err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(opts.OutputDir, 0o755); err != nil {
		return Result{}, err
	}
	kernelPath := filepath.Join(opts.OutputDir, doc.Spec.PXE.KernelPath)
	initrdPath := filepath.Join(opts.OutputDir, doc.Spec.PXE.InitrdPath)
	rootfsPath := filepath.Join(opts.OutputDir, doc.Spec.PXE.RootFSPath)
	if doc.Spec.RootFS.Source.Type == "ubuntu-cloud-squashfs" {
		live, err = generateCloudInitramfs(ctx, log, opts.CommandRunner, rootfsDir)
		if err != nil {
			return Result{}, err
		}
	}
	if err := copyFile(live.Kernel.Path, kernelPath, 0o644); err != nil {
		return Result{}, err
	}
	if err := copyFile(live.Initrd.Path, initrdPath, 0o644); err != nil {
		return Result{}, err
	}
	fmt.Fprintln(log, "packing SquashFS rootfs")
	if err := runPrivileged(ctx, log, opts.CommandRunner, "mksquashfs", rootfsDir, rootfsPath, "-noappend", "-comp", doc.Spec.RootFS.Build.Compression, "-b", doc.Spec.RootFS.Build.BlockSize, "-processors", "1", "-all-root"); err != nil {
		return Result{}, fmt.Errorf("mksquashfs: %w", err)
	}
	if err := os.WriteFile(filepath.Join(opts.OutputDir, doc.Spec.PXE.ScriptName), []byte(render.IPXEScript(doc)), 0o644); err != nil {
		return Result{}, err
	}
	manifestPath, err := writeReleaseManifest(doc, opts, source, live, opts.OutputDir)
	if err != nil {
		return Result{}, err
	}
	if err := writeChecksums(opts.OutputDir, []string{doc.Spec.PXE.KernelPath, doc.Spec.PXE.InitrdPath, doc.Spec.PXE.RootFSPath, doc.Spec.PXE.ScriptName, "manifest.json"}); err != nil {
		return Result{}, err
	}
	return Result{OutputDir: opts.OutputDir, Manifest: manifestPath}, nil
}

func requireTools(ctx context.Context, runner CommandRunner) error {
	checks := []struct {
		tool string
		args []string
	}{
		{tool: "bsdtar", args: []string{"--version"}},
		{tool: "unsquashfs", args: []string{"-help"}},
		{tool: "mksquashfs", args: []string{"-version"}},
	}
	for _, check := range checks {
		if err := runner.Run(ctx, io.Discard, check.tool, check.args...); err != nil {
			return fmt.Errorf("required build tool %q is not available: %w", check.tool, err)
		}
	}
	return nil
}

func prepareWorkDir(path string, keep bool) (string, func(), error) {
	if path != "" {
		if err := os.MkdirAll(path, 0o755); err != nil {
			return "", nil, err
		}
		cleanup := func() {}
		if !keep {
			cleanup = func() { _ = os.RemoveAll(path) }
		}
		return path, cleanup, nil
	}
	dir, err := os.MkdirTemp("", "gomi-bootenv-*")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() {}
	if !keep {
		cleanup = func() { _ = os.RemoveAll(dir) }
	}
	return dir, cleanup, nil
}

func prepareDebianLiveRootFS(ctx context.Context, log io.Writer, doc spec.Document, opts Options, workDir, rootfsDir string) (bootSource, liveArtifacts, error) {
	source, err := resolveDebianLiveISO(ctx, opts.HTTPClient, doc.Spec.RootFS.Source.URL, doc.Spec.Architecture, "standard")
	if err != nil {
		return bootSource{}, liveArtifacts{}, err
	}
	isoPath := filepath.Join(opts.CacheDir, "debian-live-"+source.ISO.SHA256+".iso")
	if err := os.MkdirAll(filepath.Dir(isoPath), 0o755); err != nil {
		return bootSource{}, liveArtifacts{}, err
	}
	if _, err := os.Stat(isoPath); err != nil {
		fmt.Fprintf(log, "downloading %s\n", source.ISO.URL)
		if err := downloadFile(ctx, opts.HTTPClient, source.ISO.URL, isoPath); err != nil {
			return bootSource{}, liveArtifacts{}, err
		}
	}
	if err := verifySHA256(isoPath, source.ISO.SHA256); err != nil {
		_ = os.Remove(isoPath)
		return bootSource{}, liveArtifacts{}, err
	}

	extractDir := filepath.Join(workDir, "iso")
	if err := os.MkdirAll(extractDir, 0o755); err != nil {
		return bootSource{}, liveArtifacts{}, err
	}
	fmt.Fprintln(log, "extracting Debian live ISO")
	if err := opts.CommandRunner.Run(ctx, log, "bsdtar", "-xf", isoPath, "-C", extractDir, "live"); err != nil {
		return bootSource{}, liveArtifacts{}, fmt.Errorf("extract Debian live ISO: %w", err)
	}
	live, err := findDebianLiveArtifacts(extractDir)
	if err != nil {
		return bootSource{}, liveArtifacts{}, err
	}
	fmt.Fprintln(log, "unpacking SquashFS rootfs")
	if err := runPrivileged(ctx, log, opts.CommandRunner, "unsquashfs", "-no-xattrs", "-no-exit-code", "-f", "-d", rootfsDir, live.RootFS.Path); err != nil {
		return bootSource{}, liveArtifacts{}, fmt.Errorf("unsquashfs: %w", err)
	}
	if err := makeRootFSWritable(ctx, log, opts.CommandRunner, rootfsDir); err != nil {
		return bootSource{}, liveArtifacts{}, err
	}
	return source, live, nil
}

func prepareUbuntuCloudRootFS(ctx context.Context, log io.Writer, doc spec.Document, opts Options, rootfsDir string) (bootSource, liveArtifacts, error) {
	source, err := resolveUbuntuCloudSquashFS(ctx, opts.HTTPClient, doc.Spec.RootFS.Source.URL, doc.Spec.RootFS.Source.SHA256, doc.Spec.Architecture)
	if err != nil {
		return bootSource{}, liveArtifacts{}, err
	}
	cacheName := "ubuntu-cloud-" + source.RootFS.SHA256 + ".squashfs"
	if source.RootFS.SHA256 == "" {
		cacheName = "ubuntu-cloud-" + urlCacheKey(source.RootFS.URL) + ".squashfs"
	}
	rootfsImage := filepath.Join(opts.CacheDir, cacheName)
	if err := os.MkdirAll(filepath.Dir(rootfsImage), 0o755); err != nil {
		return bootSource{}, liveArtifacts{}, err
	}
	if _, err := os.Stat(rootfsImage); err != nil {
		fmt.Fprintf(log, "downloading %s\n", source.RootFS.URL)
		if err := downloadFile(ctx, opts.HTTPClient, source.RootFS.URL, rootfsImage); err != nil {
			return bootSource{}, liveArtifacts{}, err
		}
	}
	if source.RootFS.SHA256 != "" {
		if err := verifySHA256(rootfsImage, source.RootFS.SHA256); err != nil {
			_ = os.Remove(rootfsImage)
			return bootSource{}, liveArtifacts{}, err
		}
	}
	fmt.Fprintln(log, "unpacking Ubuntu cloud SquashFS rootfs")
	if err := runPrivileged(ctx, log, opts.CommandRunner, "unsquashfs", "-no-xattrs", "-no-exit-code", "-f", "-d", rootfsDir, rootfsImage); err != nil {
		return bootSource{}, liveArtifacts{}, fmt.Errorf("unsquashfs: %w", err)
	}
	if err := makeRootFSWritable(ctx, log, opts.CommandRunner, rootfsDir); err != nil {
		return bootSource{}, liveArtifacts{}, err
	}
	live := liveArtifacts{RootFS: liveArtifact{Path: rootfsImage, SourcePath: source.RootFS.URL}}
	return source, live, nil
}

func makeRootFSWritable(ctx context.Context, log io.Writer, runner CommandRunner, rootfsDir string) error {
	owner := fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid())
	if err := runPrivileged(ctx, log, runner, "chown", "-R", owner, rootfsDir); err != nil {
		return fmt.Errorf("make rootfs writable: %w", err)
	}
	return nil
}

func resolveDebianLiveISO(ctx context.Context, client *http.Client, baseURL, arch, flavor string) (bootSource, error) {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/") + "/"
	if base == "/" {
		return bootSource{}, fmt.Errorf("Debian live ISO source URL is required")
	}
	raw, err := fetchText(ctx, client, base+"SHA256SUMS")
	if err != nil {
		return bootSource{}, err
	}
	iso, checksum, ok := selectDebianLiveISO(raw, arch, flavor)
	if !ok {
		return bootSource{}, fmt.Errorf("no Debian live %s/%s ISO found in SHA256SUMS", arch, flavor)
	}
	return bootSource{Type: "debian-live-iso", ISO: artifactSource{URL: base + iso, SHA256: checksum}}, nil
}

func resolveUbuntuCloudSquashFS(ctx context.Context, client *http.Client, rawURL, checksum, arch string) (bootSource, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return bootSource{}, fmt.Errorf("Ubuntu cloud SquashFS source URL is required")
	}
	if strings.HasSuffix(rawURL, ".squashfs") {
		if checksum == "" {
			sum, err := checksumFromSiblingSHA256SUMS(ctx, client, rawURL)
			if err != nil {
				return bootSource{}, fmt.Errorf("resolve checksum for %s: %w", rawURL, err)
			}
			checksum = sum
		}
		return bootSource{Type: "ubuntu-cloud-squashfs", RootFS: artifactSource{URL: rawURL, SHA256: checksum}}, nil
	}
	base := strings.TrimRight(rawURL, "/") + "/"
	raw, err := fetchText(ctx, client, base+"SHA256SUMS")
	if err != nil {
		return bootSource{}, err
	}
	name, sum, ok := selectUbuntuCloudSquashFS(raw, arch)
	if !ok {
		return bootSource{}, fmt.Errorf("no Ubuntu cloud %s SquashFS found in SHA256SUMS", arch)
	}
	return bootSource{Type: "ubuntu-cloud-squashfs", RootFS: artifactSource{URL: base + name, SHA256: sum}}, nil
}

func checksumFromSiblingSHA256SUMS(ctx context.Context, client *http.Client, rawURL string) (string, error) {
	idx := strings.LastIndex(rawURL, "/")
	if idx < 0 {
		return "", fmt.Errorf("source URL has no directory")
	}
	base := rawURL[:idx+1]
	name := rawURL[idx+1:]
	raw, err := fetchText(ctx, client, base+"SHA256SUMS")
	if err != nil {
		return "", err
	}
	scanner := bufio.NewScanner(strings.NewReader(raw))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		if strings.TrimPrefix(fields[len(fields)-1], "*") == name {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("%s is not listed in SHA256SUMS", name)
}

func selectUbuntuCloudSquashFS(sums, arch string) (filename, checksum string, ok bool) {
	if arch == "" {
		arch = "amd64"
	}
	type candidate struct{ name, sum string }
	var candidates []candidate
	scanner := bufio.NewScanner(strings.NewReader(sums))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		sum := fields[0]
		name := strings.TrimPrefix(fields[len(fields)-1], "*")
		if strings.HasSuffix(name, ".squashfs") && strings.Contains(name, "-cloudimg-"+arch+".squashfs") {
			candidates = append(candidates, candidate{name: name, sum: sum})
		}
	}
	if len(candidates) == 0 {
		return "", "", false
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].name < candidates[j].name })
	last := candidates[len(candidates)-1]
	return last.name, last.sum, true
}

func selectDebianLiveISO(sums, arch, flavor string) (filename, checksum string, ok bool) {
	if arch == "" {
		arch = "amd64"
	}
	if flavor == "" {
		flavor = "standard"
	}
	type candidate struct{ name, sum string }
	var candidates []candidate
	scanner := bufio.NewScanner(strings.NewReader(sums))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		sum := fields[0]
		name := strings.TrimPrefix(fields[len(fields)-1], "*")
		if strings.HasPrefix(name, "debian-live-") && strings.HasSuffix(name, ".iso") && strings.Contains(name, "-"+arch+"-"+flavor+".iso") {
			candidates = append(candidates, candidate{name: name, sum: sum})
		}
	}
	if len(candidates) == 0 {
		return "", "", false
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].name < candidates[j].name })
	last := candidates[len(candidates)-1]
	return last.name, last.sum, true
}

func fetchText(ctx context.Context, client *http.Client, rawURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch %s: %w", rawURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return "", fmt.Errorf("fetch %s: status %d", rawURL, resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func downloadFile(ctx context.Context, client *http.Client, rawURL, dst string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", rawURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return fmt.Errorf("download %s: status %d", rawURL, resp.StatusCode)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, resp.Body)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	return os.Rename(tmp, dst)
}

func findDebianLiveArtifacts(extractDir string) (liveArtifacts, error) {
	liveDir := filepath.Join(extractDir, "live")
	if st, err := os.Stat(liveDir); err != nil || !st.IsDir() {
		return liveArtifacts{}, fmt.Errorf("Debian live ISO has no live directory")
	}
	var kernels, initrds, rootfs []liveArtifact
	err := filepath.WalkDir(liveDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		base := filepath.Base(path)
		rel, err := filepath.Rel(extractDir, path)
		if err != nil {
			return err
		}
		artifact := liveArtifact{Path: path, SourcePath: filepath.ToSlash(rel)}
		switch {
		case strings.HasPrefix(base, "vmlinuz"):
			kernels = append(kernels, artifact)
		case strings.HasPrefix(base, "initrd"):
			initrds = append(initrds, artifact)
		case base == "filesystem.squashfs":
			rootfs = append(rootfs, artifact)
		}
		return nil
	})
	if err != nil {
		return liveArtifacts{}, err
	}
	sort.Slice(kernels, func(i, j int) bool { return kernels[i].SourcePath < kernels[j].SourcePath })
	sort.Slice(initrds, func(i, j int) bool { return initrds[i].SourcePath < initrds[j].SourcePath })
	sort.Slice(rootfs, func(i, j int) bool { return rootfs[i].SourcePath < rootfs[j].SourcePath })
	if len(kernels) == 0 || len(initrds) == 0 || len(rootfs) == 0 {
		return liveArtifacts{}, fmt.Errorf("Debian live ISO is missing kernel/initrd/filesystem.squashfs")
	}
	return liveArtifacts{Kernel: kernels[0], Initrd: initrds[0], RootFS: rootfs[0]}, nil
}

func buildRootFSPackages(doc spec.Document) []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(pkg string) {
		if pkg == "" {
			return
		}
		if _, ok := seen[pkg]; ok {
			return
		}
		seen[pkg] = struct{}{}
		out = append(out, pkg)
	}
	for _, pkg := range doc.Spec.RootFS.Packages {
		add(pkg)
	}
	if doc.Spec.RootFS.Source.Type == "ubuntu-cloud-squashfs" {
		add(doc.Spec.Kernel.Package)
		add("initramfs-tools")
		add("cloud-initramfs-rooturl")
		add("cloud-initramfs-copymods")
		add("overlayroot")
		for _, pkg := range doc.Spec.Initramfs.Packages {
			add(pkg)
		}
	}
	return out
}

func generateCloudInitramfs(ctx context.Context, log io.Writer, runner CommandRunner, rootfsDir string) (liveArtifacts, error) {
	mounted, err := mountRootFSPseudoFilesystems(ctx, log, runner, rootfsDir)
	if err != nil {
		return liveArtifacts{}, err
	}
	defer unmountAll(ctx, log, runner, mounted)
	if err := runPrivileged(ctx, log, runner, "chroot", rootfsDir, "update-initramfs", "-u", "-k", "all"); err != nil {
		return liveArtifacts{}, fmt.Errorf("update initramfs in rootfs: %w", err)
	}
	kernel, err := newestBootArtifact(rootfsDir, "vmlinuz-")
	if err != nil {
		return liveArtifacts{}, err
	}
	initrd, err := newestBootArtifact(rootfsDir, "initrd.img-")
	if err != nil {
		return liveArtifacts{}, err
	}
	return liveArtifacts{
		Kernel: liveArtifact{Path: kernel, SourcePath: strings.TrimPrefix(kernel, rootfsDir+"/")},
		Initrd: liveArtifact{Path: initrd, SourcePath: strings.TrimPrefix(initrd, rootfsDir+"/")},
	}, nil
}

func newestBootArtifact(rootfsDir, prefix string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(rootfsDir, "boot", prefix+"*"))
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("rootfs has no /boot/%s* artifact", prefix)
	}
	sort.Strings(matches)
	return matches[len(matches)-1], nil
}

func installDeployRunner(rootfsDir, runnerPath string) error {
	dst := filepath.Join(rootfsDir, "usr", "local", "sbin", "gomi-deploy-runner")
	return copyFile(runnerPath, dst, 0o755)
}

func installRootFSPackages(ctx context.Context, log io.Writer, runner CommandRunner, rootfsDir string, packages []string) error {
	if len(packages) == 0 {
		return nil
	}
	mounted, err := mountRootFSPseudoFilesystems(ctx, log, runner, rootfsDir)
	if err != nil {
		return err
	}
	defer unmountAll(ctx, log, runner, mounted)

	aptConf := filepath.Join(rootfsDir, "etc", "apt", "apt.conf.d", "99gomi-bootenv")
	if err := os.MkdirAll(filepath.Dir(aptConf), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(aptConf, []byte(strings.Join([]string{
		`Acquire::Languages "none";`,
		`Acquire::Retries "3";`,
		`Acquire::http::Timeout "30";`,
		`APT::Install-Recommends "false";`,
		"",
	}, "\n")), 0o644); err != nil {
		return err
	}
	if mirror := strings.TrimRight(strings.TrimSpace(os.Getenv("GOMI_BOOTENV_APT_MIRROR")), "/"); mirror != "" {
		if err := rewriteAptSourcesMirror(rootfsDir, mirror+"/"); err != nil {
			return err
		}
	}

	if err := runPrivileged(ctx, log, runner, "chroot", rootfsDir, "apt-get", "-o", "Acquire::Languages=none", "update"); err != nil {
		return fmt.Errorf("apt-get update in rootfs: %w", err)
	}
	args := append([]string{rootfsDir, "env", "DEBIAN_FRONTEND=noninteractive", "apt-get", "install", "-y", "--no-install-recommends"}, packages...)
	if err := runPrivileged(ctx, log, runner, "chroot", args...); err != nil {
		return fmt.Errorf("apt-get install in rootfs: %w", err)
	}
	_ = runPrivileged(ctx, log, runner, "chroot", rootfsDir, "apt-get", "clean")
	_ = os.RemoveAll(filepath.Join(rootfsDir, "var", "lib", "apt", "lists"))
	return nil
}

func rewriteAptSourcesMirror(rootfsDir, mirror string) error {
	aptDir := filepath.Join(rootfsDir, "etc", "apt")
	return filepath.WalkDir(aptDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !strings.HasSuffix(path, ".list") && !strings.HasSuffix(path, ".sources") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		updated := strings.ReplaceAll(string(body), "http://archive.ubuntu.com/ubuntu/", mirror)
		updated = strings.ReplaceAll(updated, "http://security.ubuntu.com/ubuntu/", mirror)
		if updated == string(body) {
			return nil
		}
		return os.WriteFile(path, []byte(updated), 0o644)
	})
}

func mountRootFSPseudoFilesystems(ctx context.Context, log io.Writer, runner CommandRunner, rootfsDir string) ([]string, error) {
	for _, dir := range []string{"proc", "sys", "dev"} {
		if err := os.MkdirAll(filepath.Join(rootfsDir, dir), 0o755); err != nil {
			return nil, err
		}
	}
	if err := installChrootResolver(rootfsDir); err != nil {
		return nil, err
	}

	mounts := []struct {
		target string
		args   []string
	}{
		{target: "proc", args: []string{"-t", "proc", "proc"}},
		{target: "sys", args: []string{"-t", "sysfs", "sysfs"}},
		{target: "dev", args: []string{"--bind", "/dev"}},
	}
	var mounted []string
	for _, mount := range mounts {
		target := filepath.Join(rootfsDir, mount.target)
		args := append(append([]string{}, mount.args...), target)
		if err := runPrivileged(ctx, log, runner, "mount", args...); err != nil {
			unmountAll(ctx, log, runner, mounted)
			return nil, fmt.Errorf("mount %s: %w", mount.target, err)
		}
		mounted = append(mounted, target)
	}
	return mounted, nil
}

func installChrootResolver(rootfsDir string) error {
	dst := filepath.Join(rootfsDir, "etc", "resolv.conf")
	raw, err := os.ReadFile("/etc/resolv.conf")
	if err != nil {
		raw = nil
	}
	if len(raw) == 0 || resolvConfUsesLoopback(raw) {
		raw = []byte("nameserver 1.1.1.1\nnameserver 8.8.8.8\n")
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	_ = os.Remove(dst)
	return os.WriteFile(dst, raw, 0o644)
}

func resolvConfUsesLoopback(raw []byte) bool {
	hasNameserver := false
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "nameserver" {
			continue
		}
		hasNameserver = true
		if strings.HasPrefix(fields[1], "127.") || fields[1] == "::1" {
			return true
		}
	}
	return !hasNameserver
}

func runPrivileged(ctx context.Context, log io.Writer, runner CommandRunner, name string, args ...string) error {
	if os.Geteuid() == 0 {
		return runner.Run(ctx, log, name, args...)
	}
	return runner.Run(ctx, log, "sudo", append([]string{name}, args...)...)
}

func unmountAll(ctx context.Context, log io.Writer, runner CommandRunner, targets []string) {
	for i := len(targets) - 1; i >= 0; i-- {
		_ = runPrivileged(ctx, log, runner, "umount", targets[i])
	}
}

func applyFiles(rootfsDir string, files []spec.File) error {
	for _, file := range files {
		mode := os.FileMode(0o644)
		if file.Mode != "" {
			parsed, err := strconv.ParseUint(file.Mode, 8, 32)
			if err != nil {
				return fmt.Errorf("invalid mode for %s: %w", file.Path, err)
			}
			mode = os.FileMode(parsed)
		}
		path := filepath.Join(rootfsDir, strings.TrimPrefix(file.Path, "/"))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(file.Contents), mode); err != nil {
			return err
		}
	}
	return nil
}

func applyServices(rootfsDir string, services []spec.Service) error {
	serviceDir := filepath.Join(rootfsDir, "etc", "systemd", "system")
	wantsDir := filepath.Join(serviceDir, "multi-user.target.wants")
	for _, service := range services {
		if len(service.Command) == 0 {
			continue
		}
		if err := os.MkdirAll(wantsDir, 0o755); err != nil {
			return err
		}
		unitName := service.Name + ".service"
		body := "[Unit]\nDescription=GOMI boot environment service " + service.Name + "\nWants=network-online.target\nAfter=network-online.target\n\n" +
			"[Service]\nType=oneshot\nExecStart=" + strings.Join(service.Command, " ") + "\nStandardOutput=journal+console\nStandardError=journal+console\n\n" +
			"[Install]\nWantedBy=multi-user.target\n"
		unitPath := filepath.Join(serviceDir, unitName)
		if err := os.WriteFile(unitPath, []byte(body), 0o644); err != nil {
			return err
		}
		if service.Enable {
			link := filepath.Join(wantsDir, unitName)
			_ = os.Remove(link)
			if err := os.Symlink("../"+unitName, link); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeReleaseManifest(doc spec.Document, opts Options, source bootSource, live liveArtifacts, outputDir string) (string, error) {
	artifacts := map[string]releaseArtifactInfo{}
	for key, name := range map[string]string{"kernel": doc.Spec.PXE.KernelPath, "initrd": doc.Spec.PXE.InitrdPath, "rootfs": doc.Spec.PXE.RootFSPath} {
		info, err := artifactInfo(filepath.Join(outputDir, name), name)
		if err != nil {
			return "", err
		}
		artifacts[key] = info
	}
	sourceInfo := map[string]string{"type": source.Type}
	if source.ISO.URL != "" {
		sourceInfo["url"] = source.ISO.URL
		sourceInfo["sha256"] = source.ISO.SHA256
	}
	if source.RootFS.URL != "" {
		sourceInfo["url"] = source.RootFS.URL
		sourceInfo["sha256"] = source.RootFS.SHA256
	}
	distribution := "debian"
	release := "current-live"
	if source.Type == "ubuntu-cloud-squashfs" {
		distribution = "ubuntu"
		release = "cloud"
	}
	manifest := releaseManifest{
		SchemaVersion: "gomi.bootenv/v1",
		Name:          doc.Metadata.Name,
		Version:       opts.Version,
		Distribution:  distribution,
		Release:       release,
		Arch:          doc.Spec.Architecture,
		Source:        sourceInfo,
		Artifacts:     artifacts,
		Build: map[string]string{
			"createdAt": opts.Now().Format(time.RFC3339),
			"builder":   "gomi-bootenv",
			"kernel":    live.Kernel.SourcePath,
			"initrd":    live.Initrd.SourcePath,
			"rootfs":    live.RootFS.SourcePath,
		},
	}
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", err
	}
	path := filepath.Join(outputDir, "manifest.json")
	if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func artifactInfo(path, name string) (releaseArtifactInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		return releaseArtifactInfo{}, err
	}
	sum, err := fileSHA256(path)
	if err != nil {
		return releaseArtifactInfo{}, err
	}
	return releaseArtifactInfo{Path: name, SHA256: sum, Size: info.Size()}, nil
}

func writeChecksums(outputDir string, names []string) error {
	var lines []string
	for _, name := range names {
		sum, err := fileSHA256(filepath.Join(outputDir, name))
		if err != nil {
			return err
		}
		lines = append(lines, sum+"  "+name)
	}
	sort.Strings(lines)
	return os.WriteFile(filepath.Join(outputDir, "checksums.txt"), []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

func verifySHA256(path, expected string) error {
	expected = strings.TrimSpace(strings.TrimPrefix(expected, "sha256:"))
	actual, err := fileSHA256(path)
	if err != nil {
		return err
	}
	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("checksum mismatch for %s: expected %s got %s", path, expected, actual)
	}
	return nil
}

func fileSHA256(path string) (string, error) {
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

func urlCacheKey(rawURL string) string {
	sum := sha256.Sum256([]byte(rawURL))
	return hex.EncodeToString(sum[:])[:16]
}

func copyFile(src, dst string, mode os.FileMode) error {
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
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}
