package vm

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPrepareNoCloudSeed_UploadsRawSeedISO(t *testing.T) {
	files := map[string]string{
		"user-data":      "#cloud-config\nhostname: vm-seed\n",
		"meta-data":      "instance-id: vm-seed\nlocal-hostname: vm-seed\n",
		"vendor-data":    "#cloud-config\n{}",
		"network-config": "version: 2\n",
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		body, ok := files[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	storage := &fakeCloudImageStorage{}
	d := &Deployer{}
	path, err := d.prepareNoCloudSeed(context.Background(), storage, VirtualMachine{
		Name: "vm-seed",
		Network: []NetworkInterface{
			{MAC: "52:54:00:aa:bb:cc"},
		},
	}, srv.URL)
	if err != nil {
		t.Fatalf("prepareNoCloudSeed: %v", err)
	}
	if path != "/var/lib/libvirt/images/vm-seed-cidata.raw" {
		t.Fatalf("unexpected seed path: %q", path)
	}
	if storage.deletedName != "vm-seed-cidata" {
		t.Fatalf("expected seed cleanup before upload, got %q", storage.deletedName)
	}
	if storage.createdName != "vm-seed-cidata" {
		t.Fatalf("expected seed volume name, got %q", storage.createdName)
	}
	if storage.format != "raw" {
		t.Fatalf("expected raw seed volume, got %q", storage.format)
	}
	if storage.sizeBytes != int64(len(storage.data)) || storage.sizeBytes == 0 {
		t.Fatalf("unexpected seed upload size: sizeBytes=%d data=%d", storage.sizeBytes, len(storage.data))
	}
	if !bytes.Contains(storage.data, []byte("CD001")) {
		t.Fatalf("uploaded seed does not look like an ISO9660 image")
	}
	if !bytes.Contains(storage.data, []byte("#cloud-config")) {
		t.Fatalf("uploaded seed ISO does not contain expected user-data")
	}
}

func TestPrepareNoCloudSeed_RequiresPrimaryMAC(t *testing.T) {
	storage := &fakeCloudImageStorage{}
	d := &Deployer{}
	_, err := d.prepareNoCloudSeed(context.Background(), storage, VirtualMachine{Name: "vm-no-mac"}, "http://pxe.example/pxe")
	if err == nil || !strings.Contains(err.Error(), "primary MAC is required") {
		t.Fatalf("expected primary MAC error, got %v", err)
	}
	if storage.deletedName != "" || storage.createdName != "" {
		t.Fatalf("seed volume must not be touched without MAC, deleted=%q created=%q", storage.deletedName, storage.createdName)
	}
}
