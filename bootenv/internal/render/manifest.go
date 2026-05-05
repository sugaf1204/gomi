package render

import (
	"strings"

	"github.com/sugaf1204/gomi/bootenv/internal/spec"
	"gopkg.in/yaml.v3"
)

type Manifest struct {
	APIVersion        string     `yaml:"apiVersion"`
	Name              string     `yaml:"name"`
	Architecture      string     `yaml:"architecture"`
	InitramfsBackend  string     `yaml:"initramfsBackend"`
	RootFS            string     `yaml:"rootfs"`
	BuildPlan         string     `yaml:"buildPlan"`
	IPXEScript        string     `yaml:"ipxeScript"`
	Artifacts         []Artifact `yaml:"artifacts"`
	SuggestedCommands []string   `yaml:"suggestedCommands"`
}

type Artifact struct {
	Name string `yaml:"name"`
	Path string `yaml:"path"`
	URL  string `yaml:"url"`
}

func ManifestYAML(doc spec.Document) (string, error) {
	manifest := Manifest{
		APIVersion:       "gomi.sugaf1204.github.io/ephemeral/v1alpha1",
		Name:             doc.Metadata.Name,
		Architecture:     doc.Spec.Architecture,
		InitramfsBackend: doc.Spec.Initramfs.Backend,
		RootFS:           "squashfs",
		BuildPlan:        "build-plan.json",
		IPXEScript:       doc.Spec.PXE.ScriptName,
		Artifacts: []Artifact{
			{Name: "kernel", Path: doc.Spec.PXE.KernelPath, URL: doc.Spec.PXE.BaseURL + "/" + doc.Spec.PXE.KernelPath},
			{Name: "initrd", Path: doc.Spec.PXE.InitrdPath, URL: doc.Spec.PXE.BaseURL + "/" + doc.Spec.PXE.InitrdPath},
			{Name: "rootfs", Path: doc.Spec.PXE.RootFSPath, URL: doc.Spec.PXE.BaseURL + "/" + doc.Spec.PXE.RootFSPath},
			{Name: "ipxe", Path: doc.Spec.PXE.ScriptName, URL: doc.Spec.PXE.BaseURL + "/" + doc.Spec.PXE.ScriptName},
		},
		SuggestedCommands: []string{
			"materialize build-plan.json with the selected backend",
			"publish kernel, initramfs, and rootfs.squashfs using the normalized artifact paths from manifest.yaml",
		},
	}
	var buf strings.Builder
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(manifest); err != nil {
		return "", err
	}
	return buf.String(), nil
}
