package vm

import (
	"context"
	"testing"

	"github.com/sugaf1204/gomi/internal/osimage"
	"github.com/sugaf1204/gomi/internal/resource"
)

type testOSImageStore struct {
	items map[string]osimage.OSImage
}

func (s *testOSImageStore) Upsert(_ context.Context, img osimage.OSImage) error {
	if s.items == nil {
		s.items = map[string]osimage.OSImage{}
	}
	s.items[img.Name] = img
	return nil
}

func (s *testOSImageStore) Get(_ context.Context, name string) (osimage.OSImage, error) {
	img, ok := s.items[name]
	if !ok {
		return osimage.OSImage{}, resource.ErrNotFound
	}
	return img, nil
}

func (s *testOSImageStore) List(context.Context) ([]osimage.OSImage, error) {
	out := make([]osimage.OSImage, 0, len(s.items))
	for _, img := range s.items {
		out = append(out, img)
	}
	return out, nil
}

func (s *testOSImageStore) Delete(_ context.Context, name string) error {
	if _, ok := s.items[name]; !ok {
		return resource.ErrNotFound
	}
	delete(s.items, name)
	return nil
}

func TestResolveCloudImageBacking_UsesHypervisorSyncedArtifactPath(t *testing.T) {
	store := &testOSImageStore{}
	svc := osimage.NewService(store)
	_, err := svc.Create(context.Background(), osimage.OSImage{
		Name:      "ubuntu-24.04-amd64",
		OSFamily:  "ubuntu",
		OSVersion: "24.04",
		Arch:      "amd64",
		Format:    osimage.FormatRAW,
		Source:    osimage.SourceUpload,
		Manifest: &osimage.Manifest{
			Root: osimage.RootArtifact{
				Format:      osimage.FormatRAW,
				Compression: "zst",
				Path:        "root.raw.zst",
				SHA256:      "root-sha",
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
	if path != "/var/lib/libvirt/images/ubuntu-24.04-amd64.raw" {
		t.Fatalf("expected hypervisor backing path, got %q", path)
	}
	if format != "raw" {
		t.Fatalf("expected raw backing format, got %q", format)
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

func TestResolveCloudImageBacking_MissingImage(t *testing.T) {
	d := &Deployer{OSImages: osimage.NewService(&testOSImageStore{})}
	_, _, err := d.resolveCloudImageBacking(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected missing image error")
	}
}

func TestBuildDomainConfig_IgnoresUnsupportedLegacyRawDiskFormat(t *testing.T) {
	v := VirtualMachine{
		Name: "vm-ubuntu",
		Resources: ResourceSpec{
			CPUCores: 1,
			MemoryMB: 1024,
			DiskGB:   10,
		},
		OSImageRef: "ubuntu-24.04-amd64",
		InstallCfg: &InstallConfig{Type: InstallConfigCurtin},
		AdvancedOptions: &AdvancedOptions{
			DiskFormat: "raw",
		},
	}

	cfg := BuildDomainConfig(v, v.Name, "hd", "", nil)
	if cfg.DiskFormat != "qcow2" {
		t.Fatalf("expected VM domain format to stay qcow2, got %q", cfg.DiskFormat)
	}
}
