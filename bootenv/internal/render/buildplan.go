package render

import (
	"encoding/json"

	"github.com/sugaf1204/gomi/bootenv/internal/spec"
)

type BuildPlan struct {
	APIVersion string      `json:"apiVersion"`
	Name       string      `json:"name"`
	Backend    string      `json:"backend"`
	Inputs     PlanInputs  `json:"inputs"`
	Outputs    PlanOutputs `json:"outputs"`
	Steps      []PlanStep  `json:"steps"`
}

type PlanInputs struct {
	RootFSType   string `json:"rootfsType"`
	RootFSSource string `json:"rootfsSource"`
	Kernel       string `json:"kernel"`
}

type PlanOutputs struct {
	Kernel    string `json:"kernel"`
	Initramfs string `json:"initramfs"`
	RootFS    string `json:"rootfs"`
	IPXE      string `json:"ipxe"`
}

type PlanStep struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tools       []string `json:"tools,omitempty"`
}

func BuildPlanJSON(doc spec.Document) (string, error) {
	plan := BuildPlan{
		APIVersion: "gomi.sugaf1204.github.io/ephemeral/v1alpha1",
		Name:       doc.Metadata.Name,
		Backend:    doc.Spec.Initramfs.Backend,
		Inputs: PlanInputs{
			RootFSType:   doc.Spec.RootFS.Type,
			RootFSSource: rootfsSource(doc),
			Kernel:       kernelSource(doc),
		},
		Outputs: PlanOutputs{
			Kernel:    doc.Spec.PXE.KernelPath,
			Initramfs: doc.Spec.PXE.InitrdPath,
			RootFS:    doc.Spec.PXE.RootFSPath,
			IPXE:      doc.Spec.PXE.ScriptName,
		},
		Steps: []PlanStep{
			{
				Name:        "prepare-rootfs",
				Description: "Prepare a mutable rootfs work directory from the declared source.",
				Tools:       []string{"unsquashfs", "overlayfs", "rsync"},
			},
			{
				Name:        "configure-rootfs",
				Description: "Apply declared packages, files, services, and control-plane metadata.",
				Tools:       []string{"chroot", "apt/dnf", "systemctl"},
			},
			{
				Name:        "build-initramfs",
				Description: "Generate a distro initramfs with root-url support; do not hand-roll /init.",
				Tools:       initramfsTools(doc),
			},
			{
				Name:        "pack-squashfs",
				Description: "Pack the configured deploy runtime rootfs as a read-only SquashFS artifact.",
				Tools:       []string{"mksquashfs"},
			},
			{
				Name:        "publish-boot-artifacts",
				Description: "Publish kernel, initramfs, SquashFS rootfs, manifest, and iPXE script.",
			},
		},
	}
	raw, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return "", err
	}
	return string(raw) + "\n", nil
}

func rootfsSource(doc spec.Document) string {
	if doc.Spec.RootFS.Source.URL != "" {
		return doc.Spec.RootFS.Source.URL
	}
	return doc.Spec.RootFS.Source.Path
}

func kernelSource(doc spec.Document) string {
	if doc.Spec.Kernel.Path != "" {
		return doc.Spec.Kernel.Path
	}
	return doc.Spec.Kernel.Package
}

func initramfsTools(doc spec.Document) []string {
	if doc.Spec.Initramfs.Backend == "dracut" {
		return []string{"dracut"}
	}
	return []string{"initramfs-tools", "cloud-initramfs-rooturl", "cloud-initramfs-copymods", "overlayroot"}
}
