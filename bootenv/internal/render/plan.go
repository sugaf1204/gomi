package render

import (
	"fmt"
	"strings"

	"github.com/sugaf1204/gomi/bootenv/internal/spec"
)

func PlanText(doc spec.Document) string {
	var w strings.Builder
	fmt.Fprintf(&w, "BootEnvironment: %s\n", doc.Metadata.Name)
	fmt.Fprintf(&w, "Architecture: %s\n", doc.Spec.Architecture)
	fmt.Fprintf(&w, "Initramfs backend: %s\n", doc.Spec.Initramfs.Backend)
	fmt.Fprintf(&w, "Runtime rootfs: %s\n", doc.Spec.RootFS.Type)
	fmt.Fprintf(&w, "\n")
	fmt.Fprintf(&w, "Boot chain:\n")
	fmt.Fprintf(&w, "1. Firmware loads iPXE script: %s\n", doc.Spec.PXE.ScriptName)
	fmt.Fprintf(&w, "2. iPXE fetches kernel: %s/%s\n", doc.Spec.PXE.BaseURL, doc.Spec.PXE.KernelPath)
	fmt.Fprintf(&w, "3. iPXE fetches initrd: %s/%s\n", doc.Spec.PXE.BaseURL, doc.Spec.PXE.InitrdPath)
	fmt.Fprintf(&w, "4. initramfs fetches SquashFS rootfs: %s/%s\n", doc.Spec.PXE.BaseURL, doc.Spec.PXE.RootFSPath)
	fmt.Fprintf(&w, "5. initramfs switches root into the SquashFS deploy runtime.\n")
	if doc.Spec.Network.DHCP.Enabled {
		fmt.Fprintf(&w, "6. Networking is expected through %s.\n", doc.Spec.Network.DHCP.Image)
	}
	for _, service := range doc.Spec.RootFS.Services {
		fmt.Fprintf(&w, "- rootfs service %s\n", service.Name)
	}
	if doc.Spec.ControlPlane.MetadataURL != "" {
		fmt.Fprintf(&w, "\nMetadata URL: %s\n", doc.Spec.ControlPlane.MetadataURL)
	}
	if doc.Spec.ControlPlane.WorkflowURL != "" {
		fmt.Fprintf(&w, "Workflow URL: %s\n", doc.Spec.ControlPlane.WorkflowURL)
	}
	return w.String()
}
