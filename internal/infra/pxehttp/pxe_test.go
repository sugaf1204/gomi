package pxehttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"

	apiinventory "github.com/sugaf1204/gomi/api/inventory"
	"github.com/sugaf1204/gomi/internal/cloudinit"
	"github.com/sugaf1204/gomi/internal/hwinfo"
	"github.com/sugaf1204/gomi/internal/hypervisor"
	"github.com/sugaf1204/gomi/internal/infra/memory"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/osimage"
	"github.com/sugaf1204/gomi/internal/power"
	"github.com/sugaf1204/gomi/internal/subnet"
	"github.com/sugaf1204/gomi/internal/vm"
)

type bootOrderCall struct {
	machine power.MachineInfo
	order   power.BootOrder
}

type stubPowerExecutor struct {
	calls []bootOrderCall
	err   error
}

func (s *stubPowerExecutor) ConfigureBootOrder(_ context.Context, m power.MachineInfo, order power.BootOrder) error {
	copied := append(power.BootOrder(nil), order...)
	s.calls = append(s.calls, bootOrderCall{machine: m, order: copied})
	return s.err
}

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
	if !strings.Contains(body, "iseq ${platform} efi && exit 1 ||") {
		t.Fatalf("expected UEFI local boot to return to firmware, got: %s", body)
	}
	if !strings.Contains(body, "chain --autofree tftp://${next-server}/grubnetx64.efi") {
		t.Fatalf("expected UEFI local boot to chain network GRUB over TFTP, got: %s", body)
	}
	if !strings.Contains(body, "chain --autofree http://192.168.2.254:8080/pxe/files/grubnetx64.efi") {
		t.Fatalf("expected UEFI local boot to chain network GRUB over HTTP fallback, got: %s", body)
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
	if !strings.Contains(body, "iseq ${platform} efi && exit 1 ||") {
		t.Fatalf("expected UEFI local boot to return to firmware, got: %s", body)
	}
	if !strings.Contains(body, "chain --autofree tftp://${next-server}/grubnetx64.efi") {
		t.Fatalf("expected UEFI local boot to chain network GRUB over TFTP, got: %s", body)
	}
	if !strings.Contains(body, "chain --autofree http://192.168.2.254:8080/pxe/files/grubnetx64.efi") {
		t.Fatalf("expected UEFI local boot to chain network GRUB over HTTP fallback, got: %s", body)
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
	if !strings.Contains(body, "chain --autofree tftp://${next-server}/grubnetx64.efi") {
		t.Fatalf("expected local boot to chain network GRUB for curtin, got: %s", body)
	}
	if strings.Contains(body, "iso-url=") {
		t.Fatalf("did not expect installer kernel args in curtin boot script, got: %s", body)
	}
}

func TestRenderPXENoCloudLineConfig_Curtin(t *testing.T) {
	got := RenderNoCloudLineConfig("http://192.168.2.254:8080/pxe", vm.InstallConfigCurtin, "52:54:00:44:55:66")
	want := "ds=nocloud-net;s=http://192.168.2.254:8080/pxe/nocloud/525400445566/"
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

func TestPXEPreseed_CustomInlineByMAC(t *testing.T) {
	backend := memory.New()
	vmSvc := vm.NewService(backend.VMs())
	now := time.Now().UTC()
	target := vm.VirtualMachine{
		Name:          "vm-preseed",
		HypervisorRef: "hv-01",
		Resources:     vm.ResourceSpec{CPUCores: 2, MemoryMB: 2048, DiskGB: 20},
		InstallCfg: &vm.InstallConfig{
			Type:   vm.InstallConfigPreseed,
			Inline: "d-i passwd/username string customuser",
		},
		Phase: vm.PhaseProvisioning,
		Provisioning: vm.ProvisioningStatus{
			Active:          true,
			CompletionToken: "token-preseed-custom",
		},
		NetworkInterfaces: []vm.NetworkInterfaceStatus{
			{MAC: "52:54:00:11:22:33"},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.VMs().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert vm: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/pxe/preseed.cfg?mac=52:54:00:11:22:33", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h := &Handler{vms: vmSvc}
	if err := h.PXEPreseed(c); err != nil {
		t.Fatalf("PXEPreseed: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	got := rec.Body.String()
	if !strings.Contains(got, "customuser") {
		t.Fatalf("expected custom preseed, got: %s", got)
	}
	if !strings.Contains(got, "install-complete?token=token-preseed-custom&type=preseed") {
		t.Fatalf("expected install-complete callback with token, got: %s", got)
	}
	if strings.Contains(got, "exit/poweroff") {
		t.Fatalf("expected poweroff to be removed, got: %s", got)
	}
	if !strings.Contains(got, "exit/reboot") {
		t.Fatalf("expected reboot directive, got: %s", got)
	}
}

func TestPXEPreseed_DefaultIncludesSudoAndBaseTools(t *testing.T) {
	backend := memory.New()
	vmSvc := vm.NewService(backend.VMs())
	now := time.Now().UTC()
	target := vm.VirtualMachine{
		Name:          "vm-default-preseed",
		HypervisorRef: "hv-01",
		Resources:     vm.ResourceSpec{CPUCores: 2, MemoryMB: 2048, DiskGB: 20},
		InstallCfg: &vm.InstallConfig{
			Type: vm.InstallConfigPreseed,
		},
		Network: []vm.NetworkInterface{
			{MAC: "52:54:00:11:22:44"},
		},
		Phase: vm.PhaseProvisioning,
		Provisioning: vm.ProvisioningStatus{
			Active:          true,
			CompletionToken: "token-preseed-default",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.VMs().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert vm: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/pxe/preseed.cfg?mac=52:54:00:11:22:44", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h := &Handler{vms: vmSvc}
	if err := h.PXEPreseed(c); err != nil {
		t.Fatalf("PXEPreseed: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	body := rec.Body.String()
	for _, pkg := range []string{"qemu-guest-agent", "sudo", "curl", "wget", "ca-certificates", "vim", "less", "net-tools", "iproute2", "git", "tmux", "htop", "dnsutils"} {
		if !strings.Contains(body, pkg) {
			t.Fatalf("expected package %q in default preseed package list, got: %s", pkg, body)
		}
	}
	// preseed is being phased out; the default no longer creates any user
	// (caller must supply username/keys via inline preseed). The hostname is
	// still injected from the target name.
	if !strings.Contains(body, "d-i passwd/make-user boolean false") {
		t.Fatalf("expected default preseed to disable user creation, got: %s", body)
	}
	if strings.Contains(body, "passwd/username string gomi") {
		t.Fatalf("default preseed must not create the legacy gomi user, got: %s", body)
	}
	if !strings.Contains(body, "d-i netcfg/get_hostname string vm-default-preseed") {
		t.Fatalf("expected preseed hostname to follow vm name, got: %s", body)
	}
}

func TestPXEPreseed_UsesMachineProvisioningToken(t *testing.T) {
	backend := memory.New()
	machineSvc := machine.NewService(backend.Machines())
	now := time.Now().UTC()
	target := machine.Machine{
		Name:     "bm-preseed",
		Hostname: "bm-preseed",
		MAC:      "52:54:00:99:88:77",
		Arch:     "amd64",
		Firmware: machine.FirmwareBIOS,
		OSPreset: machine.OSPreset{
			Family: machine.OSTypeDebian,
		},
		Phase: machine.PhaseProvisioning,
		Provision: &machine.ProvisionProgress{
			Active:          true,
			CompletionToken: "token-machine-preseed",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.Machines().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/pxe/preseed.cfg?mac=52:54:00:99:88:77", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h := &Handler{machines: machineSvc}
	if err := h.PXEPreseed(c); err != nil {
		t.Fatalf("PXEPreseed: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	got := rec.Body.String()
	if !strings.Contains(got, "install-complete?token=token-machine-preseed&type=preseed") {
		t.Fatalf("expected machine token callback in preseed, got: %s", got)
	}
	if !strings.Contains(got, "d-i netcfg/get_hostname string bm-preseed") {
		t.Fatalf("expected machine hostname in preseed, got: %s", got)
	}
}

func TestPXENocloudUserData_InjectsInstallCompleteToken(t *testing.T) {
	backend := memory.New()
	vmSvc := vm.NewService(backend.VMs())
	now := time.Now().UTC()
	target := vm.VirtualMachine{
		Name:          "vm-auto",
		HypervisorRef: "hv-01",
		Resources:     vm.ResourceSpec{CPUCores: 2, MemoryMB: 2048, DiskGB: 20},
		InstallCfg: &vm.InstallConfig{
			Type:   vm.InstallConfigCurtin,
			Inline: "#cloud-config\nhostname: vm-auto\npackage_update: true\n",
		},
		Phase: vm.PhaseProvisioning,
		Provisioning: vm.ProvisioningStatus{
			Active:          true,
			CompletionToken: "token-curtin",
		},
		NetworkInterfaces: []vm.NetworkInterfaceStatus{
			{MAC: "52:54:00:44:55:66"},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.VMs().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert vm: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/pxe/nocloud/525400445566/user-data", nil)
	req.Host = "192.168.2.254:8080"
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("mac")
	c.SetParamValues("525400445566")

	h := &Handler{vms: vmSvc}
	if err := h.PXENocloudUserData(c); err != nil {
		t.Fatalf("PXENocloudUserData: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "install-complete?token=token-curtin&type=curtin") {
		t.Fatalf("expected install-complete callback in curtin, got: %s", body)
	}
	if !strings.Contains(body, "package_update: true") {
		t.Fatalf("expected package_update in curtin user-data, got: %s", body)
	}
	if !strings.Contains(body, "hostname: vm-auto") {
		t.Fatalf("expected hostname to follow VM name, got: %s", body)
	}
	if strings.Contains(body, "hostname: gomi-pxe") {
		t.Fatalf("expected gomi-pxe hostname to be replaced, got: %s", body)
	}
}

func TestPXENocloudUserData_CurtinUsesCurtinCompletionType(t *testing.T) {
	backend := memory.New()
	vmSvc := vm.NewService(backend.VMs())
	now := time.Now().UTC()
	target := vm.VirtualMachine{
		Name:          "vm-curtin",
		HypervisorRef: "hv-01",
		Resources:     vm.ResourceSpec{CPUCores: 2, MemoryMB: 2048, DiskGB: 20},
		InstallCfg: &vm.InstallConfig{
			Type: vm.InstallConfigCurtin,
		},
		Phase: vm.PhaseProvisioning,
		Provisioning: vm.ProvisioningStatus{
			Active:          true,
			CompletionToken: "token-curtin-user-data",
		},
		NetworkInterfaces: []vm.NetworkInterfaceStatus{
			{MAC: "52:54:00:aa:bb:70"},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.VMs().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert vm: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/pxe/nocloud/525400aabb70/user-data", nil)
	req.Host = "192.168.2.254:8080"
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("mac")
	c.SetParamValues("525400aabb70")

	h := &Handler{vms: vmSvc}
	if err := h.PXENocloudUserData(c); err != nil {
		t.Fatalf("PXENocloudUserData: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "install-complete?token=token-curtin-user-data&type=curtin") {
		t.Fatalf("expected install-complete callback type=curtin, got: %s", body)
	}
	// Password SSH login is forbidden across the deployment surface; cloud-init
	// must emit ssh_pwauth: false so sshd rejects password authentication.
	if !strings.Contains(body, "ssh_pwauth: false") {
		t.Fatalf("expected ssh_pwauth: false in curtin user-data, got: %s", body)
	}
	if !strings.Contains(body, "hostname: vm-curtin") {
		t.Fatalf("expected hostname to be vm-curtin, got: %s", body)
	}
	if strings.Contains(body, "hostname: gomi-pxe") {
		t.Fatalf("expected gomi-pxe hostname to be replaced, got: %s", body)
	}
}

func TestPXENocloudUserData_InjectsHostnameForMachine(t *testing.T) {
	backend := memory.New()
	machineSvc := machine.NewService(backend.Machines())
	now := time.Now().UTC()
	target := machine.Machine{
		Name:     "bm-ubuntu",
		Hostname: "my-server-01",
		MAC:      "52:54:00:dd:ee:ff",
		Arch:     "amd64",
		Firmware: machine.FirmwareUEFI,
		OSPreset: machine.OSPreset{
			Family: machine.OSTypeUbuntu,
		},
		Phase: machine.PhaseProvisioning,
		Provision: &machine.ProvisionProgress{
			Active:          true,
			CompletionToken: "token-hostname-test",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.Machines().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/pxe/nocloud/525400ddeeff/user-data", nil)
	req.Host = "192.168.2.254:8080"
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("mac")
	c.SetParamValues("525400ddeeff")

	h := &Handler{machines: machineSvc}
	if err := h.PXENocloudUserData(c); err != nil {
		t.Fatalf("PXENocloudUserData: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "hostname: my-server-01") {
		t.Fatalf("expected hostname my-server-01 in cloud-config, got: %s", body)
	}
	if strings.Contains(body, "hostname: gomi-pxe") {
		t.Fatalf("expected gomi-pxe hostname to be replaced, got: %s", body)
	}
}

func TestPXENocloudVendorData(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/pxe/nocloud/525400445566/vendor-data", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("mac")
	c.SetParamValues("525400445566")

	h := &Handler{}
	if err := h.PXENocloudVendorData(c); err != nil {
		t.Fatalf("PXENocloudVendorData: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "#cloud-config") {
		t.Fatalf("expected cloud-config vendor-data, got: %s", body)
	}
}

func TestPXENocloudMetaData_UsesVMName(t *testing.T) {
	backend := memory.New()
	vmSvc := vm.NewService(backend.VMs())
	now := time.Now().UTC()
	target := vm.VirtualMachine{
		Name:          "vm-meta",
		HypervisorRef: "hv-01",
		Resources:     vm.ResourceSpec{CPUCores: 2, MemoryMB: 2048, DiskGB: 20},
		InstallCfg:    &vm.InstallConfig{Type: vm.InstallConfigCurtin},
		Network: []vm.NetworkInterface{
			{MAC: "52:54:00:12:34:56"},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.VMs().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert vm: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/pxe/nocloud/525400123456/meta-data", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("mac")
	c.SetParamValues("525400123456")

	h := &Handler{vms: vmSvc}
	if err := h.PXENocloudMetaData(c); err != nil {
		t.Fatalf("PXENocloudMetaData: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "local-hostname: vm-meta") {
		t.Fatalf("expected metadata hostname to follow VM name, got: %s", body)
	}
	if strings.Contains(body, "local-hostname: gomi-pxe") {
		t.Fatalf("expected gomi-pxe metadata hostname to be replaced, got: %s", body)
	}
}

func TestPXEInstallComplete_ByToken(t *testing.T) {
	backend := memory.New()
	vmSvc := vm.NewService(backend.VMs())
	now := time.Now().UTC()
	deadline := now.Add(10 * time.Minute)
	target := vm.VirtualMachine{
		Name:          "vm-complete",
		HypervisorRef: "hv-missing",
		Resources:     vm.ResourceSpec{CPUCores: 2, MemoryMB: 2048, DiskGB: 20},
		Phase:         vm.PhaseProvisioning,
		Provisioning: vm.ProvisioningStatus{
			Active:          true,
			StartedAt:       &now,
			DeadlineAt:      &deadline,
			CompletionToken: "token-finish-01",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.VMs().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert vm: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/pxe/install-complete?token=token-finish-01&type=preseed", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h := &Handler{vms: vmSvc}
	if err := h.PXEInstallComplete(c); err != nil {
		t.Fatalf("PXEInstallComplete: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	vmBody, _ := payload["vm"].(map[string]any)
	if vmBody["phase"] != string(vm.PhaseRunning) {
		t.Fatalf("expected phase running after completion fallback, got %v", vmBody["phase"])
	}
	provisioning, _ := vmBody["provisioning"].(map[string]any)
	if active, _ := provisioning["active"].(bool); active {
		t.Fatalf("expected provisioning.active=false, got %v", provisioning["active"])
	}
	if provisioning["completedAt"] == nil {
		t.Fatalf("expected provisioning.completedAt to be set, got %v", provisioning)
	}
}

func TestPXEInstallComplete_ByToken_Machine(t *testing.T) {
	backend := memory.New()
	machineSvc := machine.NewService(backend.Machines())
	now := time.Now().UTC()
	target := machine.Machine{
		Name:     "bm-complete",
		Hostname: "bm-complete",
		MAC:      "52:54:00:66:66:66",
		Arch:     "amd64",
		Firmware: machine.FirmwareUEFI,
		OSPreset: machine.OSPreset{
			Family: machine.OSTypeUbuntu,
		},
		Phase: machine.PhaseProvisioning,
		Provision: &machine.ProvisionProgress{
			Active:          true,
			StartedAt:       &now,
			CompletionToken: "token-machine-finish-01",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.Machines().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/pxe/install-complete?token=token-machine-finish-01&type=curtin", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h := &Handler{machines: machineSvc}
	if err := h.PXEInstallComplete(c); err != nil {
		t.Fatalf("PXEInstallComplete: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	machineBody, _ := payload["machine"].(map[string]any)
	if machineBody["phase"] != string(machine.PhaseReady) {
		t.Fatalf("expected machine phase ready after completion, got %v", machineBody["phase"])
	}
	provisioning, _ := machineBody["provision"].(map[string]any)
	if active, _ := provisioning["active"].(bool); active {
		t.Fatalf("expected provision.active=false, got %v", provisioning["active"])
	}
	if provisioning["completedAt"] == nil {
		t.Fatalf("expected provision.completedAt to be set, got %v", provisioning)
	}

	req = httptest.NewRequest(http.MethodPost, "/pxe/install-complete?token=token-machine-finish-01&type=curtin", nil)
	rec = httptest.NewRecorder()
	c = e.NewContext(req, rec)
	if err := h.PXEInstallComplete(c); err != nil {
		t.Fatalf("PXEInstallComplete retry: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected idempotent completion retry to return 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var retryPayload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &retryPayload); err != nil {
		t.Fatalf("parse retry response: %v", err)
	}
	if retryPayload["status"] != "already-finalized" {
		t.Fatalf("expected already-finalized retry response, got %v", retryPayload["status"])
	}
}

func TestPXENocloudUserData_MachineCloudInitRef(t *testing.T) {
	backend := memory.New()
	machineSvc := machine.NewService(backend.Machines())
	cloudInitSvc := cloudinit.NewService(backend.CloudInits())
	now := time.Now().UTC()

	tpl := cloudinit.CloudInitTemplate{
		Name:     "ci-machine-01",
		UserData: "#cloud-config\npackages:\n  - htop\n  - vim\n",
	}
	if _, err := cloudInitSvc.Create(context.Background(), tpl); err != nil {
		t.Fatalf("create cloud-init template: %v", err)
	}

	target := machine.Machine{
		Name:          "bm-ci",
		Hostname:      "bm-ci",
		MAC:           "52:54:00:ab:cd:ef",
		Arch:          "amd64",
		Firmware:      machine.FirmwareUEFI,
		CloudInitRefs: []string{"ci-machine-01"},
		OSPreset: machine.OSPreset{
			Family: machine.OSTypeUbuntu,
		},
		Phase:                    machine.PhaseProvisioning,
		LastDeployedCloudInitRef: "ci-machine-01",
		Provision: &machine.ProvisionProgress{
			Active:          true,
			CompletionToken: "token-machine-ci",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.Machines().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/pxe/nocloud/525400abcdef/user-data", nil)
	req.Host = "192.168.2.254:8080"
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("mac")
	c.SetParamValues("525400abcdef")

	h := &Handler{machines: machineSvc, cloudInits: cloudInitSvc}
	if err := h.PXENocloudUserData(c); err != nil {
		t.Fatalf("PXENocloudUserData: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "htop") || !strings.Contains(body, "vim") {
		t.Fatalf("expected machine cloud-init user-data with htop and vim, got: %s", body)
	}
	if !strings.Contains(body, "install-complete?token=token-machine-ci&type=curtin") {
		t.Fatalf("expected install-complete callback with machine token, got: %s", body)
	}
}

func TestPXEBootScript_CurtinInitrdForLinuxMachineProvisioning(t *testing.T) {
	backend := memory.New()
	machineSvc := machine.NewService(backend.Machines())
	now := time.Now().UTC()

	target := machine.Machine{
		Name:     "bm-linux-01",
		Hostname: "bm-linux-01",
		MAC:      "52:54:00:aa:11:22",
		Arch:     "amd64",
		Firmware: machine.FirmwareUEFI,
		OSPreset: machine.OSPreset{
			Family: "debian",
		},
		Phase: machine.PhaseProvisioning,
		Provision: &machine.ProvisionProgress{
			Active:          true,
			CompletionToken: "token-machine-linux",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.Machines().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/pxe/boot.ipxe?mac=52:54:00:aa:11:22", nil)
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

	// Must use split boot-kernel, boot-initrd, and SquashFS rootfs artifacts.
	if !strings.Contains(body, "files/linux/boot-kernel") {
		t.Fatalf("expected linux boot-kernel path, got: %s", body)
	}
	if !strings.Contains(body, "files/linux/boot-initrd") {
		t.Fatalf("expected boot-initrd path, got: %s", body)
	}
	if !strings.Contains(body, "root=squash:${base}/files/linux/rootfs.squashfs") {
		t.Fatalf("expected squashfs rootfs path, got: %s", body)
	}
	// Must pass gomi.base and gomi.token kernel parameters (token = provision CompletionToken)
	if !strings.Contains(body, "gomi.base=") {
		t.Fatalf("expected gomi.base kernel parameter, got: %s", body)
	}
	if !strings.Contains(body, "gomi.token=token-machine-linux") {
		t.Fatalf("expected gomi.token=token-machine-linux (provision token), got: %s", body)
	}
	if !strings.Contains(body, "BOOTIF=01-52-54-00-aa-11-22") {
		t.Fatalf("expected BOOTIF for deterministic PXE NIC selection, got: %s", body)
	}
	if !strings.Contains(body, "gomi.boot_mac=52:54:00:aa:11:22") {
		t.Fatalf("expected boot MAC kernel parameter, got: %s", body)
	}
	// Must include ip=dhcp for kernel-level network init
	if !strings.Contains(body, "ip=dhcp") {
		t.Fatalf("expected ip=dhcp kernel parameter, got: %s", body)
	}
	for _, param := range []string{"earlyprintk=", "efi=debug", "ignore_loglevel", "initcall_debug", "earlycon=efifb", "keep_bootcon", "console=ttyS0"} {
		if strings.Contains(body, param) {
			t.Fatalf("did not expect debug kernel parameter %q, got: %s", param, body)
		}
	}
	for _, marker := range []string{
		"echo GOMI: before dhcp",
		"echo GOMI: before kernel",
		"echo GOMI: before initrd",
		"echo GOMI: after initrd",
		"imgstat",
		"echo GOMI: booting",
	} {
		if !strings.Contains(body, marker) {
			t.Fatalf("expected PXE progress marker %q, got: %s", marker, body)
		}
	}
	// Must NOT contain autoinstall or ISO references
	if strings.Contains(body, "autoinstall") {
		t.Fatalf("curtin boot script must not contain autoinstall, got: %s", body)
	}
	if strings.Contains(body, "ubuntu.iso") {
		t.Fatalf("curtin boot script must not reference ubuntu.iso, got: %s", body)
	}
	// Must NOT contain local boot (sanboot)
	if strings.Contains(body, "sanboot") {
		t.Fatalf("bare metal machine must not use local boot (sanboot), got: %s", body)
	}
}

func TestPXEBootScript_DesktopMachineStillUsesArtifactCurtinPath(t *testing.T) {
	backend := memory.New()
	machineSvc := machine.NewService(backend.Machines())
	osImageSvc := osimage.NewService(backend.OSImages())
	now := time.Now().UTC()

	// Create a desktop OS image to resolve variant.
	img := osimage.OSImage{
		Name:      "ubuntu-desktop-24.04",
		OSFamily:  "ubuntu",
		OSVersion: "24.04",
		Arch:      "amd64",
		Format:    osimage.FormatISO,
		Source:    osimage.SourceUpload,
		Variant:   osimage.VariantDesktop,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.OSImages().Upsert(context.Background(), img); err != nil {
		t.Fatalf("upsert os image: %v", err)
	}

	target := machine.Machine{
		Name:     "bm-desktop-01",
		Hostname: "bm-desktop-01",
		MAC:      "52:54:00:de:sk:01",
		Arch:     "amd64",
		Firmware: machine.FirmwareUEFI,
		OSPreset: machine.OSPreset{
			Family:   machine.OSTypeUbuntu,
			ImageRef: "ubuntu-desktop-24.04",
		},
		Phase: machine.PhaseProvisioning,
		Provision: &machine.ProvisionProgress{
			Active:          true,
			CompletionToken: "token-desktop-machine",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.Machines().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/pxe/boot.ipxe?mac=52:54:00:de:sk:01", nil)
	req.Host = "192.168.2.254:8080"
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h := &Handler{machines: machineSvc, osimages: osImageSvc}
	if err := h.PXEBootScript(c); err != nil {
		t.Fatalf("PXEBootScript: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	body := rec.Body.String()

	if !strings.Contains(body, "files/linux/boot-kernel") {
		t.Fatalf("expected linux boot-kernel path, got: %s", body)
	}
	if !strings.Contains(body, "files/linux/boot-initrd") {
		t.Fatalf("expected linux boot-initrd path, got: %s", body)
	}
	if !strings.Contains(body, "root=squash:${base}/files/linux/rootfs.squashfs") {
		t.Fatalf("expected linux squashfs rootfs path, got: %s", body)
	}
	if !strings.Contains(body, "gomi.token=token-desktop-machine") {
		t.Fatalf("expected provision token in curtin script, got: %s", body)
	}
	if strings.Contains(body, "autoinstall") {
		t.Fatalf("desktop machine must not bypass artifact deploy through autoinstall, got: %s", body)
	}
	if strings.Contains(body, "ubuntu.iso") {
		t.Fatalf("desktop machine must not reference ISO deploy path, got: %s", body)
	}
}

func TestPXECurtinConfig_UsesInventoryAndRawArtifact(t *testing.T) {
	backend := memory.New()
	machineSvc := machine.NewService(backend.Machines())
	hwInfoSvc := hwinfo.NewService(backend.HWInfo())
	osImageSvc := osimage.NewService(backend.OSImages())
	now := time.Now().UTC()
	artifactDir := t.TempDir()

	img := osimage.OSImage{
		Name:      "debian-13-amd64",
		OSFamily:  "debian",
		OSVersion: "13",
		Arch:      "amd64",
		Format:    osimage.FormatRAW,
		Source:    osimage.SourceUpload,
		Ready:     true,
		LocalPath: artifactDir,
		Manifest: &osimage.Manifest{
			SchemaVersion: "gomi.osimage.v1",
			BootModes:     []string{"bios", "uefi"},
			Root: osimage.RootArtifact{
				Format: osimage.FormatRAW,
				Path:   "root.raw",
				SHA256: "root-sha",
				RootPartition: osimage.Partition{
					Number:     1,
					Filesystem: "ext4",
				},
				EFIPartition: &osimage.Partition{
					Number:     15,
					Filesystem: "vfat",
				},
				BootPartition: &osimage.Partition{
					Number:     16,
					Filesystem: "ext4",
				},
			},
			TargetKernel: osimage.TargetKernel{Version: "6.12.0-13-amd64"},
			Bundles: []osimage.Bundle{
				{
					ID:              "kernel-modules-6.12.0-13-amd64",
					Type:            "kernel-modules",
					KernelVersion:   "6.12.0-13-amd64",
					Path:            "modules/6.12.0-13-amd64/kernel-modules.tar.zst",
					SHA256:          "modules-sha",
					ProvidesModules: []string{"e1000e", "igc", "r8169"},
				},
				{
					ID:     "firmware-net-intel",
					Type:   "firmware",
					Path:   "firmware/linux-firmware-net-intel.tar.zst",
					SHA256: "firmware-sha",
				},
			},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.OSImages().Upsert(context.Background(), img); err != nil {
		t.Fatalf("upsert os image: %v", err)
	}
	target := machine.Machine{
		Name:     "bm-plan",
		Hostname: "bm-plan",
		MAC:      "52:54:00:aa:bb:02",
		Arch:     "amd64",
		Firmware: machine.FirmwareUEFI,
		OSPreset: machine.OSPreset{
			Family:   machine.OSTypeDebian,
			Version:  "13",
			ImageRef: "debian-13-amd64",
		},
		Phase: machine.PhaseProvisioning,
		Provision: &machine.ProvisionProgress{
			Active:          true,
			AttemptID:       "attempt-plan",
			CompletionToken: "token-plan",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.Machines().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}
	_, err := hwInfoSvc.Upsert(context.Background(), hwinfo.HardwareInfo{
		Name:        "bm-plan-hwinfo",
		MachineName: "bm-plan",
		AttemptID:   "attempt-plan",
		Disks: []hwinfo.DiskInfo{
			{
				Name:   "nvme0n1",
				Path:   "/dev/nvme0n1",
				ByID:   []string{"/dev/disk/by-id/nvme-GOMI_TEST"},
				Type:   "disk",
				SizeMB: 1024 * 64,
			},
		},
	})
	if err != nil {
		t.Fatalf("upsert hwinfo: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/pxe/curtin-config?token=token-plan&attempt_id=attempt-plan", nil)
	req.Host = "192.168.2.254:8080"
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h := &Handler{machines: machineSvc, hwinfo: hwInfoSvc, osimages: osImageSvc}
	if err := h.PXECurtinConfig(c); err != nil {
		t.Fatalf("PXECurtinConfig: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"install:",
		"block-meta:",
		`- "/dev/nvme0n1"`,
		"sources:",
		`- dd-img: "http://192.168.2.254:8080/pxe/artifacts/os-images/debian-13-amd64/root.raw"`,
		"late_commands:",
		"var/lib/cloud/seed/nocloud",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected curtin config to contain %q, got:\n%s", want, body)
		}
	}
	for _, forbidden := range []string{"kernel-modules", "firmware/linux-firmware"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("curtin config should not manage old runner bundles %q, got:\n%s", forbidden, body)
		}
	}
}

func TestPXECurtinConfig_DirectCloudImage(t *testing.T) {
	backend := memory.New()
	machineSvc := machine.NewService(backend.Machines())
	hwInfoSvc := hwinfo.NewService(backend.HWInfo())
	osImageSvc := osimage.NewService(backend.OSImages())
	now := time.Now().UTC()

	img := osimage.OSImage{
		Name:      "ubuntu-24.04-amd64",
		OSFamily:  "ubuntu",
		OSVersion: "24.04",
		Arch:      "amd64",
		Format:    osimage.FormatRAW,
		Source:    osimage.SourceURL,
		Ready:     true,
		LocalPath: "/var/lib/gomi/data/images/ubuntu-24.04-amd64.raw",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.OSImages().Upsert(context.Background(), img); err != nil {
		t.Fatalf("upsert os image: %v", err)
	}
	target := machine.Machine{
		Name:     "bm-direct-plan",
		Hostname: "bm-direct-plan",
		MAC:      "52:54:00:aa:bb:12",
		Arch:     "amd64",
		Firmware: machine.FirmwareBIOS,
		OSPreset: machine.OSPreset{
			Family:   machine.OSTypeUbuntu,
			Version:  "24.04",
			ImageRef: "ubuntu-24.04-amd64",
		},
		Phase: machine.PhaseProvisioning,
		Provision: &machine.ProvisionProgress{
			Active:          true,
			AttemptID:       "attempt-direct-plan",
			CompletionToken: "token-direct-plan",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.Machines().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}
	if _, err := hwInfoSvc.Upsert(context.Background(), hwinfo.HardwareInfo{
		Name:        "bm-direct-plan-hwinfo",
		MachineName: "bm-direct-plan",
		AttemptID:   "attempt-direct-plan",
		Disks: []hwinfo.DiskInfo{
			{
				Name:   "vda",
				Path:   "/dev/vda",
				Type:   "disk",
				SizeMB: 32768,
			},
		},
	}); err != nil {
		t.Fatalf("upsert hwinfo: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/pxe/curtin-config?token=token-direct-plan&attempt_id=attempt-direct-plan", nil)
	req.Host = "192.168.2.254:8080"
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h := &Handler{machines: machineSvc, hwinfo: hwInfoSvc, osimages: osImageSvc}
	if err := h.PXECurtinConfig(c); err != nil {
		t.Fatalf("PXECurtinConfig: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`- "/dev/vda"`,
		`- dd-img: "http://192.168.2.254:8080/pxe/files/images/ubuntu-24.04-amd64.raw"`,
		"late_commands:",
		"var/lib/cloud/seed/nocloud",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected curtin config to contain %q, got:\n%s", want, body)
		}
	}
}

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

func TestPXEArtifact_OnlyServesManifestArtifactsAndRejectsSymlinkEscape(t *testing.T) {
	backend := memory.New()
	osImageSvc := osimage.NewService(backend.OSImages())
	artifactDir := t.TempDir()
	outsideDir := t.TempDir()
	now := time.Now().UTC()

	if err := os.WriteFile(filepath.Join(artifactDir, "root.raw"), []byte("root"), 0o644); err != nil {
		t.Fatalf("write root artifact: %v", err)
	}
	if err := os.WriteFile(filepath.Join(artifactDir, "debug.log"), []byte("debug"), 0o644); err != nil {
		t.Fatalf("write non-manifest artifact: %v", err)
	}
	modulePath := filepath.Join(artifactDir, "modules", "6.12.0-13-amd64")
	if err := os.MkdirAll(modulePath, 0o755); err != nil {
		t.Fatalf("mkdir modules: %v", err)
	}
	secretPath := filepath.Join(outsideDir, "secret.tar.zst")
	if err := os.WriteFile(secretPath, []byte("secret"), 0o644); err != nil {
		t.Fatalf("write outside artifact: %v", err)
	}
	if err := os.Symlink(secretPath, filepath.Join(modulePath, "modules-extra-net.tar.zst")); err != nil {
		t.Fatalf("symlink module artifact: %v", err)
	}

	img := osimage.OSImage{
		Name:      "debian-13-amd64",
		OSFamily:  "debian",
		OSVersion: "13",
		Arch:      "amd64",
		Format:    osimage.FormatRAW,
		Source:    osimage.SourceUpload,
		Ready:     true,
		LocalPath: artifactDir,
		Manifest: &osimage.Manifest{
			SchemaVersion: "gomi.osimage.v1",
			BootModes:     []string{"bios", "uefi"},
			Root: osimage.RootArtifact{
				Format: osimage.FormatRAW,
				Path:   "root.raw",
				SHA256: "root-sha",
			},
			TargetKernel: osimage.TargetKernel{Version: "6.12.0-13-amd64"},
			Bundles: []osimage.Bundle{
				{
					ID:              "kernel-modules-6.12.0-13-amd64",
					Type:            "kernel-modules",
					KernelVersion:   "6.12.0-13-amd64",
					Path:            "modules/6.12.0-13-amd64/modules-extra-net.tar.zst",
					SHA256:          "modules-sha",
					ProvidesModules: []string{"e1000e", "igc", "r8169"},
				},
			},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.OSImages().Upsert(context.Background(), img); err != nil {
		t.Fatalf("upsert os image: %v", err)
	}
	h := &Handler{osimages: osImageSvc}

	t.Run("serves manifest root", func(t *testing.T) {
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/pxe/artifacts/os-images/debian-13-amd64/root.raw", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("name", "*")
		c.SetParamValues("debian-13-amd64", "root.raw")

		if err := h.PXEArtifact(c); err != nil {
			t.Fatalf("PXEArtifact: %v", err)
		}
		if rec.Code != http.StatusOK || rec.Body.String() != "root" {
			t.Fatalf("unexpected response: status=%d body=%q", rec.Code, rec.Body.String())
		}
	})

	t.Run("rejects file outside manifest", func(t *testing.T) {
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/pxe/artifacts/os-images/debian-13-amd64/debug.log", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("name", "*")
		c.SetParamValues("debian-13-amd64", "debug.log")

		if err := h.PXEArtifact(c); err != nil {
			t.Fatalf("PXEArtifact: %v", err)
		}
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("rejects symlink escape even when manifest-listed", func(t *testing.T) {
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/pxe/artifacts/os-images/debian-13-amd64/modules/6.12.0-13-amd64/modules-extra-net.tar.zst", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("name", "*")
		c.SetParamValues("debian-13-amd64", "modules/6.12.0-13-amd64/modules-extra-net.tar.zst")

		if err := h.PXEArtifact(c); err != nil {
			t.Fatalf("PXEArtifact: %v", err)
		}
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d body=%s", rec.Code, rec.Body.String())
		}
	})
}

func TestTargetDiskValidationAndPartitionPaths(t *testing.T) {
	wholeDisks := []string{
		"/dev/sda",
		"/dev/vda",
		"/dev/xvda",
		"/dev/nvme0n1",
		"/dev/mmcblk0",
		"/dev/disk/by-id/nvme-GOMI_TEST",
		"/dev/disk/by-path/pci-0000:00:1f.2-ata-1",
	}
	for _, disk := range wholeDisks {
		if !isWholeDiskPath(disk) {
			t.Fatalf("expected whole disk path to be allowed: %s", disk)
		}
	}
	partitions := []string{
		"/dev/sda1",
		"/dev/vda2",
		"/dev/xvda3",
		"/dev/nvme0n1p1",
		"/dev/mmcblk0p2",
		"/dev/disk/by-id/nvme-GOMI_TEST-part1",
		"/dev/disk/by-path/pci-0000:00:1f.2-ata-1-part2",
	}
	for _, disk := range partitions {
		if isWholeDiskPath(disk) {
			t.Fatalf("expected partition path to be rejected: %s", disk)
		}
		m := &machine.Machine{TargetDisk: disk}
		if _, err := selectTargetDisk(m, &hwinfo.HardwareInfo{}); err == nil {
			t.Fatalf("expected target disk override to be rejected: %s", disk)
		}
	}

	overrideInfo := &hwinfo.HardwareInfo{Disks: []hwinfo.DiskInfo{{
		Name:   "sda",
		Path:   "/dev/sda",
		ByID:   []string{"/dev/disk/by-id/current-disk"},
		ByPath: []string{"/dev/disk/by-path/current-disk"},
		Type:   "disk",
	}}}
	overrideMachine := &machine.Machine{TargetDisk: "/dev/disk/by-id/current-disk"}
	if got, err := selectTargetDisk(overrideMachine, overrideInfo); err != nil || got != "/dev/disk/by-id/current-disk" {
		t.Fatalf("expected inventory-backed target override, got disk=%q err=%v", got, err)
	}
	staleOverride := &machine.Machine{TargetDisk: "/dev/disk/by-id/stale-disk"}
	if _, err := selectTargetDisk(staleOverride, overrideInfo); err == nil {
		t.Fatal("expected stale target disk override to be rejected")
	}
}

func TestPXEInstallComplete_BIOSMachineConfiguresBootOrder(t *testing.T) {
	backend := memory.New()
	machineSvc := machine.NewService(backend.Machines())
	now := time.Now().UTC()
	target := machine.Machine{
		Name:     "bm-bios-01",
		Hostname: "bm-bios-01",
		MAC:      "52:54:00:11:22:33",
		Arch:     "amd64",
		Firmware: machine.FirmwareBIOS,
		Power: power.PowerConfig{
			Type: power.PowerTypeWebhook,
			Webhook: &power.WebhookConfig{
				PowerOnURL:   "https://power.example/on",
				PowerOffURL:  "https://power.example/off",
				BootOrderURL: "https://power.example/boot-order",
			},
		},
		OSPreset: machine.OSPreset{
			Family:   machine.OSTypeUbuntu,
			ImageRef: "ubuntu-24.04-server",
		},
		Phase: machine.PhaseProvisioning,
		Provision: &machine.ProvisionProgress{
			Active:          true,
			CompletionToken: "token-bios-finish-01",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.Machines().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}

	exec := &stubPowerExecutor{}
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/pxe/install-complete?token=token-bios-finish-01&type=curtin", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h := &Handler{machines: machineSvc, powerExecutor: exec}
	if err := h.PXEInstallComplete(c); err != nil {
		t.Fatalf("PXEInstallComplete: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}
	if len(exec.calls) != 1 {
		t.Fatalf("expected 1 boot order call, got %d", len(exec.calls))
	}
	if exec.calls[0].machine.Name != target.Name {
		t.Fatalf("expected machine %s, got %s", target.Name, exec.calls[0].machine.Name)
	}
	if len(exec.calls[0].order) != len(power.DefaultBIOSBootOrder) {
		t.Fatalf("expected boot order length %d, got %d", len(power.DefaultBIOSBootOrder), len(exec.calls[0].order))
	}
	for i, item := range power.DefaultBIOSBootOrder {
		if exec.calls[0].order[i] != item {
			t.Fatalf("expected boot order[%d]=%s, got %s", i, item, exec.calls[0].order[i])
		}
	}

	stored, err := backend.Machines().Get(context.Background(), target.Name)
	if err != nil {
		t.Fatalf("get machine: %v", err)
	}
	if stored.Phase != machine.PhaseReady {
		t.Fatalf("expected machine phase ready, got %s", stored.Phase)
	}
	if stored.Provision == nil || !strings.Contains(stored.Provision.Message, "BIOS boot order updated") {
		t.Fatalf("expected provision message to mention BIOS boot order update, got %+v", stored.Provision)
	}
}

func TestPXEDeployEvents_ImageAppliedLocalBootsAndConfiguresBIOSBootOrder(t *testing.T) {
	backend := memory.New()
	machineSvc := machine.NewService(backend.Machines())
	now := time.Now().UTC()
	target := machine.Machine{
		Name:     "bm-image-applied-01",
		Hostname: "bm-image-applied-01",
		MAC:      "52:54:00:44:55:66",
		Arch:     "amd64",
		Firmware: machine.FirmwareBIOS,
		Power: power.PowerConfig{
			Type: power.PowerTypeWebhook,
			Webhook: &power.WebhookConfig{
				PowerOnURL:   "https://power.example/on",
				PowerOffURL:  "https://power.example/off",
				BootOrderURL: "https://power.example/boot-order",
			},
		},
		Phase: machine.PhaseProvisioning,
		Provision: &machine.ProvisionProgress{
			Active:          true,
			AttemptID:       "attempt-image-applied-01",
			CompletionToken: "token-image-applied-01",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.Machines().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}

	exec := &stubPowerExecutor{}
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/pxe/deploy-events?token=token-image-applied-01&attempt_id=attempt-image-applied-01", strings.NewReader(`{"type":"image_applied"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h := &Handler{machines: machineSvc, powerExecutor: exec}
	if err := h.PXEDeployEvents(c); err != nil {
		t.Fatalf("PXEDeployEvents: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}
	if len(exec.calls) != 1 {
		t.Fatalf("expected 1 boot order call, got %d", len(exec.calls))
	}

	stored, err := backend.Machines().Get(context.Background(), target.Name)
	if err != nil {
		t.Fatalf("get machine: %v", err)
	}
	if stored.Phase != machine.PhaseProvisioning {
		t.Fatalf("expected machine to remain provisioning until target OS callback, got %s", stored.Phase)
	}
	if stored.Provision == nil || !stored.Provision.Active {
		t.Fatalf("expected provision to remain active, got %+v", stored.Provision)
	}
	if stored.Provision.Artifacts[provisionArtifactImageApplied] != "true" {
		t.Fatalf("expected imageApplied artifact, got %+v", stored.Provision.Artifacts)
	}
	if !strings.Contains(stored.Provision.Message, "waiting for target OS first boot") ||
		!strings.Contains(stored.Provision.Message, "BIOS boot order updated") {
		t.Fatalf("unexpected provision message: %q", stored.Provision.Message)
	}

	bootReq := httptest.NewRequest(http.MethodGet, "/pxe/boot.ipxe?mac=52:54:00:44:55:66", nil)
	bootRec := httptest.NewRecorder()
	bootCtx := e.NewContext(bootReq, bootRec)
	if err := h.PXEBootScript(bootCtx); err != nil {
		t.Fatalf("PXEBootScript: %v", err)
	}
	if bootRec.Code != http.StatusOK {
		t.Fatalf("unexpected boot status: %d body=%s", bootRec.Code, bootRec.Body.String())
	}
	body := bootRec.Body.String()
	if !strings.Contains(body, "chain --autofree tftp://${next-server}/grubnetx64.efi") {
		t.Fatalf("expected local boot script after image_applied, got: %s", body)
	}
	if strings.Contains(body, "curtin-initrd") {
		t.Fatalf("did not expect redeploy script after image_applied, got: %s", body)
	}
}

func TestPXEProvisionEndpointsRejectInactiveOrMismatchedAttempt(t *testing.T) {
	backend := memory.New()
	machineSvc := machine.NewService(backend.Machines())
	hwInfoSvc := hwinfo.NewService(backend.HWInfo())
	now := time.Now().UTC()
	inactive := machine.Machine{
		Name:     "bm-inactive-token",
		Hostname: "bm-inactive-token",
		MAC:      "52:54:00:aa:cc:01",
		Arch:     "amd64",
		Firmware: machine.FirmwareUEFI,
		Phase:    machine.PhaseReady,
		Provision: &machine.ProvisionProgress{
			Active:          false,
			AttemptID:       "attempt-inactive",
			CompletionToken: "token-inactive",
			CompletedAt:     &now,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.Machines().Upsert(context.Background(), inactive); err != nil {
		t.Fatalf("upsert inactive machine: %v", err)
	}
	active := machine.Machine{
		Name:     "bm-attempt-mismatch",
		Hostname: "bm-attempt-mismatch",
		MAC:      "52:54:00:aa:cc:02",
		Arch:     "amd64",
		Firmware: machine.FirmwareUEFI,
		Phase:    machine.PhaseProvisioning,
		Provision: &machine.ProvisionProgress{
			Active:          true,
			AttemptID:       "attempt-current",
			CompletionToken: "token-current",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.Machines().Upsert(context.Background(), active); err != nil {
		t.Fatalf("upsert active machine: %v", err)
	}

	e := echo.New()
	h := &Handler{machines: machineSvc, hwinfo: hwInfoSvc}
	req := httptest.NewRequest(http.MethodPost, "/pxe/inventory?token=token-inactive", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	if err := h.PXEInventory(c); err != nil {
		t.Fatalf("PXEInventory inactive: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected inactive token to be rejected, got %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/pxe/curtin-config?token=token-inactive&attempt_id=attempt-current", nil)
	rec = httptest.NewRecorder()
	c = e.NewContext(req, rec)
	if err := h.PXECurtinConfig(c); err != nil {
		t.Fatalf("PXECurtinConfig inactive: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected curtin-config inactive token to be rejected, got %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/pxe/deploy-events?token=token-current&attempt_id=attempt-stale", strings.NewReader(`{"type":"image_applied"}`))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	c = e.NewContext(req, rec)
	if err := h.PXEDeployEvents(c); err != nil {
		t.Fatalf("PXEDeployEvents stale attempt: %v", err)
	}
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected stale attempt to be rejected, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestPXENocloudUserData_MachineServerUsesCloudConfig(t *testing.T) {
	backend := memory.New()
	machineSvc := machine.NewService(backend.Machines())
	now := time.Now().UTC()

	target := machine.Machine{
		Name:     "bm-server-cloud",
		Hostname: "bm-server-cloud",
		MAC:      "52:54:00:cf:cf:cf",
		Arch:     "amd64",
		Firmware: machine.FirmwareUEFI,
		OSPreset: machine.OSPreset{
			Family: machine.OSTypeUbuntu,
		},
		Phase: machine.PhaseProvisioning,
		Provision: &machine.ProvisionProgress{
			Active:          true,
			CompletionToken: "token-server-cloud",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.Machines().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/pxe/nocloud/525400cfcfcf/user-data", nil)
	req.Host = "192.168.2.254:8080"
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("mac")
	c.SetParamValues("525400cfcfcf")

	h := &Handler{machines: machineSvc}
	if err := h.PXENocloudUserData(c); err != nil {
		t.Fatalf("PXENocloudUserData: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	body := rec.Body.String()

	// Server machine must use standard cloud-config (not autoinstall format)
	if strings.Contains(body, "autoinstall:") {
		t.Fatalf("server machine user-data must not contain autoinstall section, got: %s", body)
	}
	if !strings.Contains(body, "#cloud-config") {
		t.Fatalf("expected cloud-config header, got: %s", body)
	}
	if !strings.Contains(body, "hostname: bm-server-cloud") {
		t.Fatalf("expected hostname in cloud-config, got: %s", body)
	}
	if !strings.Contains(body, "/usr/local/sbin/gomi-fix-uefi-bootorder") {
		t.Fatalf("expected target UEFI BootOrder cleanup script, got: %s", body)
	}
	if !strings.Contains(body, "gomi-bootorder-cleanup.service") {
		t.Fatalf("expected target UEFI BootOrder cleanup service, got: %s", body)
	}
	if !strings.Contains(body, "PXE IPv6 boot entry") {
		t.Fatalf("expected target UEFI cleanup to remove PXE IPv6 entries, got: %s", body)
	}
	if !strings.Contains(body, "efibootmgr -N") {
		t.Fatalf("expected target UEFI cleanup to clear BootNext, got: %s", body)
	}
	if strings.Contains(body, "efibootmgr -n") {
		t.Fatalf("target UEFI cleanup must not set BootNext, got: %s", body)
	}
}

func TestPXENocloudUserData_MachineWoLShutdownAgent(t *testing.T) {
	backend := memory.New()
	machineSvc := machine.NewService(backend.Machines())
	now := time.Now().UTC()

	target := machine.Machine{
		Name:     "bm-wol",
		Hostname: "bm-wol",
		MAC:      "52:54:00:44:55:66",
		Arch:     "amd64",
		Firmware: machine.FirmwareUEFI,
		Power: power.PowerConfig{
			Type: power.PowerTypeWoL,
			WoL: &power.WoLConfig{
				WakeMAC:         "52:54:00:44:55:66",
				BroadcastIP:     "192.168.2.255",
				Port:            9,
				ShutdownUDPPort: 40000,
				HMACSecret:      "secret-hex",
				Token:           "token-hex",
				TokenTTLSeconds: 90,
			},
		},
		OSPreset: machine.OSPreset{
			Family: machine.OSTypeUbuntu,
		},
		Phase: machine.PhaseProvisioning,
		Provision: &machine.ProvisionProgress{
			Active:          true,
			CompletionToken: "token-wol",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.Machines().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/pxe/nocloud/525400445566/user-data", nil)
	req.Host = "192.168.2.254:8080"
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("mac")
	c.SetParamValues("525400445566")

	h := &Handler{machines: machineSvc}
	if err := h.PXENocloudUserData(c); err != nil {
		t.Fatalf("PXENocloudUserData: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"/etc/systemd/system/gomi-wol-daemon.service",
		"/etc/gomi/wol-daemon.env",
		"/usr/local/sbin/gomi-install-wol-daemon",
		"gomi-wol-daemon-linux-${arch}",
		"http://192.168.2.254:8080/files/gomi-wol-daemon-linux-${arch}",
		"GOMI_WOL_LISTEN=\":40000\"",
		"GOMI_WOL_SECRET=\"secret-hex\"",
		"GOMI_WOL_TOKEN=\"token-hex\"",
		"GOMI_WOL_TTL=\"90s\"",
		"GOMI_SERVER_URL=\"http://192.168.2.254:8080\"",
		"GOMI_MACHINE_NAME=\"bm-wol\"",
		"ExecStart=/usr/local/bin/gomi-wol-daemon --env-file /etc/gomi/wol-daemon.env",
		"systemctl enable --now gomi-wol-daemon.service",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected %q in user-data, got:\n%s", want, body)
		}
	}
	for _, forbidden := range []string{
		"EnvironmentFile=/etc/gomi/wol-daemon.env",
		"--secret",
		"--token",
		"${GOMI_WOL_SECRET}",
		"${GOMI_WOL_TOKEN}",
		"gomi-install-wol-daemon || true",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("did not expect %q in user-data, got:\n%s", forbidden, body)
		}
	}
}

func TestPXENocloudUserData_HypervisorRunsSetupAndRegisterScript(t *testing.T) {
	backend := memory.New()
	machineSvc := machine.NewService(backend.Machines())
	now := time.Now().UTC()

	target := machine.Machine{
		Name:       "node3",
		Hostname:   "node3",
		MAC:        "52:54:00:77:88:99",
		Arch:       "amd64",
		Firmware:   machine.FirmwareUEFI,
		Role:       machine.RoleHypervisor,
		BridgeName: "br0",
		OSPreset: machine.OSPreset{
			Family: machine.OSTypeUbuntu,
		},
		Phase: machine.PhaseProvisioning,
		Provision: &machine.ProvisionProgress{
			Active:          true,
			CompletionToken: "token-hv",
			Artifacts: map[string]string{
				machine.ProvisionArtifactHypervisorRegistrationToken: "hv-registration-token",
			},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.Machines().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/pxe/nocloud/525400778899/user-data", nil)
	req.Host = "192.168.2.254:8080"
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("mac")
	c.SetParamValues("525400778899")

	h := &Handler{machines: machineSvc}
	if err := h.PXENocloudUserData(c); err != nil {
		t.Fatalf("PXENocloudUserData: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"qemu-system",
		"zstd",
		`auth_tcp = "none"`,
		"systemctl start libvirtd-tcp.socket",
		"/api/v1/hypervisors/setup-and-register.sh",
		"GOMI_SERVER=",
		"http://192.168.2.254:8080",
		"GOMI_TOKEN=",
		"hv-registration-token",
		"GOMI_HOSTNAME=",
		"node3",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected hypervisor user-data to contain %q, got:\n%s", want, body)
		}
	}
	if strings.Contains(body, "qemu-kvm") {
		t.Fatalf("hypervisor user-data must not request obsolete qemu-kvm package, got:\n%s", body)
	}
}

func TestPXENocloudUserData_HypervisorCreatesMissingRegistrationToken(t *testing.T) {
	backend := memory.New()
	machineSvc := machine.NewService(backend.Machines())
	hypervisorSvc := hypervisor.NewService(backend.Hypervisors(), backend.HypervisorTokens(), backend.AgentTokens())
	now := time.Now().UTC()

	target := machine.Machine{
		Name:       "node2",
		Hostname:   "node2",
		MAC:        "52:54:00:aa:bb:02",
		Arch:       "amd64",
		Firmware:   machine.FirmwareUEFI,
		Role:       machine.RoleHypervisor,
		BridgeName: "br0",
		OSPreset: machine.OSPreset{
			Family: machine.OSTypeDebian,
		},
		Phase: machine.PhaseProvisioning,
		Provision: &machine.ProvisionProgress{
			Active:          true,
			CompletionToken: "token-hv",
			Artifacts:       map[string]string{},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.Machines().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/pxe/nocloud/525400aabb02/user-data", nil)
	req.Host = "192.168.2.254:8080"
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("mac")
	c.SetParamValues("525400aabb02")

	h := &Handler{machines: machineSvc, hypervisors: hypervisorSvc}
	if err := h.PXENocloudUserData(c); err != nil {
		t.Fatalf("PXENocloudUserData: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}

	tokens, err := backend.HypervisorTokens().List(context.Background())
	if err != nil {
		t.Fatalf("list registration tokens: %v", err)
	}
	if len(tokens) != 1 || strings.TrimSpace(tokens[0].Token) == "" {
		t.Fatalf("expected one generated registration token, got %#v", tokens)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"/api/v1/hypervisors/setup-and-register.sh",
		"GOMI_TOKEN=",
		tokens[0].Token,
		"GOMI_HOSTNAME=",
		"node2",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected hypervisor user-data to contain %q, got:\n%s", want, body)
		}
	}

	stored, err := backend.Machines().Get(context.Background(), "node2")
	if err != nil {
		t.Fatalf("get stored machine: %v", err)
	}
	if got := stored.Provision.Artifacts[machine.ProvisionArtifactHypervisorRegistrationToken]; got != tokens[0].Token {
		t.Fatalf("stored registration token = %q, want %q", got, tokens[0].Token)
	}
	if got := stored.Provision.Artifacts[machine.ProvisionArtifactHypervisorRegistrationTokenExpiresAt]; strings.TrimSpace(got) == "" {
		t.Fatalf("expected stored registration token expiry, got %#v", stored.Provision.Artifacts)
	}
}

func TestPXEInstallComplete_ExpiredToken(t *testing.T) {
	backend := memory.New()
	vmSvc := vm.NewService(backend.VMs())
	now := time.Now().UTC()
	target := vm.VirtualMachine{
		Name:          "vm-expired",
		HypervisorRef: "hv-01",
		Resources:     vm.ResourceSpec{CPUCores: 2, MemoryMB: 2048, DiskGB: 20},
		Phase:         vm.PhaseError,
		Provisioning: vm.ProvisioningStatus{
			Active:          false,
			CompletionToken: "token-expired",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.VMs().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert vm: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/pxe/install-complete?token=token-expired&type=preseed", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h := &Handler{vms: vmSvc}
	if err := h.PXEInstallComplete(c); err != nil {
		t.Fatalf("PXEInstallComplete: %v", err)
	}
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected status 409, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestPXENocloudNetworkConfig_DHCP(t *testing.T) {
	e := echo.New()
	// MAC token (no colons): 844709191cd6
	req := httptest.NewRequest(http.MethodGet, "/pxe/nocloud/844709191cd6/network-config", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("mac")
	c.SetParamValues("844709191cd6")

	h := &Handler{} // no machines registered → DHCP
	if err := h.PXENocloudNetworkConfig(c); err != nil {
		t.Fatalf("PXENocloudNetworkConfig: %v", err)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `macaddress: "84:47:09:1c:d6"`) && !strings.Contains(body, `macaddress: "84:47:09:19:1c:d6"`) {
		// normalizeMAC reconstructs colon-separated MAC from token
		if !strings.Contains(body, "macaddress:") {
			t.Fatalf("expected macaddress match in DHCP config, got:\n%s", body)
		}
	}
	if !strings.Contains(body, "dhcp4: true") {
		t.Fatalf("expected dhcp4: true, got:\n%s", body)
	}
	if !strings.Contains(body, "wakeonlan: true") {
		t.Fatalf("expected wakeonlan enabled, got:\n%s", body)
	}
	if strings.Contains(body, "en*") || strings.Contains(body, "eth*") {
		t.Fatalf("should not use wildcard name match, got:\n%s", body)
	}
}

func TestBuildNetworkConfig_DHCP(t *testing.T) {
	got := buildNetworkConfig("84:47:09:1f:1c:d6", "", nil)
	if !strings.Contains(got, `macaddress: "84:47:09:1f:1c:d6"`) {
		t.Fatalf("expected mac match, got:\n%s", got)
	}
	if !strings.Contains(got, "dhcp4: true") {
		t.Fatalf("expected dhcp4: true, got:\n%s", got)
	}
	if !strings.Contains(got, "wakeonlan: true") {
		t.Fatalf("expected wakeonlan enabled, got:\n%s", got)
	}
	if strings.Contains(got, "addresses:") {
		t.Fatalf("DHCP config should not have static addresses, got:\n%s", got)
	}
}

func TestBuildNetworkConfig_Static(t *testing.T) {
	spec := &subnet.SubnetSpec{
		CIDR:           "192.168.2.0/24",
		DefaultGateway: "192.168.2.1",
		DNSServers:     []string{"192.168.2.1"},
	}
	got := buildNetworkConfig("84:47:09:1f:1c:d6", "192.168.2.100", spec)
	if !strings.Contains(got, `macaddress: "84:47:09:1f:1c:d6"`) {
		t.Fatalf("expected mac match, got:\n%s", got)
	}
	if !strings.Contains(got, "192.168.2.100/24") {
		t.Fatalf("expected static IP, got:\n%s", got)
	}
	if !strings.Contains(got, "dhcp4: false") {
		t.Fatalf("expected dhcp4: false, got:\n%s", got)
	}
	if !strings.Contains(got, "wakeonlan: true") {
		t.Fatalf("expected wakeonlan enabled, got:\n%s", got)
	}
	if !strings.Contains(got, "192.168.2.1") {
		t.Fatalf("expected gateway, got:\n%s", got)
	}
}
