package bootenv

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Phase string

const (
	PhaseMissing  Phase = "missing"
	PhaseBuilding Phase = "building"
	PhaseReady    Phase = "ready"
	PhaseError    Phase = "error"
)

type Status struct {
	Name        string    `json:"name"`
	Phase       Phase     `json:"phase"`
	Message     string    `json:"message,omitempty"`
	ArtifactDir string    `json:"artifactDir,omitempty"`
	LogPath     string    `json:"logPath,omitempty"`
	KernelPath  string    `json:"kernelPath,omitempty"`
	InitrdPath  string    `json:"initrdPath,omitempty"`
	RootFSPath  string    `json:"rootfsPath,omitempty"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type Definition struct {
	Name string
}

type Config struct {
	DataDir    string
	FilesDir   string
	SourceURL  string
	HTTPClient *http.Client
	Now        func() time.Time
}

type Manager struct {
	dataDir     string
	filesDir    string
	sourceURL   string
	httpClient  *http.Client
	now         func() time.Time
	definitions map[string]Definition

	mu       sync.Mutex
	statuses map[string]Status
	running  map[string]struct{}
	done     map[string]chan struct{}
}

func NewManager(cfg Config) *Manager {
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = http.DefaultClient
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	m := &Manager{
		dataDir:     filepath.Join(cfg.DataDir, "bootenv"),
		filesDir:    cfg.FilesDir,
		sourceURL:   strings.TrimSpace(cfg.SourceURL),
		httpClient:  cfg.HTTPClient,
		now:         cfg.Now,
		definitions: defaultDefinitions(),
		statuses:    map[string]Status{},
		running:     map[string]struct{}{},
		done:        map[string]chan struct{}{},
	}
	return m
}

func defaultDefinitions() map[string]Definition {
	return map[string]Definition{
		"ubuntu-minimal-cloud-amd64": {
			Name: "ubuntu-minimal-cloud-amd64",
		},
	}
}

func (m *Manager) List() []Status {
	out := make([]Status, 0, len(m.definitions))
	for name := range m.definitions {
		out = append(out, m.Status(name))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (m *Manager) Status(name string) Status {
	name = strings.TrimSpace(name)
	m.mu.Lock()
	if st, ok := m.statuses[name]; ok {
		if _, running := m.running[name]; running || st.Phase == PhaseError {
			m.mu.Unlock()
			return st
		}
	}
	m.mu.Unlock()

	def, ok := m.definitions[name]
	if !ok {
		return Status{Name: name, Phase: PhaseError, Message: "unknown boot environment", UpdatedAt: m.now()}
	}
	if dir, ok := m.currentArtifactDir(def.Name); ok {
		st := Status{
			Name:        def.Name,
			Phase:       PhaseReady,
			ArtifactDir: dir,
			KernelPath:  filepath.Join(dir, "boot-kernel"),
			InitrdPath:  filepath.Join(dir, "boot-initrd"),
			RootFSPath:  filepath.Join(dir, "rootfs.squashfs"),
			UpdatedAt:   m.now(),
		}
		if saved, ok := readSavedStatus(filepath.Join(dir, "status.json"), def.Name); ok {
			st = saved
			st.Phase = PhaseReady
			st.ArtifactDir = dir
			st.KernelPath = filepath.Join(dir, "boot-kernel")
			st.InitrdPath = filepath.Join(dir, "boot-initrd")
			st.RootFSPath = filepath.Join(dir, "rootfs.squashfs")
			if st.UpdatedAt.IsZero() {
				st.UpdatedAt = m.now()
			}
		}
		if st.LogPath == "" {
			logPath := filepath.Join(m.dataDir, "logs", def.Name+"-"+filepath.Base(dir)+".log")
			if _, err := os.Stat(logPath); err == nil {
				st.LogPath = logPath
			}
		}
		return st
	}
	return Status{Name: def.Name, Phase: PhaseMissing, UpdatedAt: m.now()}
}

func (m *Manager) StartEnsure(name string) Status {
	return m.start(name, false)
}

func (m *Manager) StartRebuild(name string) Status {
	return m.start(name, true)
}

func (m *Manager) start(name string, force bool) Status {
	name = strings.TrimSpace(name)
	def, ok := m.definitions[name]
	if !ok {
		return Status{Name: name, Phase: PhaseError, Message: "unknown boot environment", UpdatedAt: m.now()}
	}
	if !force {
		current := m.Status(def.Name)
		if current.Phase == PhaseReady {
			return current
		}
	}
	m.mu.Lock()
	if _, ok := m.running[def.Name]; ok {
		st := m.statuses[def.Name]
		m.mu.Unlock()
		return st
	}
	st := Status{Name: def.Name, Phase: PhaseBuilding, Message: "queued", UpdatedAt: m.now()}
	m.statuses[def.Name] = st
	m.running[def.Name] = struct{}{}
	m.done[def.Name] = make(chan struct{})
	m.mu.Unlock()

	go m.runBuild(context.Background(), def, force)
	return st
}

func (m *Manager) Ensure(ctx context.Context, name string) (Status, error) {
	name = strings.TrimSpace(name)
	def, ok := m.definitions[name]
	if !ok {
		return Status{Name: name, Phase: PhaseError, Message: "unknown boot environment", UpdatedAt: m.now()}, fmt.Errorf("unknown boot environment: %s", name)
	}
	current := m.Status(def.Name)
	if current.Phase == PhaseReady {
		return current, nil
	}
	for {
		m.mu.Lock()
		if ch, ok := m.done[def.Name]; ok {
			m.mu.Unlock()
			select {
			case <-ch:
				st := m.Status(def.Name)
				if st.Phase == PhaseReady {
					return st, nil
				}
				if st.Phase == PhaseError {
					return st, fmt.Errorf("%s", st.Message)
				}
				continue
			case <-ctx.Done():
				return m.Status(def.Name), ctx.Err()
			}
		}
		ch := make(chan struct{})
		m.statuses[def.Name] = Status{Name: def.Name, Phase: PhaseBuilding, Message: "fetching", UpdatedAt: m.now()}
		m.running[def.Name] = struct{}{}
		m.done[def.Name] = ch
		m.mu.Unlock()

		st, err := m.build(ctx, def, false)
		m.finishBuild(def.Name, st, err)
		return m.Status(def.Name), err
	}
}

func (m *Manager) runBuild(ctx context.Context, def Definition, force bool) {
	st, err := m.build(ctx, def, force)
	m.finishBuild(def.Name, st, err)
}

func (m *Manager) finishBuild(name string, st Status, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ch := m.done[name]
	delete(m.running, name)
	delete(m.done, name)
	if err != nil {
		st.Phase = PhaseError
		st.Message = err.Error()
		st.UpdatedAt = m.now()
	}
	m.statuses[name] = st
	if ch != nil {
		close(ch)
	}
}

func (m *Manager) build(ctx context.Context, def Definition, force bool) (Status, error) {
	if !force {
		if st := m.Status(def.Name); st.Phase == PhaseReady {
			return st, nil
		}
	}
	buildID := m.now().Format("20060102T150405Z")
	buildDir := filepath.Join(m.dataDir, "builds", def.Name+"-"+buildID)
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		return Status{Name: def.Name, Phase: PhaseError, UpdatedAt: m.now()}, err
	}
	defer removeAllWritable(buildDir)

	logDir := filepath.Join(m.dataDir, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return Status{Name: def.Name, Phase: PhaseError, UpdatedAt: m.now()}, err
	}
	logPath := filepath.Join(logDir, def.Name+"-"+buildID+".log")
	logFile, err := os.Create(logPath)
	if err != nil {
		return Status{Name: def.Name, Phase: PhaseError, UpdatedAt: m.now()}, err
	}
	defer logFile.Close()
	log := func(format string, args ...any) {
		fmt.Fprintf(logFile, time.Now().UTC().Format(time.RFC3339)+" "+format+"\n", args...)
	}

	st := Status{Name: def.Name, Phase: PhaseBuilding, Message: "fetching boot environment", LogPath: logPath, UpdatedAt: m.now()}
	m.setStatus(st)
	if m.sourceURL == "" {
		return st, fmt.Errorf("boot environment source URL is required")
	}
	log("fetching prebuilt boot environment from %s", m.sourceURL)
	return m.fetchPrebuilt(ctx, def, buildID, buildDir, logPath, logFile, st)
}

func (m *Manager) setStatus(st Status) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.statuses[st.Name] = st
}

type prebuiltManifest struct {
	SchemaVersion string                      `json:"schemaVersion"`
	Name          string                      `json:"name"`
	Version       string                      `json:"version,omitempty"`
	Distribution  string                      `json:"distribution,omitempty"`
	Release       string                      `json:"release,omitempty"`
	Arch          string                      `json:"arch,omitempty"`
	Source        map[string]any              `json:"source,omitempty"`
	Artifacts     map[string]prebuiltArtifact `json:"artifacts"`
	Build         map[string]any              `json:"build,omitempty"`
}

type prebuiltArtifact struct {
	Path   string `json:"path"`
	URL    string `json:"url,omitempty"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size,omitempty"`
}

