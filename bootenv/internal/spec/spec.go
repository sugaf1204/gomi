package spec

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	APIVersion = "gomi.sugaf1204.github.io/ephemeral/v1alpha1"
	Kind       = "BootEnvironment"
)

var namePattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

type Document struct {
	APIVersion string   `yaml:"apiVersion"`
	Kind       string   `yaml:"kind"`
	Metadata   Metadata `yaml:"metadata"`
	Spec       Spec     `yaml:"spec"`
}

type Metadata struct {
	Name string `yaml:"name"`
}

type Spec struct {
	Architecture string       `yaml:"architecture,omitempty"`
	Kernel       Kernel       `yaml:"kernel"`
	Initramfs    Initramfs    `yaml:"initramfs"`
	RootFS       RootFS       `yaml:"rootfs"`
	Cmdline      []string     `yaml:"cmdline,omitempty"`
	Network      Network      `yaml:"network,omitempty"`
	ControlPlane ControlPlane `yaml:"controlPlane,omitempty"`
	PXE          PXE          `yaml:"pxe,omitempty"`
}

type Network struct {
	DHCP DHCP `yaml:"dhcp,omitempty"`
}

type DHCP struct {
	Enabled bool     `yaml:"enabled,omitempty"`
	Image   string   `yaml:"image,omitempty"`
	Command []string `yaml:"command,omitempty"`
}

type ControlPlane struct {
	MetadataURL string `yaml:"metadataURL,omitempty"`
	WorkflowURL string `yaml:"workflowURL,omitempty"`
}

type PXE struct {
	BaseURL    string   `yaml:"baseURL,omitempty"`
	ScriptName string   `yaml:"scriptName,omitempty"`
	KernelPath string   `yaml:"kernelPath,omitempty"`
	InitrdPath string   `yaml:"initrdPath,omitempty"`
	RootFSPath string   `yaml:"rootfsPath,omitempty"`
	ExtraArgs  []string `yaml:"extraArgs,omitempty"`
}

type Kernel struct {
	Package string `yaml:"package,omitempty"`
	Path    string `yaml:"path,omitempty"`
}

type Initramfs struct {
	Backend  string   `yaml:"backend,omitempty"`
	Packages []string `yaml:"packages,omitempty"`
	Hooks    []Hook   `yaml:"hooks,omitempty"`
	Cmdline  []string `yaml:"cmdline,omitempty"`
}

type RootFS struct {
	Type     string       `yaml:"type,omitempty"`
	Source   RootFSSource `yaml:"source"`
	Packages []string     `yaml:"packages,omitempty"`
	Files    []File       `yaml:"files,omitempty"`
	Services []Service    `yaml:"services,omitempty"`
	Build    SquashFS     `yaml:"build,omitempty"`
}

type RootFSSource struct {
	Type   string `yaml:"type,omitempty"`
	URL    string `yaml:"url,omitempty"`
	Path   string `yaml:"path,omitempty"`
	SHA256 string `yaml:"sha256,omitempty"`
}

type SquashFS struct {
	Compression string `yaml:"compression,omitempty"`
	BlockSize   string `yaml:"blockSize,omitempty"`
	Output      string `yaml:"output,omitempty"`
}

type File struct {
	Path     string `yaml:"path"`
	Mode     string `yaml:"mode,omitempty"`
	Contents string `yaml:"contents,omitempty"`
}

type Service struct {
	Name    string   `yaml:"name"`
	Enable  bool     `yaml:"enable,omitempty"`
	Command []string `yaml:"command,omitempty"`
}

type Hook struct {
	Name    string `yaml:"name"`
	Package string `yaml:"package,omitempty"`
}

func Load(path string) (Document, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Document{}, err
	}
	var doc Document
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return Document{}, fmt.Errorf("parse %s: %w", path, err)
	}
	doc.ApplyDefaults()
	if err := doc.Validate(); err != nil {
		return Document{}, err
	}
	return doc, nil
}

