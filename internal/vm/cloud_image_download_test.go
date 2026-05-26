package vm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sugaf1204/gomi/internal/osimage"
)

func TestPrepareCloudImageBacking_DownloadsURLImageToHypervisor(t *testing.T) {
	payload := []byte("qcow2-data")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "10")
		if _, err := w.Write(payload); err != nil {
			t.Fatalf("write response: %v", err)
		}
	}))
	defer srv.Close()
	sum := sha256.Sum256(payload)
	checksum := strings.ToUpper(hex.EncodeToString(sum[:]))

	store := &testOSImageStore{}
	svc := osimage.NewService(store)
	_, err := svc.Create(context.Background(), osimage.OSImage{
		Name:      "debian-13-amd64-cloud",
		OSFamily:  "debian",
		OSVersion: "13",
		Arch:      "amd64",
		Format:    osimage.FormatQCOW2,
		Source:    osimage.SourceURL,
		URL:       srv.URL + "/debian-13.qcow2",
		Checksum:  checksum,
		SizeBytes: int64(len(payload)),
	})
	if err != nil {
		t.Fatalf("Create image: %v", err)
	}
	if _, err := svc.UpdateStatus(context.Background(), "debian-13-amd64-cloud", true, "", ""); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	img, err := svc.Get(context.Background(), "debian-13-amd64-cloud")
	if err != nil {
		t.Fatalf("Get image: %v", err)
	}
	expectedVolumeName := cloudImageBackingVolumeBaseName(img, "qcow2")

	storage := &fakeCloudImageStorage{}
	d := &Deployer{OSImages: svc}
	path, format, err := d.prepareCloudImageBacking(context.Background(), storage, "debian-13-amd64-cloud")
	if err != nil {
		t.Fatalf("prepareCloudImageBacking: %v", err)
	}
	if path != "/var/lib/libvirt/images/"+cloudImageVolumeName(expectedVolumeName, "qcow2") {
		t.Fatalf("unexpected backing path: %q", path)
	}
	if format != "qcow2" {
		t.Fatalf("unexpected backing format: %q", format)
	}
	if storage.createdName != expectedVolumeName {
		t.Fatalf("expected uploaded image name, got %q", storage.createdName)
	}
	if storage.sizeBytes != int64(len(payload)) {
		t.Fatalf("expected upload size %d, got %d", len(payload), storage.sizeBytes)
	}
	if string(storage.data) != string(payload) {
		t.Fatalf("unexpected uploaded payload %q", string(storage.data))
	}
	if !storage.createHadDeadline {
		t.Fatalf("expected cloud image upload to use a bounded context")
	}
}

func TestUploadCloudImageBacking_ReturnsCleanupErrorAfterChecksumMismatch(t *testing.T) {
	payload := []byte("corrupt-qcow2")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		if _, err := w.Write(payload); err != nil {
			t.Fatalf("write response: %v", err)
		}
	}))
	defer srv.Close()

	storage := &fakeCloudImageStorage{deleteErr: errors.New("delete failed")}
	d := &Deployer{}
	err := d.uploadCloudImageBacking(context.Background(), storage, osimage.OSImage{
		Name:      "bad-cloud-image",
		URL:       srv.URL + "/bad.qcow2",
		Checksum:  strings.Repeat("0", 64),
		SizeBytes: int64(len(payload)),
	}, "bad-cloud-image", "qcow2")
	if err == nil {
		t.Fatal("expected checksum mismatch error")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") || !strings.Contains(err.Error(), "cleanup failed") || !strings.Contains(err.Error(), "delete failed") {
		t.Fatalf("expected checksum cleanup error, got %v", err)
	}
	if storage.deletedName != "bad-cloud-image" {
		t.Fatalf("expected corrupted volume cleanup, got %q", storage.deletedName)
	}
}

func TestPrepareCloudImageBacking_SerializesConcurrentURLImageSync(t *testing.T) {
	payload := []byte("qcow2-data")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "10")
		if _, err := w.Write(payload); err != nil {
			t.Fatalf("write response: %v", err)
		}
	}))
	defer srv.Close()

	store := &testOSImageStore{}
	svc := osimage.NewService(store)
	_, err := svc.Create(context.Background(), osimage.OSImage{
		Name:      "ubuntu-24.04-amd64-cloud",
		OSFamily:  "ubuntu",
		OSVersion: "24.04",
		Arch:      "amd64",
		Format:    osimage.FormatQCOW2,
		Source:    osimage.SourceURL,
		URL:       srv.URL + "/ubuntu-24.04.img",
		SizeBytes: int64(len(payload)),
	})
	if err != nil {
		t.Fatalf("Create image: %v", err)
	}
	if _, err := svc.UpdateStatus(context.Background(), "ubuntu-24.04-amd64-cloud", true, "", ""); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	storage := &concurrentCloudImageStorage{
		volumes:  map[string]bool{},
		inflight: map[string]bool{},
	}
	d := &Deployer{OSImages: svc}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := d.prepareCloudImageBacking(context.Background(), storage, "ubuntu-24.04-amd64-cloud")
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("prepareCloudImageBacking: %v", err)
		}
	}
	if storage.createCalls != 1 {
		t.Fatalf("expected one backing upload, got %d", storage.createCalls)
	}
}

