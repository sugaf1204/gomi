package pxehttp

import (
	"context"
	"github.com/labstack/echo/v4"
	"github.com/sugaf1204/gomi/internal/infra/memory"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/vm"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPXEBootScript(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/pxe/boot.ipxe", nil)
	req.Host = "192.168.2.254:8080"
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h := &Handler{}
	if err := h.PXEBootScript(c); err != nil {
		t.Fatalf("PXEBootScript: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "#!ipxe") {
		t.Fatalf("expected ipxe shebang, got: %s", body)
	}
	if !strings.Contains(body, "set base http://192.168.2.254:8080/pxe") {
		t.Fatalf("expected base URL in script, got: %s", body)
	}
	if !strings.Contains(body, "url=${base}/preseed.cfg") {
		t.Fatalf("expected preseed URL in script, got: %s", body)
	}
	if !strings.Contains(body, "initrd=initrd.gz") {
		t.Fatalf("expected explicit initrd kernel argument, got: %s", body)
	}
	if !strings.Contains(body, "initrd --name initrd.gz ${base}/files/debian/initrd.gz") {
		t.Fatalf("expected initrd line in script, got: %s", body)
	}
}

func TestPXEFile(t *testing.T) {
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, "debian"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "debian", "linux"), []byte("kernel"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/pxe/files/debian/linux", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("*")
	c.SetParamValues("debian/linux")

	h := &Handler{pxeFilesDir: tmp}
	if err := h.PXEFile(c); err != nil {
		t.Fatalf("PXEFile: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	if rec.Body.String() != "kernel" {
		t.Fatalf("unexpected body: %q", rec.Body.String())
	}
}

func TestPXEFileRecordsProvisionedTransferTiming(t *testing.T) {
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, "images"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "images", "rootfs.squashfs"), []byte("root"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	backend := memory.New()
	machineSvc := machine.NewService(backend.Machines())
	now := time.Now().UTC()
	target := machine.Machine{
		Name:     "bm-transfer",
		Hostname: "bm-transfer",
		MAC:      "52:54:00:aa:bb:99",
		Arch:     "amd64",
		Firmware: machine.FirmwareUEFI,
		Phase:    machine.PhaseProvisioning,
		Provision: &machine.ProvisionProgress{
			Active:          true,
			AttemptID:       "attempt-transfer",
			CompletionToken: "token-transfer",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.Machines().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/pxe/files/images/rootfs.squashfs?attempt_id=attempt-transfer&token=token-transfer", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("*")
	c.SetParamValues("images/rootfs.squashfs")

	h := &Handler{pxeFilesDir: tmp, machines: machineSvc}
	if err := h.PXEFile(c); err != nil {
		t.Fatalf("PXEFile: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}
	stored, err := backend.Machines().Get(context.Background(), target.Name)
	if err != nil {
		t.Fatalf("get machine: %v", err)
	}
	if len(stored.Provision.Timings) != 1 {
		t.Fatalf("expected one transfer timing, got %#v", stored.Provision.Timings)
	}
	timing := stored.Provision.Timings[0]
	if timing.Name != "server.file_transfer" || timing.Source != "server" || timing.Result != "success" {
		t.Fatalf("unexpected transfer timing: %#v", timing)
	}
	if !strings.Contains(timing.Message, "4 bytes") {
		t.Fatalf("expected served size in timing message, got %q", timing.Message)
	}
}

func TestSanitizePXEPath(t *testing.T) {
	if _, err := sanitizePXEPath("../../etc/passwd"); err == nil {
		t.Fatal("expected path traversal to be rejected")
	}
	if _, err := sanitizePXEPath(".."); err == nil {
		t.Fatal("expected literal parent path to be rejected")
	}
	if got, err := sanitizePXEPath("debian/initrd.gz"); err != nil || got != "debian/initrd.gz" {
		t.Fatalf("unexpected sanitize result: %q err=%v", got, err)
	}
}

func TestPXEBootScript_LocalBootWhenVMNotProvisioning(t *testing.T) {
	backend := memory.New()
	vmSvc := vm.NewService(backend.VMs())
	now := time.Now().UTC()
	target := vm.VirtualMachine{
		Name:          "vm-pxe",
		HypervisorRef: "hv-01",
		Resources:     vm.ResourceSpec{CPUCores: 2, MemoryMB: 2048, DiskGB: 20},
		Phase:         vm.PhaseStopped,
		NetworkInterfaces: []vm.NetworkInterfaceStatus{
			{MAC: "52:54:00:aa:bb:cc"},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.VMs().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert vm: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/pxe/boot.ipxe?mac=52:54:00:aa:bb:cc", nil)
	req.Host = "192.168.2.254:8080"
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h := &Handler{vms: vmSvc}
	if err := h.PXEBootScript(c); err != nil {
		t.Fatalf("PXEBootScript: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "iseq ${platform} efi && goto local_efi || goto local_bios") ||
		!strings.Contains(body, "sanboot --no-describe --drive 0 || exit 1") {
		t.Fatalf("expected UEFI local boot to use arch-neutral sanboot with firmware fallback, got: %s", body)
	}
	if strings.Contains(body, "BOOTX64.EFI") {
		t.Fatalf("UEFI local boot must not force an x86-only EFI filename, got: %s", body)
	}
	if strings.Contains(body, "grubnetx64.efi") {
		t.Fatalf("UEFI local boot must not chain network GRUB, got: %s", body)
	}
	if !strings.Contains(body, "sanboot --no-describe --drive 0x80") {
		t.Fatalf("expected local disk sanboot command, got: %s", body)
	}
	if !strings.Contains(body, "|| exit") {
		t.Fatalf("expected exit fallback in local boot script, got: %s", body)
	}
}

func TestPXEBootScript_LocalBootWhenNoProvisioningTarget(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/pxe/boot.ipxe?mac=52:54:00:aa:bb:cc", nil)
	req.Host = "192.168.2.254:8080"
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h := &Handler{}
	if err := h.PXEBootScript(c); err != nil {
		t.Fatalf("PXEBootScript: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "iseq ${platform} efi && goto local_efi || goto local_bios") ||
		!strings.Contains(body, "sanboot --no-describe --drive 0 || exit 1") {
		t.Fatalf("expected UEFI local boot to use arch-neutral sanboot with firmware fallback, got: %s", body)
	}
	if strings.Contains(body, "BOOTX64.EFI") {
		t.Fatalf("UEFI local boot must not force an x86-only EFI filename, got: %s", body)
	}
	if strings.Contains(body, "grubnetx64.efi") {
		t.Fatalf("UEFI local boot must not chain network GRUB, got: %s", body)
	}
	if !strings.Contains(body, "sanboot --no-describe --drive 0x80") {
		t.Fatalf("expected local boot script for non-provisioning MAC, got: %s", body)
	}
}

func TestPXEBootScript_CurtinScriptWhenLinuxMachineProvisioning(t *testing.T) {
	backend := memory.New()
	machineSvc := machine.NewService(backend.Machines())
	now := time.Now().UTC()

	target := machine.Machine{
		Name:     "bm-01",
		Hostname: "bm-01",
		MAC:      "52:54:00:77:88:99",
		Arch:     "amd64",
		Firmware: machine.FirmwareBIOS,
		OSPreset: machine.OSPreset{
			Family: machine.OSTypeDebian,
		},
		Phase: machine.PhaseProvisioning,
		Provision: &machine.ProvisionProgress{
			Active:          true,
			CompletionToken: "token-machine-debian",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.Machines().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/pxe/boot.ipxe?mac=52:54:00:77:88:99", nil)
	req.Host = "192.168.2.254:8080"
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h := &Handler{machines: machineSvc}
	if err := h.PXEBootScript(c); err != nil {
		t.Fatalf("PXEBootScript: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "files/linux/boot-kernel") {
		t.Fatalf("expected linux boot-kernel path for provisioning machine, got: %s", body)
	}
	if !strings.Contains(body, "files/linux/boot-initrd") {
		t.Fatalf("expected linux boot-initrd path for provisioning machine, got: %s", body)
	}
	if !strings.Contains(body, "root=squash:${base}/files/linux/rootfs.squashfs") {
		t.Fatalf("expected linux squashfs rootfs path for provisioning machine, got: %s", body)
	}
	if !strings.Contains(body, "gomi.token=token-machine-debian") {
		t.Fatalf("expected provision token in curtin script, got: %s", body)
	}
	if strings.Contains(body, "preseed.cfg") {
		t.Fatalf("machine provisioning should not use preseed by OS family, got: %s", body)
	}
}

func TestPXEBootScript_CurtinForUbuntuVMUsesLocalBootScript(t *testing.T) {
	backend := memory.New()
	vmSvc := vm.NewService(backend.VMs())
	now := time.Now().UTC()
	target := vm.VirtualMachine{
		Name:          "vm-auto",
		HypervisorRef: "hv-01",
		Resources:     vm.ResourceSpec{CPUCores: 2, MemoryMB: 2048, DiskGB: 20},
		Network: []vm.NetworkInterface{
			{MAC: "52:54:00:44:55:66"},
		},
		InstallCfg: &vm.InstallConfig{
			Type: vm.InstallConfigCurtin,
		},
		Phase: vm.PhaseProvisioning,
		Provisioning: vm.ProvisioningStatus{
			Active:          true,
			CompletionToken: "token-curtin-vm",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.VMs().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert vm: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/pxe/boot.ipxe?mac=52:54:00:44:55:66", nil)
	req.Host = "192.168.2.254:8080"
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h := &Handler{vms: vmSvc}
	if err := h.PXEBootScript(c); err != nil {
		t.Fatalf("PXEBootScript: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "iseq ${platform} efi && goto local_efi || goto local_bios") ||
		!strings.Contains(body, "sanboot --no-describe --drive 0 || exit 1") {
		t.Fatalf("expected UEFI local boot to use arch-neutral sanboot with firmware fallback for curtin, got: %s", body)
	}
	if strings.Contains(body, "BOOTX64.EFI") {
		t.Fatalf("UEFI local boot must not force an x86-only EFI filename for curtin, got: %s", body)
	}
	if strings.Contains(body, "grubnetx64.efi") {
		t.Fatalf("UEFI local boot must not chain network GRUB for curtin, got: %s", body)
	}
	if strings.Contains(body, "iso-url=") {
		t.Fatalf("did not expect installer kernel args in curtin boot script, got: %s", body)
	}
}

func TestRenderPXENoCloudLineConfig_Curtin(t *testing.T) {
	got := RenderNoCloudLineConfig("http://192.168.2.254:8080/pxe", vm.InstallConfigCurtin, "52:54:00:44:55:66")
	want := "ds=nocloud;s=http://192.168.2.254:8080/pxe/nocloud/525400445566/"
	if got != want {
		t.Fatalf("unexpected line config: got=%q want=%q", got, want)
	}
}

func TestRenderPXENoCloudLineConfig_PreseedEmpty(t *testing.T) {
	got := RenderNoCloudLineConfig("http://192.168.2.254:8080/pxe", vm.InstallConfigPreseed, "52:54:00:44:55:66")
	if got != "" {
		t.Fatalf("expected empty line config for preseed, got=%q", got)
	}
}

func TestWithDeployCloudInitDefaultsDisablesPackageBackedLocaleAndResize(t *testing.T) {
	got := withDeployCloudInitDefaults("#cloud-config\nhostname: test-node\n", true)
	for _, want := range []string{
		"hostname: test-node",
		"locale: false",
		"resize_rootfs: false",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected rendered cloud-init to contain %q, got:\n%s", want, got)
		}
	}
}

func TestWithDeployCloudInitDefaultsKeepsResizeRootfsByDefault(t *testing.T) {
	got := withDeployCloudInitDefaults("#cloud-config\nhostname: test-node\n", false)
	for _, want := range []string{
		"hostname: test-node",
		"locale: false",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected rendered cloud-init to contain %q, got:\n%s", want, got)
		}
	}
	if strings.Contains(got, "resize_rootfs: false") {
		t.Fatalf("resize_rootfs must not be disabled for non-completed-rootfs deploys, got:\n%s", got)
	}
}

func TestWithDeployCloudInitDefaultsPreservesJinjaHeader(t *testing.T) {
	got := withDeployCloudInitDefaults("## template: jinja\n#cloud-config\nhostname: '{{ ds.meta_data.local_hostname }}'\n", true)
	if !strings.HasPrefix(got, "## template: jinja\n#cloud-config\n") {
		t.Fatalf("expected jinja and cloud-config headers to be preserved, got:\n%s", got)
	}
	for _, want := range []string{
		"hostname: '{{ ds.meta_data.local_hostname }}'",
		"locale: false",
		"resize_rootfs: false",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected rendered cloud-init to contain %q, got:\n%s", want, got)
		}
	}
}