func (d *Document) ApplyDefaults() {
	if d.Spec.Architecture == "" {
		d.Spec.Architecture = "amd64"
	}
	if d.Spec.Initramfs.Backend == "" {
		d.Spec.Initramfs.Backend = "initramfs-tools"
	}
	if d.Spec.RootFS.Type == "" {
		d.Spec.RootFS.Type = "squashfs"
	}
	if d.Spec.RootFS.Build.Compression == "" {
		d.Spec.RootFS.Build.Compression = "zstd"
	}
	if d.Spec.RootFS.Build.BlockSize == "" {
		d.Spec.RootFS.Build.BlockSize = "1M"
	}
	if d.Spec.RootFS.Build.Output == "" && d.Metadata.Name != "" {
		d.Spec.RootFS.Build.Output = d.Metadata.Name + ".rootfs.squashfs"
	}
	if d.Spec.Network.DHCP.Enabled && d.Spec.Network.DHCP.Image == "" {
		d.Spec.Network.DHCP.Image = "systemd-networkd"
	}
	if d.Spec.PXE.ScriptName == "" {
		d.Spec.PXE.ScriptName = "boot.ipxe"
	}
	if d.Spec.PXE.KernelPath == "" && d.Metadata.Name != "" {
		d.Spec.PXE.KernelPath = d.Metadata.Name + ".kernel"
	}
	if d.Spec.PXE.InitrdPath == "" && d.Metadata.Name != "" {
		d.Spec.PXE.InitrdPath = d.Metadata.Name + ".initrd.img"
	}
	if d.Spec.PXE.RootFSPath == "" {
		d.Spec.PXE.RootFSPath = d.Spec.RootFS.Build.Output
	}
}

func (d Document) Validate() error {
	var problems []string
	if d.APIVersion != APIVersion {
		problems = append(problems, fmt.Sprintf("apiVersion must be %q", APIVersion))
	}
	if d.Kind != Kind {
		problems = append(problems, fmt.Sprintf("kind must be %q", Kind))
	}
	if !namePattern.MatchString(d.Metadata.Name) {
		problems = append(problems, "metadata.name must be a lowercase DNS-style name")
	}
	if d.Spec.Kernel.Package == "" && d.Spec.Kernel.Path == "" {
		problems = append(problems, "spec.kernel.package or spec.kernel.path is required")
	}
	switch d.Spec.Initramfs.Backend {
	case "initramfs-tools", "dracut":
	default:
		problems = append(problems, "spec.initramfs.backend must be initramfs-tools or dracut")
	}
	if d.Spec.RootFS.Type != "squashfs" {
		problems = append(problems, "spec.rootfs.type must be squashfs")
	}
	if d.Spec.RootFS.Source.Type == "" {
		problems = append(problems, "spec.rootfs.source.type is required")
	}
	switch d.Spec.RootFS.Source.Type {
	case "directory":
		if d.Spec.RootFS.Source.Path == "" {
			problems = append(problems, "spec.rootfs.source.path is required for directory source")
		}
	case "debian-live-iso", "ubuntu-cloud-squashfs":
		if d.Spec.RootFS.Source.URL == "" {
			problems = append(problems, fmt.Sprintf("spec.rootfs.source.url is required for %s source", d.Spec.RootFS.Source.Type))
		}
	default:
		problems = append(problems, "spec.rootfs.source.type must be directory, debian-live-iso, or ubuntu-cloud-squashfs")
	}
	if d.Spec.PXE.BaseURL == "" {
		problems = append(problems, "spec.pxe.baseURL is required")
	}
	seen := map[string]struct{}{}
	for _, service := range d.Spec.RootFS.Services {
		if service.Name == "" {
			problems = append(problems, "rootfs service name is required")
			continue
		}
		if !namePattern.MatchString(service.Name) {
			problems = append(problems, fmt.Sprintf("rootfs service %q must use a lowercase DNS-style name", service.Name))
		}
		if _, ok := seen[service.Name]; ok {
			problems = append(problems, fmt.Sprintf("rootfs service %q is duplicated", service.Name))
		}
		seen[service.Name] = struct{}{}
	}
	for _, file := range d.Spec.RootFS.Files {
		if file.Path == "" {
			problems = append(problems, "rootfs file path is required")
		}
	}
	if len(problems) > 0 {
		return errors.New(joinProblems(problems))
	}
	return nil
}

func EmbeddedYAML(d Document) (string, error) {
	var buf strings.Builder
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(d); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func joinProblems(problems []string) string {
	out := "invalid spec:"
	for _, problem := range problems {
		out += "\n- " + problem
	}
	return out
}
