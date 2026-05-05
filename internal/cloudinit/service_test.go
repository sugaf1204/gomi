package cloudinit_test

import (
	"context"
	"errors"
	"testing"

	"github.com/sugaf1204/gomi/internal/cloudinit"
	"github.com/sugaf1204/gomi/internal/infra/memory"
	"github.com/sugaf1204/gomi/internal/resource"
)

func newTestService() *cloudinit.Service {
	b := memory.New()
	return cloudinit.NewService(b.CloudInits())
}

func testTemplate() cloudinit.CloudInitTemplate {
	return cloudinit.CloudInitTemplate{
		Name:     "basic-tpl",
		UserData: "#cloud-config\npackages:\n  - vim\n",
	}
}

func TestServiceCreate(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()
	tpl, err := svc.Create(ctx, testTemplate())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if tpl.Name != "basic-tpl" {
		t.Fatalf("expected name basic-tpl, got %s", tpl.Name)
	}
}

func TestServiceCRUD(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()
	_, _ = svc.Create(ctx, testTemplate())

	got, err := svc.Get(ctx, "basic-tpl")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.UserData == "" {
		t.Fatal("expected non-empty userData")
	}

	list, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 template, got %d", len(list))
	}

	if err := svc.Delete(ctx, "basic-tpl"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err = svc.Get(ctx, "basic-tpl")
	if !errors.Is(err, resource.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestServiceUpdate(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	created, err := svc.Create(ctx, testTemplate())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Update the template with new userData.
	updated := created
	updated.UserData = "#cloud-config\npackages:\n  - vim\n  - curl\n  - htop\n"
	updated.Description = "Updated template with more packages"
	result, err := svc.Update(ctx, updated)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if result.UserData != updated.UserData {
		t.Fatalf("expected updated userData, got %s", result.UserData)
	}
	if result.Description != "Updated template with more packages" {
		t.Fatalf("expected updated description, got %s", result.Description)
	}
	// CreatedAt should be preserved.
	if result.CreatedAt != created.CreatedAt {
		t.Fatalf("expected createdAt to be preserved")
	}
	// UpdatedAt should be newer.
	if !result.UpdatedAt.After(created.CreatedAt) && !result.UpdatedAt.Equal(created.CreatedAt) {
		t.Fatal("expected updatedAt to be >= createdAt")
	}
}

func TestServiceUpdateNonExistent(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	tpl := cloudinit.CloudInitTemplate{
		Name:     "ghost-tpl",
		UserData: "#cloud-config\n",
	}
	_, err := svc.Update(ctx, tpl)
	if !errors.Is(err, resource.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for updating non-existent, got %v", err)
	}
}

func TestServiceCreateDuplicate(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	_, err := svc.Create(ctx, testTemplate())
	if err != nil {
		t.Fatalf("first Create: %v", err)
	}

	// Upsert-based store overwrites.
	_, err = svc.Create(ctx, testTemplate())
	if err != nil {
		t.Fatalf("second Create (upsert): %v", err)
	}

	list, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 template after duplicate create, got %d", len(list))
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
		t.Fatalf("expected 0 templates, got %d", len(list))
	}
}

func TestServiceDeleteNonExistent(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	err := svc.Delete(ctx, "ghost-tpl")
	if !errors.Is(err, resource.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestServiceGetNonExistent(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	_, err := svc.Get(ctx, "ghost-tpl")
	if !errors.Is(err, resource.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestServiceCreateSetsTimestamps(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	tpl, err := svc.Create(ctx, testTemplate())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if tpl.CreatedAt.IsZero() {
		t.Fatal("expected non-zero createdAt")
	}
	if tpl.UpdatedAt.IsZero() {
		t.Fatal("expected non-zero updatedAt")
	}
}

func TestServiceUpdateWithNetworkConfig(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	created, err := svc.Create(ctx, testTemplate())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	updated := created
	updated.NetworkConfig = "version: 2\nethernets:\n  eth0:\n    dhcp4: true\n"
	result, err := svc.Update(ctx, updated)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if result.NetworkConfig != updated.NetworkConfig {
		t.Fatalf("expected networkConfig to be set")
	}

	// Verify via Get.
	got, err := svc.Get(ctx, "basic-tpl")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.NetworkConfig != updated.NetworkConfig {
		t.Fatalf("expected networkConfig to persist after update")
	}
}