func (m *Manager) fetchPrebuilt(ctx context.Context, def Definition, buildID, buildDir, logPath string, logFile io.Writer, st Status) (Status, error) {
	manifestLocation, baseLocation := resolvePrebuiltManifestLocation(m.sourceURL)
	st.Message = "downloading boot environment manifest"
	st.UpdatedAt = m.now()
	m.setStatus(st)
	fmt.Fprintf(logFile, time.Now().UTC().Format(time.RFC3339)+" downloading boot environment manifest from %s\n", manifestLocation)

	rawManifest, err := m.readLocation(ctx, manifestLocation)
	if err != nil {
		return st, err
	}
	var manifest prebuiltManifest
	if err := json.Unmarshal(rawManifest, &manifest); err != nil {
		return st, fmt.Errorf("parse boot environment manifest: %w", err)
	}
	if err := validatePrebuiltManifest(def, manifest); err != nil {
		return st, err
	}

	artifactDir := filepath.Join(m.dataDir, "artifacts", def.Name, buildID)
	artifactPublished := false
	defer func() {
		if !artifactPublished {
			_ = os.RemoveAll(artifactDir)
		}
	}()
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		return st, err
	}

	artifactMap := map[string]string{
		"kernel": "boot-kernel",
		"initrd": "boot-initrd",
		"rootfs": "rootfs.squashfs",
	}
	for key, dstName := range artifactMap {
		artifact := manifest.Artifacts[key]
		st.Message = "downloading boot environment artifact: " + key
		st.UpdatedAt = m.now()
		m.setStatus(st)
		src := resolvePrebuiltArtifactLocation(baseLocation, artifact)
		dst := filepath.Join(artifactDir, dstName)
		fmt.Fprintf(logFile, time.Now().UTC().Format(time.RFC3339)+" downloading %s from %s\n", key, src)
		if err := m.copyLocation(ctx, src, dst, 0o644); err != nil {
			return st, err
		}
		if err := verifySHA256(dst, artifact.SHA256); err != nil {
			return st, err
		}
		if artifact.Size > 0 {
			info, err := os.Stat(dst)
			if err != nil {
				return st, err
			}
			if info.Size() != artifact.Size {
				return st, fmt.Errorf("size mismatch for %s: expected %d got %d", dstName, artifact.Size, info.Size())
			}
		}
	}
	if err := os.WriteFile(filepath.Join(artifactDir, "manifest.json"), append(rawManifest, '\n'), 0o644); err != nil {
		return st, err
	}
	if err := m.publishPXECompatibilityFiles(artifactDir); err != nil {
		return st, err
	}
	if err := m.publishCurrent(def.Name, buildID, artifactDir, logPath); err != nil {
		return st, err
	}
	artifactPublished = true
	_ = os.WriteFile(filepath.Join(buildDir, ".prebuilt"), []byte(manifestLocation+"\n"), 0o644)
	return Status{
		Name:        def.Name,
		Phase:       PhaseReady,
		ArtifactDir: artifactDir,
		LogPath:     logPath,
		KernelPath:  filepath.Join(artifactDir, "boot-kernel"),
		InitrdPath:  filepath.Join(artifactDir, "boot-initrd"),
		RootFSPath:  filepath.Join(artifactDir, "rootfs.squashfs"),
		UpdatedAt:   m.now(),
	}, nil
}

