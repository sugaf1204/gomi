package pxehttp

import (
	"context"
	"encoding/json"
	"github.com/labstack/echo/v4"
	apiinventory "github.com/sugaf1204/gomi/api/inventory"
	"github.com/sugaf1204/gomi/internal/hwinfo"
	"github.com/sugaf1204/gomi/internal/infra/memory"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/osimage"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPXEInventory_StoresHardwareInfoAndReturnsPlanURLs(t *testing.T) {
	backend := memory.New()
	machineSvc := machine.NewService(backend.Machines())
	hwInfoSvc := hwinfo.NewService(backend.HWInfo())
	now := time.Now().UTC()

	target := machine.Machine{
		Name:     "bm-inventory",
		Hostname: "bm-inventory",
		MAC:      "52:54:00:aa:bb:03",
		Arch:     "amd64",
		Firmware: machine.FirmwareUEFI,
		Phase:    machine.PhaseProvisioning,
		Provision: &machine.ProvisionProgress{
			Active:          true,
			AttemptID:       "attempt-inventory",
			CompletionToken: "token-inventory",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.Machines().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/pxe/inventory?token=token-inventory", strings.NewReader(strings.Join([]string{
		"runtime\t6.8.0-00-generic",
		"boot\tuefi\ttrue",
		"disk\tnvme0n1\t/dev/nvme0n1\t65536\t0\t/dev/disk/by-id/nvme-GOMI_TEST\t/dev/disk/by-path/pci-0000:00:01.0-nvme-1",
		"nic\tenp1s0\t52:54:00:aa:bb:03\tup",
		"",
	}, "\n")))
	req.Header.Set("Content-Type", "text/plain")
	req.Host = "192.168.2.254:8080"
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h := &Handler{machines: machineSvc, hwinfo: hwInfoSvc}
	if err := h.PXEInventory(c); err != nil {
		t.Fatalf("PXEInventory: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}
	var response map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode inventory response: %v", err)
	}
	if response["attemptId"] != "attempt-inventory" ||
		!strings.HasPrefix(response["curtinConfigUrl"], "http://192.168.2.254:8080/pxe/curtin-config?") ||
		!strings.HasPrefix(response["eventsUrl"], "http://192.168.2.254:8080/pxe/deploy-events?") {
		t.Fatalf("unexpected inventory response: %+v", response)
	}
	got, err := hwInfoSvc.Get(context.Background(), "bm-inventory")
	if err != nil {
		t.Fatalf("get hwinfo: %v", err)
	}
	if got.Runtime.KernelVersion != "6.8.0-00-generic" || len(got.Disks) != 1 || got.Disks[0].ByID[0] != "/dev/disk/by-id/nvme-GOMI_TEST" || got.NICs[0].State != "up" {
		t.Fatalf("inventory was not stored: %+v", got)
	}
	updated, err := machineSvc.Get(context.Background(), "bm-inventory")
	if err != nil {
		t.Fatalf("get machine: %v", err)
	}
	if updated.Provision == nil || updated.Provision.AttemptID == "" || updated.Provision.InventoryID != "bm-inventory-hwinfo" {
		t.Fatalf("provision progress not updated: %+v", updated.Provision)
	}
	if got.AttemptID != updated.Provision.AttemptID {
		t.Fatalf("expected inventory attempt id %q, got %q", updated.Provision.AttemptID, got.AttemptID)
	}
	if len(updated.Provision.Timings) != 3 {
		t.Fatalf("expected inventory server timings, got %#v", updated.Provision.Timings)
	}
	if updated.Provision.Timings[1].Name != "server.inventory.store" || updated.Provision.Timings[1].Source != "server" {
		t.Fatalf("expected inventory store timing, got %#v", updated.Provision.Timings)
	}
}

