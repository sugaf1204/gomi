package oscatalog

import (
	"os"
	"sort"
	"strings"

	"github.com/sugaf1204/gomi/internal/osimage"
)

const defaultOSImageSourceURL = "https://github.com/sugaf1204/gomi/releases/latest/download"

type Entry struct {
	Name              string              `json:"name"`
	OSFamily          string              `json:"osFamily"`
	OSVersion         string              `json:"osVersion"`
	Arch              string              `json:"arch"`
	Format            osimage.ImageFormat `json:"format"`
	SourceFormat      osimage.ImageFormat `json:"sourceFormat,omitempty"`
	SourceCompression string              `json:"sourceCompression,omitempty"`
	Variant           osimage.Variant     `json:"variant,omitempty"`
	URL               string              `json:"url"`
	Checksum          string              `json:"checksum,omitempty"`
	Description       string              `json:"description,omitempty"`
	BootEnvironment   string              `json:"bootEnvironment"`
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

var entryTemplates = []Entry{
	{
		Name:              "debian-13-amd64-cloud",
		OSFamily:          "debian",
		OSVersion:         "13",
		Arch:              "amd64",
		Format:            osimage.FormatRAW,
		SourceFormat:      osimage.FormatRAW,
		SourceCompression: "zstd",
		Variant:           osimage.VariantCloud,
		URL:               "debian-13-amd64-cloud.raw.zst",
		Description:       "Debian 13 amd64 cloud raw image",
		BootEnvironment:   "ubuntu-minimal-cloud-amd64",
	},
	{
		Name:              "debian-13-amd64-baremetal",
		OSFamily:          "debian",
		OSVersion:         "13",
		Arch:              "amd64",
		Format:            osimage.FormatRAW,
		SourceFormat:      osimage.FormatRAW,
		SourceCompression: "zstd",
		Variant:           osimage.VariantBareMetal,
		URL:               "debian-13-amd64-baremetal.raw.zst",
		Description:       "Debian 13 amd64 bare-metal raw image with expanded kernel modules",
		BootEnvironment:   "ubuntu-minimal-cloud-amd64",
	},
	{
		Name:              "ubuntu-22.04-amd64-cloud",
		OSFamily:          "ubuntu",
		OSVersion:         "22.04",
		Arch:              "amd64",
		Format:            osimage.FormatRAW,
		SourceFormat:      osimage.FormatRAW,
		SourceCompression: "zstd",
		Variant:           osimage.VariantCloud,
		URL:               "ubuntu-22.04-amd64-cloud.raw.zst",
		Description:       "Ubuntu 22.04 LTS amd64 cloud raw image",
		BootEnvironment:   "ubuntu-minimal-cloud-amd64",
	},
	{
		Name:              "ubuntu-22.04-amd64-baremetal",
		OSFamily:          "ubuntu",
		OSVersion:         "22.04",
		Arch:              "amd64",
		Format:            osimage.FormatRAW,
		SourceFormat:      osimage.FormatRAW,
		SourceCompression: "zstd",
		Variant:           osimage.VariantBareMetal,
		URL:               "ubuntu-22.04-amd64-baremetal.raw.zst",
		Description:       "Ubuntu 22.04 LTS amd64 bare-metal raw image with generic kernel modules",
		BootEnvironment:   "ubuntu-minimal-cloud-amd64",
	},
	{
		Name:              "ubuntu-24.04-amd64-cloud",
		OSFamily:          "ubuntu",
		OSVersion:         "24.04",
		Arch:              "amd64",
		Format:            osimage.FormatRAW,
		SourceFormat:      osimage.FormatRAW,
		SourceCompression: "zstd",
		Variant:           osimage.VariantCloud,
		URL:               "ubuntu-24.04-amd64-cloud.raw.zst",
		Description:       "Ubuntu 24.04 LTS amd64 cloud raw image",
		BootEnvironment:   "ubuntu-minimal-cloud-amd64",
	},
	{
		Name:              "ubuntu-24.04-amd64-baremetal",
		OSFamily:          "ubuntu",
		OSVersion:         "24.04",
		Arch:              "amd64",
		Format:            osimage.FormatRAW,
		SourceFormat:      osimage.FormatRAW,
		SourceCompression: "zstd",
		Variant:           osimage.VariantBareMetal,
		URL:               "ubuntu-24.04-amd64-baremetal.raw.zst",
		Description:       "Ubuntu 24.04 LTS amd64 bare-metal raw image with generic kernel modules",
		BootEnvironment:   "ubuntu-minimal-cloud-amd64",
	},
}

func List() []Entry {
	out := materializeEntries()
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func Get(name string) (Entry, bool) {
	name = strings.TrimSpace(name)
	for _, entry := range materializeEntries() {
		if entry.Name == name {
			return entry, true
		}
	}
	return Entry{}, false
}

func materializeEntries() []Entry {
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("GOMI_OS_IMAGE_SOURCE_URL")), "/")
	if base == "" {
		base = defaultOSImageSourceURL
	}
	out := make([]Entry, 0, len(entryTemplates))
	for _, entry := range entryTemplates {
		entry.URL = resolveArtifactURL(base, entry.URL)
		out = append(out, entry)
	}
	return out
}

func resolveArtifactURL(base, artifact string) string {
	artifact = strings.TrimSpace(artifact)
	if strings.HasPrefix(artifact, "http://") || strings.HasPrefix(artifact, "https://") {
		return artifact
	}
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(artifact, "/")
}