func validatePrebuiltManifest(def Definition, manifest prebuiltManifest) error {
	if manifest.SchemaVersion != "gomi.bootenv/v1" {
		return fmt.Errorf("unsupported boot environment manifest schema: %s", manifest.SchemaVersion)
	}
	if strings.TrimSpace(manifest.Name) != def.Name {
		return fmt.Errorf("boot environment manifest name mismatch: expected %s got %s", def.Name, manifest.Name)
	}
	for _, key := range []string{"kernel", "initrd", "rootfs"} {
		artifact, ok := manifest.Artifacts[key]
		if !ok {
			return fmt.Errorf("boot environment manifest missing artifact %q", key)
		}
		if strings.TrimSpace(artifact.Path) == "" && strings.TrimSpace(artifact.URL) == "" {
			return fmt.Errorf("boot environment artifact %q requires path or url", key)
		}
		if strings.TrimSpace(artifact.SHA256) == "" {
			return fmt.Errorf("boot environment artifact %q requires sha256", key)
		}
	}
	return nil
}

func resolvePrebuiltManifestLocation(raw string) (manifestLocation, baseLocation string) {
	raw = strings.TrimSpace(raw)
	if isHTTPLocation(raw) {
		trimmed := strings.TrimRight(raw, "/")
		if strings.HasSuffix(trimmed, ".json") {
			return trimmed, trimmed[:strings.LastIndex(trimmed, "/")]
		}
		return trimmed + "/manifest.json", trimmed
	}
	path := localPathFromLocation(raw)
	if strings.HasSuffix(path, ".json") {
		return path, filepath.Dir(path)
	}
	return filepath.Join(path, "manifest.json"), path
}