func TestPXEInventory_StoresAPIInventoryJSON(t *testing.T) {
	backend := memory.New()
	machineSvc := machine.NewService(backend.Machines())
	hwInfoSvc := hwinfo.NewService(backend.HWInfo())
	now := time.Now().UTC()

	target := machine.Machine{
		Name:     "bm-json-inventory",
		Hostname: "bm-json-inventory",
		MAC:      "52:54:00:aa:bb:04",
		Arch:     "amd64",
		Firmware: machine.FirmwareUEFI,
		Phase:    machine.PhaseProvisioning,
		Provision: &machine.ProvisionProgress{
			Active:          true,
			AttemptID:       "attempt-json-inventory",
			CompletionToken: "token-json-inventory",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.Machines().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}

	payload, err := json.Marshal(apiinventory.HardwareInventory{
		Runtime: apiinventory.RuntimeInfo{KernelVersion: "6.8.0-31-generic"},
		Boot:    apiinventory.BootInfo{FirmwareMode: "uefi", EFIVars: true},
		Disks: []apiinventory.DiskInfo{{
			Name:   "vda",
			Path:   "/dev/vda",
			SizeMB: 32768,
			ByID:   []string{"/dev/disk/by-id/virtio-root"},
		}},
		NICs: []apiinventory.NICInfo{{
			Name:     "ens3",
			MAC:      "52:54:00:aa:bb:04",
			State:    "up",
			Driver:   "virtio_net",
			Modalias: "pci:v00001AF4d00001000",
		}},
	})
	if err != nil {
		t.Fatalf("marshal inventory: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/pxe/inventory?token=token-json-inventory", strings.NewReader(string(payload)))
	req.Header.Set("Content-Type", "application/json")
	req.Host = "192.168.2.254:8080"
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h := &Handler{machines: machineSvc, hwinfo: hwInfoSvc}
	if err := h.PXEInventory(c); err != nil {
		t.Fatalf("PXEInventory: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}
	var response map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode inventory response: %v", err)
	}
	if response["curtinConfigUrl"] == "" || response["eventsUrl"] == "" {
		t.Fatalf("unexpected inventory response: %+v", response)
	}
	got, err := hwInfoSvc.Get(context.Background(), "bm-json-inventory")
	if err != nil {
		t.Fatalf("get hwinfo: %v", err)
	}
	if got.Runtime.KernelVersion != "6.8.0-31-generic" || got.Disks[0].Path != "/dev/vda" || got.NICs[0].Driver != "virtio_net" {
		t.Fatalf("inventory JSON was not converted: %+v", got)
	}
}

func TestPXEInventory_QCOW2ImageReturnsDiskImageDeployPlan(t *testing.T) {
	backend := memory.New()
	machineSvc := machine.NewService(backend.Machines())
	hwInfoSvc := hwinfo.NewService(backend.HWInfo())
	osImageSvc := osimage.NewService(backend.OSImages())
	now := time.Now().UTC()
	imageRef := "debian-13-amd64-qcow2"

	if err := backend.OSImages().Upsert(context.Background(), osimage.OSImage{
		Name:      imageRef,
		OSFamily:  "debian",
		OSVersion: "13",
		Arch:      "amd64",
		Format:    osimage.FormatQCOW2,
		Source:    osimage.SourceURL,
		Ready:     true,
		LocalPath: "/var/lib/gomi/data/images/debian-13-amd64-qcow2",
		Manifest: &osimage.Manifest{
			Capabilities: osimage.Capabilities{DeployTargets: []osimage.DeploymentTarget{osimage.DeploymentTargetBareMetal}},
			Root: osimage.RootArtifact{
				Format: osimage.FormatQCOW2,
				Path:   "root.qcow2",
				SHA256: "sha256:abc123",
				RootPartition: osimage.Partition{
					Number:     1,
					Filesystem: "ext4",
				},
				EFIPartition: &osimage.Partition{Number: 15, Filesystem: "vfat"},
			},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("upsert os image: %v", err)
	}
	target := machine.Machine{
		Name:     "bm-qcow2-inventory",
		Hostname: "bm-qcow2-inventory",
		MAC:      "52:54:00:aa:bb:05",
		Arch:     "amd64",
		Firmware: machine.FirmwareUEFI,
		OSPreset: machine.OSPreset{
			Family:   machine.OSTypeDebian,
			Version:  "13",
			ImageRef: imageRef,
		},
		Phase: machine.PhaseProvisioning,
		Provision: &machine.ProvisionProgress{
			Active:          true,
			AttemptID:       "attempt-qcow2-inventory",
			CompletionToken: "token-qcow2-inventory",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.Machines().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}

	payload, err := json.Marshal(apiinventory.HardwareInventory{
		Runtime: apiinventory.RuntimeInfo{KernelVersion: "6.8.0-31-generic"},
		Boot:    apiinventory.BootInfo{FirmwareMode: "uefi", EFIVars: true},
		Disks: []apiinventory.DiskInfo{{
			Name:   "nvme0n1",
			Path:   "/dev/nvme0n1",
			SizeMB: 65536,
			Type:   "disk",
		}},
		NICs: []apiinventory.NICInfo{{Name: "enp1s0", MAC: "52:54:00:aa:bb:05", State: "up"}},
	})
	if err != nil {
		t.Fatalf("marshal inventory: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/pxe/inventory?token=token-qcow2-inventory", strings.NewReader(string(payload)))
	req.Header.Set("Content-Type", "application/json")
	req.Host = "192.168.2.254:8080"
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h := &Handler{machines: machineSvc, hwinfo: hwInfoSvc, osimages: osImageSvc}
	if err := h.PXEInventory(c); err != nil {
		t.Fatalf("PXEInventory: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}
	var response map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode inventory response: %v", err)
	}
	if response["deployMode"] != "disk-image" {
		t.Fatalf("deployMode = %v, want disk-image; body=%s", response["deployMode"], rec.Body.String())
	}
	if _, ok := response["curtinConfigUrl"]; ok {
		t.Fatalf("disk-image response must not include curtinConfigUrl: %s", rec.Body.String())
	}
	deploy, ok := response["diskImageDeploy"].(map[string]any)
	if !ok {
		t.Fatalf("missing diskImageDeploy: %s", rec.Body.String())
	}
	if deploy["format"] != "qcow2" || deploy["targetDisk"] != "/dev/nvme0n1" || deploy["rootPartitionNumber"].(float64) != 1 {
		t.Fatalf("unexpected diskImageDeploy: %#v", deploy)
	}
	if deploy["osFamily"] != "debian" || deploy["osVersion"] != "13" {
		t.Fatalf("disk-image response must include OS metadata for runner quirks: %#v", deploy)
	}
	if _, ok := deploy["sha256"]; ok {
		t.Fatalf("disk-image response must not force runner to download qcow2 into tmpfs: %#v", deploy)
	}
	if !strings.Contains(deploy["imageUrl"].(string), "/pxe/artifacts/os-images/debian-13-amd64-qcow2/root.qcow2") {
		t.Fatalf("unexpected image URL: %#v", deploy["imageUrl"])
	}
	if deploy["seedUrl"] != "http://192.168.2.254:8080/pxe/nocloud/525400aabb05" {
		t.Fatalf("unexpected seed URL: %#v", deploy["seedUrl"])
	}
}

func TestPXEInventory_SquashFSImageReturnsCurtinPlan(t *testing.T) {
	backend := memory.New()
	machineSvc := machine.NewService(backend.Machines())
	hwInfoSvc := hwinfo.NewService(backend.HWInfo())
	osImageSvc := osimage.NewService(backend.OSImages())
	now := time.Now().UTC()
	imageRef := "debian-13-amd64-squashfs"

	if err := backend.OSImages().Upsert(context.Background(), osimage.OSImage{
		Name:      imageRef,
		OSFamily:  "debian",
		OSVersion: "13",
		Arch:      "amd64",
		Format:    osimage.FormatSquashFS,
		Source:    osimage.SourceURL,
		Ready:     true,
		LocalPath: "/var/lib/gomi/data/images/debian-13-amd64-squashfs",
		Manifest: &osimage.Manifest{
			Capabilities: osimage.Capabilities{DeployTargets: []osimage.DeploymentTarget{osimage.DeploymentTargetBareMetal}},
			Root: osimage.RootArtifact{
				Format: osimage.FormatSquashFS,
				Path:   "rootfs.squashfs",
			},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("upsert os image: %v", err)
	}
	target := machine.Machine{
		Name:     "bm-squashfs-inventory",
		Hostname: "bm-squashfs-inventory",
		MAC:      "52:54:00:aa:bb:08",
		Arch:     "amd64",
		Firmware: machine.FirmwareUEFI,
		OSPreset: machine.OSPreset{
			Family:   machine.OSTypeDebian,
			Version:  "13",
			ImageRef: imageRef,
		},
		Phase: machine.PhaseProvisioning,
		Provision: &machine.ProvisionProgress{
			Active:          true,
			AttemptID:       "attempt-squashfs-inventory",
			CompletionToken: "token-squashfs-inventory",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.Machines().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}
	payload := `{"runtime":{"kernelVersion":"6.8"},"disks":[{"name":"vda","path":"/dev/vda","sizeMB":32768,"type":"disk"}]}`

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/pxe/inventory?token=token-squashfs-inventory", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Host = "192.168.2.254:8080"
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h := &Handler{machines: machineSvc, hwinfo: hwInfoSvc, osimages: osImageSvc}
	if err := h.PXEInventory(c); err != nil {
		t.Fatalf("PXEInventory: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}
	var response map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode inventory response: %v", err)
	}
	if response["deployMode"] != "curtin" {
		t.Fatalf("deployMode = %v, want curtin; body=%s", response["deployMode"], rec.Body.String())
	}
	if response["curtinConfigUrl"] == "" || response["eventsUrl"] == "" {
		t.Fatalf("expected curtin plan URLs, got: %+v", response)
	}
	if _, ok := response["diskImageDeploy"]; ok {
		t.Fatalf("squashfs curtin plan must not include diskImageDeploy: %s", rec.Body.String())
	}
}

func TestPXEInventory_NonBareMetalQCOW2ImageRejectsBareMetalDeploy(t *testing.T) {
	backend := memory.New()
	machineSvc := machine.NewService(backend.Machines())
	hwInfoSvc := hwinfo.NewService(backend.HWInfo())
	osImageSvc := osimage.NewService(backend.OSImages())
	now := time.Now().UTC()
	imageRef := "debian-13-amd64-cloud-qcow2"

	if err := backend.OSImages().Upsert(context.Background(), osimage.OSImage{
		Name:      imageRef,
		OSFamily:  "debian",
		OSVersion: "13",
		Arch:      "amd64",
		Format:    osimage.FormatQCOW2,
		Source:    osimage.SourceURL,
		Ready:     true,
		URL:       "https://cloud.debian.org/images/cloud/bookworm/latest/debian-13-genericcloud-amd64.qcow2",
		Manifest: &osimage.Manifest{
			Root: osimage.RootArtifact{Format: osimage.FormatQCOW2, Path: "root.qcow2"},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("upsert os image: %v", err)
	}
	target := machine.Machine{
		Name:     "cloud-qcow2-inventory",
		Hostname: "cloud-qcow2-inventory",
		MAC:      "52:54:00:aa:bb:07",
		Arch:     "amd64",
		Firmware: machine.FirmwareUEFI,
		OSPreset: machine.OSPreset{
			Family:   machine.OSTypeDebian,
			Version:  "13",
			ImageRef: imageRef,
		},
		Phase: machine.PhaseProvisioning,
		Provision: &machine.ProvisionProgress{
			Active:          true,
			AttemptID:       "attempt-cloud-qcow2-inventory",
			CompletionToken: "token-cloud-qcow2-inventory",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.Machines().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}
	payload := `{"runtime":{"kernelVersion":"6.8"},"disks":[{"name":"vda","path":"/dev/vda","sizeMB":32768,"type":"disk"}]}`

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/pxe/inventory?token=token-cloud-qcow2-inventory", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Host = "192.168.2.254:8080"
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h := &Handler{machines: machineSvc, hwinfo: hwInfoSvc, osimages: osImageSvc}
	if err := h.PXEInventory(c); err != nil {
		t.Fatalf("PXEInventory: %v", err)
	}
	if rec.Code != http.StatusConflict {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "does not support bare-metal deployment") {
		t.Fatalf("expected bare-metal capability error, got: %s", rec.Body.String())
	}
}

func TestPXEInventory_QCOW2ImageRequiresRootPartitionManifest(t *testing.T) {
	backend := memory.New()
	machineSvc := machine.NewService(backend.Machines())
	hwInfoSvc := hwinfo.NewService(backend.HWInfo())
	osImageSvc := osimage.NewService(backend.OSImages())
	now := time.Now().UTC()
	imageRef := "debian-13-amd64-qcow2-bad"

	if err := backend.OSImages().Upsert(context.Background(), osimage.OSImage{
		Name:      imageRef,
		OSFamily:  "debian",
		OSVersion: "13",
		Arch:      "amd64",
		Format:    osimage.FormatQCOW2,
		Source:    osimage.SourceURL,
		Variant:   osimage.VariantBareMetal,
		Ready:     true,
		LocalPath: "/var/lib/gomi/data/images/debian-13-amd64-qcow2-bad",
		Manifest: &osimage.Manifest{
			Root: osimage.RootArtifact{Format: osimage.FormatQCOW2, Path: "root.qcow2"},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("upsert os image: %v", err)
	}
	target := machine.Machine{
		Name:     "bm-qcow2-bad-inventory",
		Hostname: "bm-qcow2-bad-inventory",
		MAC:      "52:54:00:aa:bb:06",
		Arch:     "amd64",
		Firmware: machine.FirmwareUEFI,
		OSPreset: machine.OSPreset{
			Family:   machine.OSTypeDebian,
			Version:  "13",
			ImageRef: imageRef,
		},
		Phase: machine.PhaseProvisioning,
		Provision: &machine.ProvisionProgress{
			Active:          true,
			AttemptID:       "attempt-qcow2-bad-inventory",
			CompletionToken: "token-qcow2-bad-inventory",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.Machines().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}
	payload := `{"runtime":{"kernelVersion":"6.8"},"disks":[{"name":"vda","path":"/dev/vda","sizeMB":32768,"type":"disk"}]}`

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/pxe/inventory?token=token-qcow2-bad-inventory", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Host = "192.168.2.254:8080"
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h := &Handler{machines: machineSvc, hwinfo: hwInfoSvc, osimages: osImageSvc}
	if err := h.PXEInventory(c); err != nil {
		t.Fatalf("PXEInventory: %v", err)
	}
	if rec.Code != http.StatusConflict {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "manifest.root.rootPartition.number") {
		t.Fatalf("expected root partition manifest error, got: %s", rec.Body.String())
	}
}
