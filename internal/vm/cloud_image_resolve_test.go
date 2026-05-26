package vm

import (
	"context"
	"testing"

	"github.com/sugaf1204/gomi/internal/osimage"
)

func TestResolveCloudImageBacking_UsesHypervisorSyncedArtifactPath(t *testing.T) {
	store := &testOSImageStore{}
	svc := osimage.NewService(store)
	_, err := svc.Create(context.Background(), osimage.OSImage{
		Name:      "ubuntu-24.04-amd64",
		OSFamily:  "ubuntu",
		OSVersion: "24.04",
		Arch:      "amd64",
		Format:    osimage.FormatQCOW2,
		Source:    osimage.SourceUpload,
		Manifest: &osimage.Manifest{
			Root: osimage.RootArtifact{
				Format: osimage.FormatQCOW2,
				Path:   "root.qcow2",
				SHA256: "root-sha",
			},
		},
	})
	if err != nil {
		t.Fatalf("Create image: %v", err)
	}
	if _, err := svc.UpdateStatus(context.Background(), "ubuntu-24.04-amd64", true, "/var/lib/gomi/artifacts/ubuntu-24.04-amd64", ""); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	d := &Deployer{OSImages: svc}
	path, format, err := d.resolveCloudImageBacking(context.Background(), "ubuntu-24.04-amd64")
	if err != nil {
		t.Fatalf("resolveCloudImageBacking: %v", err)
	}
	if path != "/var/lib/libvirt/images/ubuntu-24.04-amd64.qcow2" {
		t.Fatalf("expected hypervisor backing path, got %q", path)
	}
	if format != "qcow2" {
		t.Fatalf("expected qcow2 backing format, got %q", format)
	}
}

func TestResolveCloudImageBacking_LegacyImageUsesHypervisorSyncedPath(t *testing.T) {
	store := &testOSImageStore{}
	svc := osimage.NewService(store)
	_, err := svc.Create(context.Background(), osimage.OSImage{
		Name:      "ubuntu-qcow2",
		OSFamily:  "ubuntu",
		OSVersion: "24.04",
		Arch:      "amd64",
		Format:    osimage.FormatQCOW2,
		Source:    osimage.SourceUpload,
	})
	if err != nil {
		t.Fatalf("Create image: %v", err)
	}
	if _, err := svc.UpdateStatus(context.Background(), "ubuntu-qcow2", true, "/var/lib/gomi/images/ubuntu-qcow2.qcow2", ""); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	d := &Deployer{OSImages: svc}
	path, format, err := d.resolveCloudImageBacking(context.Background(), "ubuntu-qcow2")
	if err != nil {
		t.Fatalf("resolveCloudImageBacking: %v", err)
	}
	if path != "/var/lib/libvirt/images/ubuntu-qcow2.qcow2" {
		t.Fatalf("expected hypervisor backing path, got %q", path)
	}
	if format != "qcow2" {
		t.Fatalf("expected qcow2 backing format, got %q", format)
	}
}

func TestResolveCloudImageBacking_ImageNameWithQCOW2SuffixUsesSingleSuffix(t *testing.T) {
	store := &testOSImageStore{}
	svc := osimage.NewService(store)
	_, err := svc.Create(context.Background(), osimage.OSImage{
		Name:      "ubuntu-24.04-amd64-cloud.qcow2",
		OSFamily:  "ubuntu",
		OSVersion: "24.04",
		Arch:      "amd64",
		Format:    osimage.FormatQCOW2,
		Source:    osimage.SourceUpload,
	})
	if err != nil {
		t.Fatalf("Create image: %v", err)
	}
	if _, err := svc.UpdateStatus(context.Background(), "ubuntu-24.04-amd64-cloud.qcow2", true, "/var/lib/gomi/images/ubuntu-24.04-amd64-cloud.qcow2", ""); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	d := &Deployer{OSImages: svc}
	path, format, err := d.resolveCloudImageBacking(context.Background(), "ubuntu-24.04-amd64-cloud.qcow2")
	if err != nil {
		t.Fatalf("resolveCloudImageBacking: %v", err)
	}
	if path != "/var/lib/libvirt/images/ubuntu-24.04-amd64-cloud.qcow2" {
		t.Fatalf("expected single qcow2 suffix, got %q", path)
	}
	if format != "qcow2" {
		t.Fatalf("expected qcow2 backing format, got %q", format)
	}
}

func TestResolveCloudImageBacking_MissingImage(t *testing.T) {
	d := &Deployer{OSImages: osimage.NewService(&testOSImageStore{})}
	_, _, err := d.resolveCloudImageBacking(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected missing image error")
	}
}
