package spec

import (
	"strings"
	"testing"
)

func TestValidateRejectsProceduralEmptySpec(t *testing.T) {
	doc := Document{
		APIVersion: APIVersion,
		Kind:       Kind,
		Metadata:   Metadata{Name: "bad-env"},
	}

	err := doc.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	for _, want := range []string{
		"spec.kernel.package or spec.kernel.path is required",
		"spec.rootfs.source.type is required",
		"spec.pxe.baseURL is required",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("validation error missing %q:\n%s", want, err)
		}
	}
}

func TestDefaults(t *testing.T) {
	doc := Document{
		APIVersion: APIVersion,
		Kind:       Kind,
		Metadata:   Metadata{Name: "agent"},
		Spec: Spec{
			Kernel:  Kernel{Package: "linux-generic"},
			RootFS:  RootFS{Source: RootFSSource{Type: "directory", Path: "/tmp/rootfs"}},
			Network: Network{DHCP: DHCP{Enabled: true}},
			PXE:     PXE{BaseURL: "http://example.test/agent"},
		},
	}

	doc.ApplyDefaults()
	if doc.Spec.Architecture != "amd64" {
		t.Fatalf("architecture = %q", doc.Spec.Architecture)
	}
	if doc.Spec.RootFS.Build.Compression != "zstd" {
		t.Fatalf("compression = %q", doc.Spec.RootFS.Build.Compression)
	}
	if doc.Spec.PXE.RootFSPath != "agent.rootfs.squashfs" {
		t.Fatalf("rootfs path = %q", doc.Spec.PXE.RootFSPath)
	}
	if doc.Spec.Network.DHCP.Image != "systemd-networkd" {
		t.Fatalf("dhcp image = %q", doc.Spec.Network.DHCP.Image)
	}
	if doc.Spec.PXE.KernelPath != "agent.kernel" {
		t.Fatalf("kernel path = %q", doc.Spec.PXE.KernelPath)
	}
	if err := doc.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateAcceptsUbuntuCloudSquashFS(t *testing.T) {
	doc := Document{
		APIVersion: APIVersion,
		Kind:       Kind,
		Metadata:   Metadata{Name: "ubuntu-cloud"},
		Spec: Spec{
			Kernel: Kernel{Package: "linux-image-generic"},
			RootFS: RootFS{Source: RootFSSource{
				Type: "ubuntu-cloud-squashfs",
				URL:  "https://cloud-images.ubuntu.com/minimal/releases/noble/release/",
			}},
			PXE: PXE{BaseURL: "http://example.test/bootenv"},
		},
	}
	doc.ApplyDefaults()
	if err := doc.Validate(); err != nil {
		t.Fatal(err)
	}
}
