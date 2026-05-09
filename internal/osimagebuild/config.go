package osimagebuild

import (
	"context"
	"embed"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sugaf1204/gomi/internal/oscatalog"
	"github.com/sugaf1204/gomi/internal/osimage"
	"gopkg.in/yaml.v3"
)

const SystemBuildConfigPath = "/usr/share/gomi/osimage/builds.yaml"

//go:embed default-builds.yaml
var defaultBuildConfigYAML []byte

//go:embed definitions/*.yaml
var embeddedDefinitions embed.FS

type LoadOptions = oscatalog.LoadOptions

type Config struct {
	Entries []BuildEntry `yaml:"entries" json:"entries"`

	baseDir                string
	useEmbeddedDefinitions bool
}

type BuildEntry struct {
	Name       string   `yaml:"name" json:"name"`
	Backend    string   `yaml:"backend,omitempty" json:"backend,omitempty"`
	Definition string   `yaml:"definition" json:"definition"`
	SquashFS   SquashFS `yaml:"squashfs,omitempty" json:"squashfs,omitempty"`
}

type SquashFS struct {
	Compression string `yaml:"compression,omitempty" json:"compression,omitempty"`
	BlockSize   string `yaml:"blockSize,omitempty" json:"blockSize,omitempty"`
}

type Matrix struct {
	Include []MatrixEntry `json:"include"`
}

type MatrixEntry struct {
	Name string `json:"name"`
}

func LoadCatalog(ctx context.Context, opts LoadOptions) ([]oscatalog.Entry, error) {
	return oscatalog.Load(ctx, opts)
}

func LoadConfig(path string) (Config, error) {
	raw, baseDir, useEmbeddedDefinitions, err := loadConfigBytes(path)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse OS image build config: %w", err)
	}
	if err := validateConfig(cfg); err != nil {
		return Config{}, err
	}
	cfg.baseDir = baseDir
	cfg.useEmbeddedDefinitions = useEmbeddedDefinitions
	return cfg, nil
}

func loadConfigBytes(path string) ([]byte, string, bool, error) {
	path = strings.TrimSpace(path)
	if path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, "", false, fmt.Errorf("read OS image build config: %w", err)
		}
		return raw, filepath.Dir(path), false, nil
	}
	if raw, err := os.ReadFile(SystemBuildConfigPath); err == nil {
		return raw, filepath.Dir(SystemBuildConfigPath), false, nil
	} else if !os.IsNotExist(err) {
		return nil, "", false, fmt.Errorf("read system OS image build config: %w", err)
	}
	if root, err := findRepoRoot(); err == nil {
		repoPath := filepath.Join(root, "internal", "osimagebuild", "default-builds.yaml")
		if raw, err := os.ReadFile(repoPath); err == nil {
			return raw, filepath.Dir(repoPath), false, nil
		}
	}
	return defaultBuildConfigYAML, "", true, nil
}

func validateConfig(cfg Config) error {
	if len(cfg.Entries) == 0 {
		return fmt.Errorf("OS image build config must contain at least one entry")
	}
	seen := map[string]struct{}{}
	for _, entry := range cfg.Entries {
		name := strings.TrimSpace(entry.Name)
		if name == "" {
			return fmt.Errorf("OS image build config entry name is required")
		}
		if _, ok := seen[name]; ok {
			return fmt.Errorf("duplicate OS image build config entry: %s", name)
		}
		seen[name] = struct{}{}
		if err := validateBuildEntry(name, entry); err != nil {
			return err
		}
	}
	return nil
}

func validateBuildEntry(name string, entry BuildEntry) error {
	if strings.TrimSpace(entry.Definition) == "" {
		return fmt.Errorf("%s: definition is required", name)
	}
	switch normalizeBackend(entry.Backend) {
	case "distrobuilder":
	default:
		return fmt.Errorf("%s: unsupported backend: %s", name, entry.Backend)
	}
	return nil
}

func BuildMatrix(entries []oscatalog.Entry, cfg Config) (Matrix, error) {
	catalogNames := map[string]oscatalog.Entry{}
	for _, entry := range entries {
		catalogNames[entry.Name] = entry
	}
	names := make([]string, 0, len(cfg.Entries))
	for _, build := range cfg.Entries {
		entry, ok := catalogNames[build.Name]
		if !ok {
			return Matrix{}, fmt.Errorf("%s: build config entry has no runtime catalog entry", build.Name)
		}
		if entry.Format != osimage.FormatSquashFS {
			return Matrix{}, fmt.Errorf("%s: runtime catalog format must be squashfs for rootfs SquashFS builds", build.Name)
		}
		names = append(names, build.Name)
	}
	sort.Strings(names)
	matrix := Matrix{Include: make([]MatrixEntry, 0, len(names))}
	for _, name := range names {
		matrix.Include = append(matrix.Include, MatrixEntry{Name: name})
	}
	return matrix, nil
}

func findBuildEntry(entries []BuildEntry, name string) (BuildEntry, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return BuildEntry{}, fmt.Errorf("entry name is required")
	}
	for _, entry := range entries {
		if entry.Name == name {
			return entry, nil
		}
	}
	return BuildEntry{}, fmt.Errorf("build config entry not found: %s", name)
}

func normalizeBackend(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "distrobuilder"
	}
	return value
}
