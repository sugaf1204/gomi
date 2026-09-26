package sql

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/sugaf1204/gomi/internal/cloudinit"
)

func TestCloudInitDeliveryMigrationAndPersistence(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "legacy.db")
	b, err := New("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	check(b.Migrate())
	now := time.Now().UTC()
	check(b.CloudInits().Upsert(context.Background(), cloudinit.CloudInitTemplate{Name: "legacy", UserData: "#cloud-config", CreatedAt: now, UpdatedAt: now}))
	// Recreate the column layout of a pre-feature database, preserving a row.
	_, err = b.db.Exec("ALTER TABLE cloud_init_templates DROP COLUMN delivery_mode")
	check(err)
	check(b.Migrate())
	check(b.Migrate())
	legacy, err := b.CloudInits().Get(context.Background(), "legacy")
	check(err)
	if legacy.DeliveryMode != "" || legacy.UserData != "#cloud-config" {
		t.Fatalf("legacy changed: %+v", legacy)
	}
	legacy.DeliveryMode = cloudinit.DeliveryVMSeed
	check(b.CloudInits().Upsert(context.Background(), legacy))
	legacy.DeliveryMode = ""
	check(b.CloudInits().Upsert(context.Background(), legacy))
	check(b.Close())
	b, err = New("sqlite", dsn)
	check(err)
	defer b.Close()
	check(b.Migrate())
	got, err := b.CloudInits().Get(context.Background(), "legacy")
	check(err)
	list, err := b.CloudInits().List(context.Background())
	check(err)
	if got.DeliveryMode != cloudinit.DeliveryVMSeed || len(list) != 1 || list[0].DeliveryMode != cloudinit.DeliveryVMSeed {
		t.Fatal("lost protected mode on upsert or restart")
	}
}