func TestPrepareCloudImageBacking_SkipsDownloadWhenHypervisorVolumeExists(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be called when backing volume exists")
	}))
	defer srv.Close()

	store := &testOSImageStore{}
	svc := osimage.NewService(store)
	_, err := svc.Create(context.Background(), osimage.OSImage{
		Name:      "ubuntu-24.04-amd64-cloud",
		OSFamily:  "ubuntu",
		OSVersion: "24.04",
		Arch:      "amd64",
		Format:    osimage.FormatQCOW2,
		Source:    osimage.SourceURL,
		URL:       srv.URL + "/ubuntu-24.04.img",
	})
	if err != nil {
		t.Fatalf("Create image: %v", err)
	}
	if _, err := svc.UpdateStatus(context.Background(), "ubuntu-24.04-amd64-cloud", true, "", ""); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	img, err := svc.Get(context.Background(), "ubuntu-24.04-amd64-cloud")
	if err != nil {
		t.Fatalf("Get image: %v", err)
	}
	expectedVolumeName := cloudImageBackingVolumeBaseName(img, "qcow2")

	storage := &fakeCloudImageStorage{exists: true}
	d := &Deployer{OSImages: svc}
	if _, _, err := d.prepareCloudImageBacking(context.Background(), storage, "ubuntu-24.04-amd64-cloud"); err != nil {
		t.Fatalf("prepareCloudImageBacking: %v", err)
	}
	if storage.data != nil {
		t.Fatalf("expected no upload when volume exists")
	}
	if storage.createdName != expectedVolumeName {
		t.Fatalf("expected lookup by content-addressed volume name %q, got %q", expectedVolumeName, storage.createdName)
	}
}

func TestPrepareCloudImageBacking_DownloadsURLImageEvenWhenSameNamedLocalBackingExists(t *testing.T) {
	payload := []byte("fresh-qcow2")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		if _, err := w.Write(payload); err != nil {
			t.Fatalf("write response: %v", err)
		}
	}))
	defer srv.Close()

	store := &testOSImageStore{}
	svc := osimage.NewService(store)
	_, err := svc.Create(context.Background(), osimage.OSImage{
		Name:      "ubuntu-24.04-amd64-cloud",
		OSFamily:  "ubuntu",
		OSVersion: "24.04",
		Arch:      "amd64",
		Format:    osimage.FormatQCOW2,
		Source:    osimage.SourceURL,
		URL:       srv.URL + "/ubuntu-24.04.img",
	})
	if err != nil {
		t.Fatalf("Create image: %v", err)
	}
	if _, err := svc.UpdateStatus(context.Background(), "ubuntu-24.04-amd64-cloud", true, "/var/lib/gomi/images/ubuntu-24.04-amd64-cloud.qcow2", ""); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	img, err := svc.Get(context.Background(), "ubuntu-24.04-amd64-cloud")
	if err != nil {
		t.Fatalf("Get image: %v", err)
	}
	expectedVolumeName := cloudImageURLVolumeBaseName(img, "qcow2")

	storage := &fakeCloudImageStorage{
		existsByName: map[string]bool{
			cloudImageVolumeName("ubuntu-24.04-amd64-cloud", "qcow2"): true,
		},
	}
	d := &Deployer{OSImages: svc}
	path, format, err := d.prepareCloudImageBacking(context.Background(), storage, "ubuntu-24.04-amd64-cloud")
	if err != nil {
		t.Fatalf("prepareCloudImageBacking: %v", err)
	}
	if path != "/var/lib/libvirt/images/"+cloudImageVolumeName(expectedVolumeName, "qcow2") {
		t.Fatalf("expected content-addressed backing path, got %q", path)
	}
	if format != "qcow2" {
		t.Fatalf("expected qcow2 backing format, got %q", format)
	}
	if storage.createdName != expectedVolumeName {
		t.Fatalf("expected content-addressed upload %q, got %q", expectedVolumeName, storage.createdName)
	}
	if string(storage.data) != string(payload) {
		t.Fatalf("unexpected uploaded payload %q", string(storage.data))
	}
}

