package oscatalog

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/sugaf1204/gomi/internal/osimage"
	"gopkg.in/yaml.v3"
)

const defaultOSImageSourceURL = "https://github.com/sugaf1204/gomi/releases/latest/download"

//go:embed default-catalog.yaml
var defaultCatalogYAML []byte

type Entry struct {
	Name              string              `json:"name" yaml:"name"`
	OSFamily          string              `json:"osFamily" yaml:"osFamily"`
	OSVersion         string              `json:"osVersion" yaml:"osVersion"`
	Arch              string              `json:"arch" yaml:"arch"`
	Format            osimage.ImageFormat `json:"format" yaml:"format"`
	SourceFormat      osimage.ImageFormat `json:"sourceFormat,omitempty" yaml:"sourceFormat,omitempty"`
	SourceCompression string              `json:"sourceCompression,omitempty" yaml:"sourceCompression,omitempty"`
	Variant           osimage.Variant     `json:"variant,omitempty" yaml:"variant,omitempty"`
	URL               string              `json:"url" yaml:"url"`
	Checksum          string              `json:"checksum,omitempty" yaml:"checksum,omitempty"`
	Description       string              `json:"description,omitempty" yaml:"description,omitempty"`
	BootEnvironment   string              `json:"bootEnvironment" yaml:"bootEnvironment"`
	Build             *BuildRecipe        `json:"build,omitempty" yaml:"build,omitempty"`
}

type BuildRecipe struct {
	Type                string      `json:"type" yaml:"type"`
	Source              BuildSource `json:"source" yaml:"source"`
	AptPackages         []string    `json:"aptPackages,omitempty" yaml:"aptPackages,omitempty"`
	CurtinKernelPackage string      `json:"curtinKernelPackage,omitempty" yaml:"curtinKernelPackage,omitempty"`
	PackerTemplate      string      `json:"packerTemplate,omitempty" yaml:"packerTemplate,omitempty"`
	DiskSize            string      `json:"diskSize,omitempty" yaml:"diskSize,omitempty"`
}

type BuildSource struct {
	URL      string              `json:"url" yaml:"url"`
	Checksum string              `json:"checksum" yaml:"checksum"`
	Format   osimage.ImageFormat `json:"format" yaml:"format"`
}

type LoadOptions struct {
	SourceBase      string
	CatalogFile     string
	CatalogURL      string
	ReplaceExternal bool
}

func (e Entry) OSImage() osimage.OSImage {
	return osimage.OSImage{
		Name:        e.Name,
		OSFamily:    e.OSFamily,
		OSVersion:   e.OSVersion,
		Arch:        e.Arch,
		Format:      e.Format,
		Source:      osimage.SourceURL,
		Variant:     e.Variant,
		URL:         e.URL,
		Checksum:    e.Checksum,
		Description: e.Description,
	}
}

func List() []Entry {
	out, _ := ListWithContext(context.Background())
	return out
}

func ListWithContext(ctx context.Context) ([]Entry, error) {
	return Load(ctx, EnvOptions())
}

func Get(name string) (Entry, bool) {
	entry, ok, _ := GetWithContext(context.Background(), name)
	return entry, ok
}

func GetWithContext(ctx context.Context, name string) (Entry, bool, error) {
	name = strings.TrimSpace(name)
	entries, err := ListWithContext(ctx)
	if err != nil {
		return Entry{}, false, err
	}
	for _, entry := range entries {
		if entry.Name == name {
			return entry, true, nil
		}
	}
	return Entry{}, false, nil
}

func EnvOptions() LoadOptions {
	return LoadOptions{
		SourceBase:      os.Getenv("GOMI_OS_IMAGE_SOURCE_URL"),
		CatalogFile:     os.Getenv("GOMI_OS_CATALOG_FILE"),
		CatalogURL:      os.Getenv("GOMI_OS_CATALOG_URL"),
		ReplaceExternal: truthy(os.Getenv("GOMI_OS_CATALOG_REPLACE")),
	}
}

func Load(ctx context.Context, opts LoadOptions) ([]Entry, error) {
	var entries []Entry
	hasExternal := strings.TrimSpace(opts.CatalogFile) != "" || strings.TrimSpace(opts.CatalogURL) != ""
	if !opts.ReplaceExternal || !hasExternal {
		defaultEntries, err := Parse(defaultCatalogYAML)
		if err != nil {
			return nil, err
		}
		entries = append(entries, defaultEntries...)
	}
	if path := strings.TrimSpace(opts.CatalogFile); path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read OS catalog file: %w", err)
		}
		more, err := Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("parse OS catalog file: %w", err)
		}
		entries = mergeEntries(entries, more)
	}
	if rawURL := strings.TrimSpace(opts.CatalogURL); rawURL != "" {
		raw, err := fetchCatalog(ctx, rawURL)
		if err != nil {
			return nil, err
		}
		more, err := Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("parse OS catalog URL: %w", err)
		}
		entries = mergeEntries(entries, more)
	}
	return materializeEntries(entries, opts.SourceBase)
}

