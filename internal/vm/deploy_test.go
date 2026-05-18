package vm

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/sugaf1204/gomi/internal/hypervisor"
	"github.com/sugaf1204/gomi/internal/osimage"
	"github.com/sugaf1204/gomi/internal/resource"
)

type fakeCloudImageStorage struct {
	exists       bool
	existsByName map[string]bool
	createdName  string
	format       string
	sizeBytes    int64
	data         []byte
	deletedName  string
}

type concurrentCloudImageStorage struct {
	mu          sync.Mutex
	volumes     map[string]bool
	inflight    map[string]bool
	createCalls int
}

func (s *concurrentCloudImageStorage) VolumeExists(_ context.Context, name string, format string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.volumes[name+"."+format], nil
}

func (s *concurrentCloudImageStorage) CreateVolumeFromReader(_ context.Context, name string, sizeBytes int64, format string, r io.Reader) error {
	key := name + "." + format
	s.mu.Lock()
	if s.volumes[key] || s.inflight[key] {
		s.mu.Unlock()
		return errors.New("duplicate backing volume create")
	}
	s.inflight[key] = true
	s.mu.Unlock()

	if _, err := io.Copy(io.Discard, r); err != nil {
		return err
	}

	s.mu.Lock()
	delete(s.inflight, key)
	s.volumes[key] = true
	s.createCalls++
	s.mu.Unlock()
	return nil
}

func (s *concurrentCloudImageStorage) DeleteVolume(_ context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.volumes, name+".qcow2")
	return nil
}

func (s *fakeCloudImageStorage) VolumeExists(_ context.Context, name string, format string) (bool, error) {
	s.createdName = name
	s.format = format
	if s.existsByName != nil {
		return s.existsByName[cloudImageVolumeName(name, format)], nil
	}
	return s.exists, nil
}

func (s *fakeCloudImageStorage) CreateVolumeFromReader(_ context.Context, name string, sizeBytes int64, format string, r io.Reader) error {
	s.createdName = name
	s.format = format
	s.sizeBytes = sizeBytes
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	s.data = data
	return nil
}

func (s *fakeCloudImageStorage) DeleteVolume(_ context.Context, name string) error {
	s.deletedName = name
	return nil
}

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

func TestPrepareCloudImageBacking_DownloadsURLImageToHypervisor(t *testing.T) {
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
		Name:      "debian-13-amd64-cloud",
		OSFamily:  "debian",
		OSVersion: "13",
		Arch:      "amd64",
		Format:    osimage.FormatQCOW2,
		Source:    osimage.SourceURL,
		URL:       srv.URL + "/debian-13.qcow2",
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

func TestBuildDomainConfig_IgnoresUnsupportedLegacyDiskFormat(t *testing.T) {
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
			DiskFormat: "vmdk",
		},
	}

	cfg := BuildDomainConfig(v, v.Name, "hd", "", nil)
	if cfg.DiskFormat != "qcow2" {
		t.Fatalf("expected VM domain format to stay qcow2, got %q", cfg.DiskFormat)
	}
}

func TestResolvePXEBaseURL_UsesConfiguredBaseURL(t *testing.T) {
	d := &Deployer{PXEBaseURL: " http://pxe.example:8080/pxe/ "}

	got, err := d.resolvePXEBaseURL(hypervisor.Hypervisor{}, InstallConfigCurtin)
	if err != nil {
		t.Fatalf("resolvePXEBaseURL: %v", err)
	}
	if got != "http://pxe.example:8080/pxe" {
		t.Fatalf("expected configured base URL, got %q", got)
	}
}

func TestResolvePXEBaseURL_DerivesFromConcreteListenAddr(t *testing.T) {
	got, err := resolvePXEBaseURLFromListen("192.168.2.192:8080", func() (string, error) {
		t.Fatal("primary IP detector should not be called for concrete listen addr")
		return "", nil
	})
	if err != nil {
		t.Fatalf("resolvePXEBaseURLFromListen: %v", err)
	}
	if got != "http://192.168.2.192:8080/pxe" {
		t.Fatalf("expected listen-derived PXE URL, got %q", got)
	}
}

func TestResolvePXEBaseURL_DetectsPrimaryIPForWildcardListenAddr(t *testing.T) {
	got, err := resolvePXEBaseURLFromListen("0.0.0.0:8080", func() (string, error) {
		return "192.168.2.192", nil
	})
	if err != nil {
		t.Fatalf("resolvePXEBaseURLFromListen: %v", err)
	}
	if got != "http://192.168.2.192:8080/pxe" {
		t.Fatalf("expected detected primary IP PXE URL, got %q", got)
	}
}

func TestResolvePXEBaseURL_ErrorsForLoopbackListenAddr(t *testing.T) {
	if _, err := resolvePXEBaseURLFromListen("127.0.0.1:8080", func() (string, error) {
		return "192.168.2.192", nil
	}); err == nil {
		t.Fatal("expected loopback listen addr to require explicit PXE base URL")
	}
}
