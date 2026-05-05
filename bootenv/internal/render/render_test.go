package render

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sugaf1204/gomi/bootenv/internal/spec"
)

func TestRenderBuildPlanAndIPXE(t *testing.T) {
	doc := loadExample(t)

	buildPlan, err := BuildPlanJSON(doc)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"backend": "initramfs-tools"`,
		`"rootfsType": "squashfs"`,
		`"rootfs": "rootfs.squashfs"`,
		`"pack-squashfs"`,
	} {
		if !strings.Contains(buildPlan, want) {
			t.Fatalf("build plan missing %q:\n%s", want, buildPlan)
		}
	}

	ipxe := IPXEScript(doc)
	for _, want := range []string{
		"#!ipxe",
		"kernel ${base-url}/boot-kernel console=tty0 console=ttyS0 panic=10 boot=live components fetch=http://gomi.local/files/linux/rootfs.squashfs",
		"initrd ${base-url}/boot-initrd",
	} {
		if !strings.Contains(ipxe, want) {
			t.Fatalf("ipxe script missing %q:\n%s", want, ipxe)
		}
	}
}

func TestRenderWritesBundle(t *testing.T) {
	doc := loadExample(t)
	dir := t.TempDir()

	if err := Render(doc, dir); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"build-plan.json",
		"boot.ipxe",
		"manifest.yaml",
		"plan.txt",
		"boot-environment.yaml",
	} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("expected %s: %v", name, err)
		}
	}
}

func TestIPXEScriptUsesRootURLForUbuntuCloudSquashFS(t *testing.T) {
	doc := loadExample(t)
	doc.Spec.RootFS.Source.Type = "ubuntu-cloud-squashfs"
	doc.Spec.Initramfs.Cmdline = []string{"ip=dhcp", "overlayroot=tmpfs:recurse=0"}

	ipxe := IPXEScript(doc)
	for _, want := range []string{
		"ip=dhcp overlayroot=tmpfs:recurse=0 root=squash:http://gomi.local/files/linux/rootfs.squashfs",
		"initrd ${base-url}/boot-initrd",
	} {
		if !strings.Contains(ipxe, want) {
			t.Fatalf("ipxe script missing %q:\n%s", want, ipxe)
		}
	}
	if strings.Contains(ipxe, "fetch=") {
		t.Fatalf("cloud squashfs boot must not use live-boot fetch=:\n%s", ipxe)
	}
}

func loadExample(t *testing.T) spec.Document {
	t.Helper()
	doc, err := spec.Load(filepath.Join("..", "..", "bootenvs", "debian-live-standard-amd64.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}
