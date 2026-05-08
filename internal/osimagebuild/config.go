package osimagebuild

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/sugaf1204/gomi/internal/oscatalog"
	"github.com/sugaf1204/gomi/internal/osimage"
	"gopkg.in/yaml.v3"
)

const SystemBuildConfigPath = "/usr/share/gomi/osimage/builds.yaml"

//go:embed default-builds.yaml
var defaultBuildConfigYAML []byte

type LoadOptions = oscatalog.LoadOptions

type Config struct {
	Entries []BuildEntry `yaml:"entries" json:"entries"`
}

type BuildEntry struct {
	Name           string   `yaml:"name" json:"name"`
	Source         Source   `yaml:"source" json:"source"`
	PackageManager string   `yaml:"packageManager" json:"packageManager"`
	Packages       []string `yaml:"packages" json:"packages"`
	VerifyModules  []string `yaml:"verifyModules,omitempty" json:"verifyModules,omitempty"`
	CleanupPaths   []string `yaml:"cleanupPaths,omitempty" json:"cleanupPaths,omitempty"`
	CleanupGlobs   []string `yaml:"cleanupGlobs,omitempty" json:"cleanupGlobs,omitempty"`
	SquashFS       SquashFS `yaml:"squashfs,omitempty" json:"squashfs,omitempty"`
}

type Source struct {
	URL         string `yaml:"url" json:"url"`
	Checksum    string `yaml:"checksum" json:"checksum"`
	Format      string `yaml:"format" json:"format"`
	Compression string `yaml:"compression,omitempty" json:"compression,omitempty"`
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
	raw, err := loadConfigBytes(path)
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
	return cfg, nil
}

func loadConfigBytes(path string) ([]byte, error) {
	path = strings.TrimSpace(path)
	if path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read OS image build config: %w", err)
		}
		return raw, nil
	}
	if raw, err := os.ReadFile(SystemBuildConfigPath); err == nil {
		return raw, nil
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read system OS image build config: %w", err)
	}
	return defaultBuildConfigYAML, nil
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
		if strings.TrimSpace(entry.Source.URL) == "" {
			return fmt.Errorf("%s: source.url is required", name)
		}
		if strings.TrimSpace(entry.Source.Format) == "" {
			return fmt.Errorf("%s: source.format is required", name)
		}
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
