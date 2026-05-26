package bootenv

import (
	"context"
	"fmt"
	"net/http"
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