func Parse(raw []byte) ([]Entry, error) {
	var doc struct {
		Entries []Entry `yaml:"entries"`
	}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&doc); err == nil && len(doc.Entries) > 0 {
		return doc.Entries, nil
	}

	var list []Entry
	dec = yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&list); err == nil && len(list) > 0 {
		return list, nil
	}

	var single Entry
	dec = yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&single); err != nil {
		return nil, err
	}
	if strings.TrimSpace(single.Name) == "" {
		return nil, errors.New("OS catalog has no entries")
	}
	return []Entry{single}, nil
}

func Validate(entry Entry) error {
	if strings.TrimSpace(entry.Name) == "" {
		return errors.New("name is required")
	}
	if strings.TrimSpace(entry.OSFamily) == "" {
		return fmt.Errorf("%s: osFamily is required", entry.Name)
	}
	if strings.TrimSpace(entry.OSVersion) == "" {
		return fmt.Errorf("%s: osVersion is required", entry.Name)
	}
	if strings.TrimSpace(entry.Arch) == "" {
		return fmt.Errorf("%s: arch is required", entry.Name)
	}
	if entry.Format != osimage.FormatRAW {
		return fmt.Errorf("%s: unsupported format %q; only raw catalog images are supported", entry.Name, entry.Format)
	}
	sourceFormat := entry.SourceFormat
	if sourceFormat == "" {
		sourceFormat = entry.Format
	}
	if sourceFormat != osimage.FormatRAW {
		return fmt.Errorf("%s: unsupported sourceFormat %q; only raw catalog sources are supported", entry.Name, sourceFormat)
	}
	if strings.TrimSpace(entry.URL) == "" {
		return fmt.Errorf("%s: url is required", entry.Name)
	}
	if compression := strings.TrimSpace(entry.SourceCompression); compression != "" && compression != "zstd" {
		return fmt.Errorf("%s: unsupported sourceCompression %q", entry.Name, compression)
	}
	if strings.TrimSpace(entry.BootEnvironment) == "" {
		return fmt.Errorf("%s: bootEnvironment is required", entry.Name)
	}
	if entry.Build != nil {
		if entry.Build.Type != "packer-qemu-cloud-image" {
			return fmt.Errorf("%s: unsupported build.type %q", entry.Name, entry.Build.Type)
		}
		if strings.TrimSpace(entry.Build.Source.URL) == "" {
			return fmt.Errorf("%s: build.source.url is required", entry.Name)
		}
		if strings.TrimSpace(entry.Build.Source.Checksum) == "" {
			return fmt.Errorf("%s: build.source.checksum is required", entry.Name)
		}
		if entry.Build.Source.Format == "" {
			return fmt.Errorf("%s: build.source.format is required", entry.Name)
		}
	}
	return nil
}

func BuildEntries(entries []Entry) []Entry {
	out := make([]Entry, 0)
	for _, entry := range entries {
		if entry.Build != nil {
			out = append(out, entry)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func materializeEntries(entries []Entry, sourceBase string) ([]Entry, error) {
	base := strings.TrimRight(strings.TrimSpace(sourceBase), "/")
	if base == "" {
		base = defaultOSImageSourceURL
	}
	out := make([]Entry, 0, len(entries))
	seen := map[string]struct{}{}
	for _, entry := range entries {
		entry.Name = strings.TrimSpace(entry.Name)
		if _, ok := seen[entry.Name]; ok {
			return nil, fmt.Errorf("duplicate OS catalog entry: %s", entry.Name)
		}
		seen[entry.Name] = struct{}{}
		entry.URL = resolveArtifactURL(base, entry.URL)
		if err := Validate(entry); err != nil {
			return nil, err
		}
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func resolveArtifactURL(base, artifact string) string {
	artifact = strings.TrimSpace(artifact)
	if strings.HasPrefix(artifact, "http://") || strings.HasPrefix(artifact, "https://") {
		return artifact
	}
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(artifact, "/")
}

func mergeEntries(base, overlay []Entry) []Entry {
	out := append([]Entry{}, base...)
	index := map[string]int{}
	for i, entry := range out {
		index[strings.TrimSpace(entry.Name)] = i
	}
	for _, entry := range overlay {
		name := strings.TrimSpace(entry.Name)
		if i, ok := index[name]; ok {
			out[i] = entry
			continue
		}
		index[name] = len(out)
		out = append(out, entry)
	}
	return out
}

func fetchCatalog(ctx context.Context, rawURL string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download OS catalog URL: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("download OS catalog URL: status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func truthy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "t", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}