func resolvePrebuiltArtifactLocation(base string, artifact prebuiltArtifact) string {
	if strings.TrimSpace(artifact.URL) != "" {
		return strings.TrimSpace(artifact.URL)
	}
	path := strings.TrimSpace(artifact.Path)
	if isHTTPLocation(base) {
		return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(path, "/")
	}
	return filepath.Join(base, path)
}

func isHTTPLocation(raw string) bool {
	return strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://")
}

func localPathFromLocation(raw string) string {
	if strings.HasPrefix(raw, "file://") {
		u, err := url.Parse(raw)
		if err == nil {
			return u.Path
		}
	}
	return raw
}

func (m *Manager) readLocation(ctx context.Context, raw string) ([]byte, error) {
	if isHTTPLocation(raw) {
		return fetchBytes(ctx, m.httpClient, raw)
	}
	return os.ReadFile(localPathFromLocation(raw))
}

func (m *Manager) copyLocation(ctx context.Context, src, dst string, mode os.FileMode) error {
	if isHTTPLocation(src) {
		if err := downloadFile(ctx, m.httpClient, src, dst); err != nil {
			return err
		}
		return os.Chmod(dst, mode)
	}
	return copyFile(localPathFromLocation(src), dst, mode)
}

func (m *Manager) publishCurrent(name, buildID, artifactDir, logPath string) error {
	current := filepath.Join(m.dataDir, "artifacts", name, "current")
	tmp := current + ".tmp"
	_ = os.Remove(tmp)
	if err := os.Symlink(buildID, tmp); err != nil {
		return err
	}
	defer os.Remove(tmp)
	if err := os.Rename(tmp, current); err != nil {
		return err
	}
	return m.writeStatusFile(name, artifactDir, logPath)
}