func TestPrepareCloudImageBacking_DownloadsWhenPreStagedURLImageIsMissingOnHypervisor(t *testing.T) {
	payload := []byte("qcow2-data")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "10")
		if _, err := w.Write(payload); err != nil {
			t.Fatalf("write response: %v", err)
		}
	}))
	defer srv.Close()

	store := &testOSImageStore{}
	svc := osimage.NewService(store)
	_, err := svc.Create(context.Background(), osimage.OSImage{
		Name:      "ubuntu-24.04-amd64-cloud",
		OSFamily:  "ubuntu",
		OSVersion: "24.04",
		Arch:      "amd64",
		Format:    osimage.FormatQCOW2,
		Source:    osimage.SourceURL,
		URL:       srv.URL + "/ubuntu-24.04.img",
		SizeBytes: int64(len(payload)),
	})
	if err != nil {
		t.Fatalf("Create image: %v", err)
	}
	if _, err := svc.UpdateStatus(context.Background(), "ubuntu-24.04-amd64-cloud", true, "/var/lib/gomi/images/ubuntu-24.04-amd64-cloud.qcow2", ""); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	img, err := svc.Get(context.Background(), "ubuntu-24.04-amd64-cloud")
	if err != nil {
		t.Fatalf("Get image: %v", err)
	}
	expectedVolumeName := cloudImageURLVolumeBaseName(img, "qcow2")

	storage := &fakeCloudImageStorage{}
	d := &Deployer{OSImages: svc}
	path, format, err := d.prepareCloudImageBacking(context.Background(), storage, "ubuntu-24.04-amd64-cloud")
	if err != nil {
		t.Fatalf("prepareCloudImageBacking: %v", err)
	}
	if path != "/var/lib/libvirt/images/"+cloudImageVolumeName(expectedVolumeName, "qcow2") {
		t.Fatalf("expected downloaded backing path, got %q", path)
	}
	if format != "qcow2" {
		t.Fatalf("expected qcow2 backing format, got %q", format)
	}
	if storage.createdName != expectedVolumeName {
		t.Fatalf("expected fallback download to %q, got %q", expectedVolumeName, storage.createdName)
	}
	if string(storage.data) != string(payload) {
		t.Fatalf("unexpected uploaded payload %q", string(storage.data))
	}
}

func TestPrepareCloudImageBacking_DoesNotReuseOldURLBackingAfterImageRecreate(t *testing.T) {
	payload := []byte("new-qcow2")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "9")
		if _, err := w.Write(payload); err != nil {
			t.Fatalf("write response: %v", err)
		}
	}))
	defer srv.Close()

	oldImage := osimage.OSImage{
		Name:      "ubuntu-24.04-amd64-cloud",
		OSFamily:  "ubuntu",
		OSVersion: "24.04",
		Arch:      "amd64",
		Format:    osimage.FormatQCOW2,
		Source:    osimage.SourceURL,
		URL:       "https://example.invalid/old.img",
		Ready:     true,
		CreatedAt: time.Unix(100, 0).UTC(),
		UpdatedAt: time.Unix(100, 0).UTC(),
	}
	newImage := oldImage
	newImage.URL = srv.URL + "/new.img"
	newImage.SizeBytes = int64(len(payload))
	newImage.CreatedAt = time.Unix(200, 0).UTC()
	newImage.UpdatedAt = time.Unix(200, 0).UTC()

	oldVolumeName := cloudImageBackingVolumeBaseName(oldImage, "qcow2")
	newVolumeName := cloudImageBackingVolumeBaseName(newImage, "qcow2")
	if oldVolumeName == newVolumeName {
		t.Fatalf("expected recreated image to use a different backing volume")
	}

	store := &testOSImageStore{items: map[string]osimage.OSImage{
		newImage.Name: newImage,
	}}
	storage := &fakeCloudImageStorage{
		existsByName: map[string]bool{
			cloudImageVolumeName(oldVolumeName, "qcow2"): true,
		},
	}
	d := &Deployer{OSImages: osimage.NewService(store)}
	path, format, err := d.prepareCloudImageBacking(context.Background(), storage, newImage.Name)
	if err != nil {
		t.Fatalf("prepareCloudImageBacking: %v", err)
	}
	if format != "qcow2" {
		t.Fatalf("unexpected backing format: %q", format)
	}
	if path != "/var/lib/libvirt/images/"+cloudImageVolumeName(newVolumeName, "qcow2") {
		t.Fatalf("unexpected backing path: %q", path)
	}
	if storage.createdName != newVolumeName {
		t.Fatalf("expected new backing volume %q, got %q", newVolumeName, storage.createdName)
	}
	if string(storage.data) != string(payload) {
		t.Fatalf("unexpected uploaded payload %q", string(storage.data))
	}
}
