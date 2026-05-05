package osimage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/sugaf1204/gomi/internal/infra/memory"
	"github.com/sugaf1204/gomi/internal/osimage"
	"github.com/sugaf1204/gomi/internal/resource"
)

func newTestService() *osimage.Service {
	b := memory.New()
	return osimage.NewService(b.OSImages())
}

func testImage() osimage.OSImage {
	return osimage.OSImage{
		Name:      "ubuntu-test",
		OSFamily:  "ubuntu",
		OSVersion: "24.04",
		Arch:      "amd64",
		Format:    osimage.FormatQCOW2,
		Source:    osimage.SourceUpload,
	}
}

func TestServiceCreate(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()
	img, err := svc.Create(ctx, testImage())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if img.Name != "ubuntu-test" {
		t.Fatalf("expected name ubuntu-test, got %s", img.Name)
	}
}

func TestServiceCRUD(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()
	_, _ = svc.Create(ctx, testImage())

	got, err := svc.Get(ctx, "ubuntu-test")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.OSFamily != "ubuntu" {
		t.Fatalf("expected osFamily ubuntu, got %s", got.OSFamily)
	}

	list, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 image, got %d", len(list))
	}

	if err := svc.Delete(ctx, "ubuntu-test"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err = svc.Get(ctx, "ubuntu-test")
	if !errors.Is(err, resource.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestServiceUpdateStatus(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()
	_, _ = svc.Create(ctx, testImage())

	updated, err := svc.UpdateStatus(ctx, "ubuntu-test", true, "/data/images/ubuntu.qcow2", "")
	if err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	if !updated.Ready {
		t.Fatal("expected ready=true")
	}
}

func TestServiceUpdateStatusWithError(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()
	_, _ = svc.Create(ctx, testImage())

	// Set error status.
	updated, err := svc.UpdateStatus(ctx, "ubuntu-test", false, "", "download failed: timeout")
	if err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	if updated.Ready {
		t.Fatal("expected ready=false")
	}
	if updated.Error != "download failed: timeout" {
		t.Fatalf("expected error message, got %s", updated.Error)
	}
	if updated.LocalPath != "" {
		t.Fatalf("expected empty localPath, got %s", updated.LocalPath)
	}
}

func TestServiceUpdateStatusClearError(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()
	_, _ = svc.Create(ctx, testImage())

	// First set an error.
	_, err := svc.UpdateStatus(ctx, "ubuntu-test", false, "", "download failed")
	if err != nil {
		t.Fatalf("UpdateStatus (error): %v", err)
	}

	// Then clear the error by setting ready.
	updated, err := svc.UpdateStatus(ctx, "ubuntu-test", true, "/data/images/ubuntu.qcow2", "")
	if err != nil {
		t.Fatalf("UpdateStatus (clear): %v", err)
	}
	if !updated.Ready {
		t.Fatal("expected ready=true after clearing error")
	}
	if updated.Error != "" {
		t.Fatalf("expected empty error after clearing, got %s", updated.Error)
	}
	if updated.LocalPath != "/data/images/ubuntu.qcow2" {
		t.Fatalf("expected localPath to be set, got %s", updated.LocalPath)
	}
}

func TestServiceUpdateStatusNonExistent(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	_, err := svc.UpdateStatus(ctx, "ghost-image", true, "/path", "")
	if !errors.Is(err, resource.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestServiceCreateWithUnsupportedSource(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	img := osimage.OSImage{
		Name:      "centos-x",
		OSFamily:  "centos",
		OSVersion: "9",
		Arch:      "amd64",
		Format:    osimage.FormatQCOW2,
		Source:    osimage.SourceType("s3"),
	}
	if _, err := svc.Create(ctx, img); err == nil {
		t.Fatal("expected create to fail for unsupported source")
	}
}

func TestServiceCreateWithURLSource(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	img := osimage.OSImage{
		Name:      "debian-url",
		OSFamily:  "debian",
		OSVersion: "13",
		Arch:      "amd64",
		Format:    osimage.FormatRAW,
		Source:    osimage.SourceURL,
		URL:       "https://cloud.debian.org/images/cloud/trixie/latest/debian-13-generic-amd64.raw",
	}
	created, err := svc.Create(ctx, img)
	if err != nil {
		t.Fatalf("Create with url source: %v", err)
	}
	if created.Source != osimage.SourceURL {
		t.Fatalf("expected source url, got %s", created.Source)
	}
	if created.Format != osimage.FormatRAW {
		t.Fatalf("expected format raw, got %s", created.Format)
	}
}

func TestServiceCreateWithDefaultFormat(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	img := osimage.OSImage{
		Name:      "ubuntu-noformat",
		OSFamily:  "ubuntu",
		OSVersion: "22.04",
		Arch:      "amd64",
		// Format and Source omitted -- should default.
	}
	created, err := svc.Create(ctx, img)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Format != osimage.FormatQCOW2 {
		t.Fatalf("expected default format qcow2, got %s", created.Format)
	}
	if created.Source != osimage.SourceUpload {
		t.Fatalf("expected default source upload, got %s", created.Source)
	}
}

func TestServiceListEmpty(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	list, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 images, got %d", len(list))
	}
}

func TestServiceDeleteNonExistent(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	err := svc.Delete(ctx, "ghost-img")
	if !errors.Is(err, resource.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestServiceCreateSetsTimestamps(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	img, err := svc.Create(ctx, testImage())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if img.CreatedAt.IsZero() {
		t.Fatal("expected non-zero createdAt")
	}
	if img.UpdatedAt.IsZero() {
		t.Fatal("expected non-zero updatedAt")
	}
}

func TestServiceCreateWithISOFormat(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	img := osimage.OSImage{
		Name:      "ubuntu-iso",
		OSFamily:  "ubuntu",
		OSVersion: "24.04",
		Arch:      "amd64",
		Format:    osimage.FormatISO,
		Source:    osimage.SourceUpload,
	}
	created, err := svc.Create(ctx, img)
	if err != nil {
		t.Fatalf("Create ISO: %v", err)
	}
	if created.Format != osimage.FormatISO {
		t.Fatalf("expected format iso, got %s", created.Format)
	}
}