func (m *Manager) publishPXECompatibilityFiles(artifactDir string) error {
	linuxDir := filepath.Join(m.filesDir, "linux")
	if err := os.MkdirAll(linuxDir, 0o755); err != nil {
		return err
	}
	for _, name := range []string{"boot-kernel", "boot-initrd", "rootfs.squashfs"} {
		dst := filepath.Join(linuxDir, name)
		tmp := dst + ".tmp"
		_ = os.Remove(tmp)
		if err := os.Symlink(filepath.Join(artifactDir, name), tmp); err != nil {
			return err
		}
		defer os.Remove(tmp)
		if err := os.Rename(tmp, dst); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) currentArtifactDir(name string) (string, bool) {
	current := filepath.Join(m.dataDir, "artifacts", name, "current")
	resolved, err := filepath.EvalSymlinks(current)
	if err != nil {
		return "", false
	}
	for _, file := range []string{"boot-kernel", "boot-initrd", "rootfs.squashfs"} {
		if st, err := os.Stat(filepath.Join(resolved, file)); err != nil || st.IsDir() || st.Size() == 0 {
			return "", false
		}
	}
	return resolved, true
}

func (m *Manager) writeStatusFile(name, artifactDir, logPath string) error {
	st := Status{
		Name:        name,
		Phase:       PhaseReady,
		ArtifactDir: artifactDir,
		LogPath:     logPath,
		KernelPath:  filepath.Join(artifactDir, "boot-kernel"),
		InitrdPath:  filepath.Join(artifactDir, "boot-initrd"),
		RootFSPath:  filepath.Join(artifactDir, "rootfs.squashfs"),
		UpdatedAt:   m.now(),
	}
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(artifactDir, "status.json"), append(raw, '\n'), 0o644)
}

func readSavedStatus(path, expectedName string) (Status, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Status{}, false
	}
	var st Status
	if err := json.Unmarshal(raw, &st); err != nil {
		return Status{}, false
	}
	if st.Name != expectedName {
		return Status{}, false
	}
	return st, true
}

func removeAllWritable(path string) {
	_ = filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			_ = os.Chmod(p, 0o755)
		}
		return nil
	})
	_ = os.RemoveAll(path)
}

func verifySHA256(path, expected string) error {
	expected = strings.TrimSpace(strings.TrimPrefix(expected, "sha256:"))
	if expected == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	actual := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("checksum mismatch for %s: expected %s got %s", path, expected, actual)
	}
	return nil
}

func downloadFile(ctx context.Context, client *http.Client, rawURL, dst string) error {
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		err := downloadFileOnce(ctx, client, rawURL, dst)
		if err == nil {
			return nil
		}
		lastErr = err
		if ctx.Err() != nil {
			break
		}
		time.Sleep(time.Duration(attempt) * time.Second)
	}
	return lastErr
}

func fetchBytes(ctx context.Context, client *http.Client, rawURL string) ([]byte, error) {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", rawURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("fetch %s: status %d", rawURL, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func downloadFileOnce(ctx context.Context, client *http.Client, rawURL, dst string) error {
	if client == nil {
		client = http.DefaultClient
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
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
		io.Copy(io.Discard, resp.Body)
		return fmt.Errorf("download %s: status %d", rawURL, resp.StatusCode)
	}
	tmp := dst + ".download"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	defer os.Remove(tmp)
	if _, err := io.Copy(f, resp.Body); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
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
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer os.Remove(tmp)
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}
