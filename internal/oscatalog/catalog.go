package oscatalog

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/sugaf1204/gomi/internal/osimage"
	"gopkg.in/yaml.v3"
)

const defaultOSImageSourceURL = "https://github.com/sugaf1204/gomi/releases/latest/download"

//go:embed default-catalog.yaml
var defaultCatalogYAML []byte

//go:embed catalog.schema.json
var catalogSchemaJSON []byte

var (
	catalogSchemaOnce sync.Once
	catalogSchema     *jsonschema.Resolved
	catalogSchemaErr  error
)

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
}

type LoadOptions struct {
	SourceBase      string
	CatalogFile     string
	CatalogURL      string
	ReplaceExternal bool
}

func (e Entry) OSImage() osimage.OSImage {
	img := osimage.OSImage{
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
	if e.Format == osimage.FormatSquashFS {
		img.Manifest = &osimage.Manifest{
			SchemaVersion: "gomi.osimage.v1alpha1",
			Name:          e.Name,
			OSFamily:      e.OSFamily,
			OSVersion:     e.OSVersion,
			Arch:          e.Arch,
			Root: osimage.RootArtifact{
				Format: e.Format,
				Path:   "rootfs.squashfs",
			},
		}
	}
	return img
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
	doc, err := decodeYAMLDocument(raw)
	if err != nil {
		return nil, err
	}
	if err := validateCatalogDocument(doc); err != nil {
		return nil, err
	}
	jsonRaw, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	switch typed := doc.(type) {
	case []any:
		var entries []Entry
		if err := json.Unmarshal(jsonRaw, &entries); err != nil {
			return nil, err
		}
		return entries, validateUniqueNames(entries)
	case map[string]any:
		if _, ok := typed["entries"]; ok {
			var wrapper struct {
				Entries []Entry `json:"entries"`
			}
			if err := json.Unmarshal(jsonRaw, &wrapper); err != nil {
				return nil, err
			}
			return wrapper.Entries, validateUniqueNames(wrapper.Entries)
		}
		var single Entry
		if err := json.Unmarshal(jsonRaw, &single); err != nil {
			return nil, err
		}
		return []Entry{single}, nil
	default:
		return nil, errors.New("OS catalog must be an entry, entry list, or entries object")
	}
}

func Validate(entry Entry) error {
	doc, err := toJSONDocument(entry)
	if err != nil {
		return err
	}
	return validateCatalogDocument(doc)
}

func materializeEntries(entries []Entry, sourceBase string) ([]Entry, error) {
	base := strings.TrimRight(strings.TrimSpace(sourceBase), "/")
	if base == "" {
		base = defaultOSImageSourceURL
	}
	out := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		entry.Name = strings.TrimSpace(entry.Name)
		entry.URL = resolveArtifactURL(base, entry.URL)
		out = append(out, entry)
	}
	if err := validateCatalogSemantics(out); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

type semanticValidator func([]Entry) error

var semanticValidators = []semanticValidator{
	validateUniqueNames,
	validateResolvedArtifactURLs,
}

func validateCatalogSemantics(entries []Entry) error {
	for _, validate := range semanticValidators {
		if err := validate(entries); err != nil {
			return err
		}
	}
	return nil
}

func validateUniqueNames(entries []Entry) error {
	seen := map[string]struct{}{}
	for _, entry := range entries {
		if _, ok := seen[entry.Name]; ok {
			return fmt.Errorf("duplicate OS catalog entry: %s", entry.Name)
		}
		seen[entry.Name] = struct{}{}
	}
	return nil
}

func validateResolvedArtifactURLs(entries []Entry) error {
	for _, entry := range entries {
		if !strings.HasPrefix(entry.URL, "http://") && !strings.HasPrefix(entry.URL, "https://") {
			return fmt.Errorf("%s: resolved url must be http or https: %s", entry.Name, entry.URL)
		}
	}
	return nil
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

func decodeYAMLDocument(raw []byte) (any, error) {
	var doc any
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(&doc); err != nil {
		return nil, err
	}
	return normalizeYAML(doc), nil
}

func normalizeYAML(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for k, v := range typed {
			out[k] = normalizeYAML(v)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(typed))
		for k, v := range typed {
			out[fmt.Sprint(k)] = normalizeYAML(v)
		}
		return out
	case []any:
		for i, v := range typed {
			typed[i] = normalizeYAML(v)
		}
		return typed
	default:
		return typed
	}
}

func toJSONDocument(value any) (any, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	return doc, nil
}

func validateCatalogDocument(doc any) error {
	schema, err := getCatalogSchema()
	if err != nil {
		return err
	}
	if err := schema.Validate(doc); err != nil {
		return fmt.Errorf("validate OS catalog schema: %w", err)
	}
	return nil
}

func getCatalogSchema() (*jsonschema.Resolved, error) {
	catalogSchemaOnce.Do(func() {
		var schema jsonschema.Schema
		if err := json.Unmarshal(catalogSchemaJSON, &schema); err != nil {
			catalogSchemaErr = err
			return
		}
		catalogSchema, catalogSchemaErr = schema.Resolve(&jsonschema.ResolveOptions{
			BaseURI: "https://gomi.invalid/schemas/os-catalog.schema.json",
		})
	})
	return catalogSchema, catalogSchemaErr
}

func truthy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "t", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}
