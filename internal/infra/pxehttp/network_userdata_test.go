package pxehttp

import (
	"context"
	"github.com/labstack/echo/v4"
	"github.com/sugaf1204/gomi/internal/infra/memory"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/osimage"
	"github.com/sugaf1204/gomi/internal/power"
	"github.com/sugaf1204/gomi/internal/vm"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPXENocloudUserData_UbuntuVMStaticEnablesNetworkdBeforeNetplan(t *testing.T) {
	backend := memory.New()
	vmSvc := vm.NewService(backend.VMs())
	osImageSvc := osimage.NewService(backend.OSImages())
	now := time.Now().UTC()
	if err := backend.OSImages().Upsert(context.Background(), osimage.OSImage{
		Name:      "ubuntu-26.04-amd64-cloud",
		OSFamily:  "ubuntu",
		OSVersion: "26.04",
		Arch:      "amd64",
		Ready:     true,
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("upsert os image: %v", err)
	}
	target := vm.VirtualMachine{
		Name:         "vm-ubuntu-netplan",
		OSImageRef:   "ubuntu-26.04-amd64-cloud",
		IPAssignment: vm.IPAssignmentStatic,
		Network: []vm.NetworkInterface{
			{MAC: "52:54:00:44:00:52", IPAddress: "192.168.2.232"},
		},
		Phase: vm.PhaseProvisioning,
		Provisioning: vm.ProvisioningStatus{
			Active:          true,
			CompletionToken: "token-vm-ubuntu-netplan",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.VMs().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert vm: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/pxe/nocloud/525400440052/user-data", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("mac")
	c.SetParamValues("525400440052")

	h := &Handler{vms: vmSvc, osimages: osImageSvc}
	if err := h.PXENocloudUserData(c); err != nil {
		t.Fatalf("PXENocloudUserData: %v", err)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"50-gomi-network.yaml",
		"renderer: networkd",
		"192.168.2.232/24",
		"systemctl enable --now systemd-networkd.service systemd-networkd.socket",
		"netplan apply",
		"install-complete?token=token-vm-ubuntu-netplan",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected Ubuntu VM user-data to contain %q, got:\n%s", want, body)
		}
	}
	if strings.Contains(body, "/usr/local/sbin/gomi-apply-netplan-networkd") ||
		strings.Contains(body, "NetworkManager/system-connections") {
		t.Fatalf("Ubuntu VM must not receive Debian rollback or NetworkManager config, got:\n%s", body)
	}
	enableIdx := strings.Index(body, "- systemctl enable --now systemd-networkd.service systemd-networkd.socket")
	applyIdx := strings.Index(body, "- netplan apply")
	if enableIdx == -1 || applyIdx == -1 || enableIdx > applyIdx {
		t.Fatalf("expected Ubuntu VM to enable networkd before netplan apply, got:\n%s", body)
	}
}

func TestPXENocloudUserData_CompletedRootFSDisablesResizeBeforeImageApplied(t *testing.T) {
	backend := memory.New()
	machineSvc := machine.NewService(backend.Machines())
	osImageSvc := osimage.NewService(backend.OSImages())
	now := time.Now().UTC()
	deadline := now.Add(30 * time.Minute)
	imageRef := "debian-13-amd64-baremetal"
	if err := backend.OSImages().Upsert(context.Background(), osimage.OSImage{
		Name:      imageRef,
		OSFamily:  "debian",
		OSVersion: "13",
		Arch:      "amd64",
		Format:    osimage.FormatSquashFS,
		Source:    osimage.SourceUpload,
		Ready:     true,
		Manifest: &osimage.Manifest{
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
		Name:     "bm-debian-rootfs",
		Hostname: "bm-debian-rootfs",
		MAC:      "52:54:00:44:00:48",
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
			StartedAt:       &now,
			DeadlineAt:      &deadline,
			CompletionToken: "token-debian-rootfs",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.Machines().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/pxe/nocloud/525400440048/user-data", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("mac")
	c.SetParamValues("525400440048")

	h := &Handler{machines: machineSvc, osimages: osImageSvc}
	if err := h.PXENocloudUserData(c); err != nil {
		t.Fatalf("PXENocloudUserData: %v", err)
	}
	if body := rec.Body.String(); !strings.Contains(body, "resize_rootfs: false") {
		t.Fatalf("completed rootfs deploy must disable resize_rootfs before image_applied, got:\n%s", body)
	}
}

func TestPXENocloudUserData_QCOW2MachineDoesNotDisableResizeBeforeImageApplied(t *testing.T) {
	backend := memory.New()
	machineSvc := machine.NewService(backend.Machines())
	osImageSvc := osimage.NewService(backend.OSImages())
	now := time.Now().UTC()
	deadline := now.Add(30 * time.Minute)
	imageRef := "debian-13-amd64-cloud"
	if err := backend.OSImages().Upsert(context.Background(), osimage.OSImage{
		Name:      imageRef,
		OSFamily:  "debian",
		OSVersion: "13",
		Arch:      "amd64",
		Format:    osimage.FormatQCOW2,
		Source:    osimage.SourceUpload,
		Ready:     true,
		Manifest: &osimage.Manifest{
			Capabilities: osimage.Capabilities{DeployTargets: []osimage.DeploymentTarget{osimage.DeploymentTargetBareMetal}},
			Root: osimage.RootArtifact{
				Format:        osimage.FormatQCOW2,
				Path:          "root.qcow2",
				RootPartition: osimage.Partition{Number: 1, Filesystem: "ext4"},
			},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("upsert os image: %v", err)
	}
	target := machine.Machine{
		Name:     "bm-debian-qcow2",
		Hostname: "bm-debian-qcow2",
		MAC:      "52:54:00:44:00:49",
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
			StartedAt:       &now,
			DeadlineAt:      &deadline,
			CompletionToken: "token-debian-qcow2",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.Machines().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/pxe/nocloud/525400440049/user-data", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("mac")
	c.SetParamValues("525400440049")

	h := &Handler{machines: machineSvc, osimages: osImageSvc}
	if err := h.PXENocloudUserData(c); err != nil {
		t.Fatalf("PXENocloudUserData: %v", err)
	}
	if body := rec.Body.String(); strings.Contains(body, "resize_rootfs: false") {
		t.Fatalf("non-rootfs deploy must not disable resize_rootfs, got:\n%s", body)
	} else if strings.Contains(body, "50-gomi-network.yaml") ||
		strings.Contains(body, "netplan apply") ||
		strings.Contains(body, "NetworkManager/system-connections") ||
		strings.Contains(body, ".nmconnection") {
		t.Fatalf("qcow2 disk-image deploy must not inject OS-specific network files, got:\n%s", body)
	}
}

func TestPXENocloudUserData_FedoraHypervisorWritesNetworkManagerBridge(t *testing.T) {
	backend := memory.New()
	machineSvc := machine.NewService(backend.Machines())
	now := time.Now().UTC()
	deadline := now.Add(30 * time.Minute)
	target := machine.Machine{
		Name:         "bm-fedora-hv",
		Hostname:     "bm-fedora-hv",
		MAC:          "52:54:00:44:00:47",
		Arch:         "amd64",
		Firmware:     machine.FirmwareUEFI,
		Role:         machine.RoleHypervisor,
		BridgeName:   "br-fedora",
		IP:           "192.168.2.227",
		IPAssignment: machine.IPAssignmentModeStatic,
		OSPreset: machine.OSPreset{
			Family:   machine.OSType("fedora"),
			Version:  "44",
			ImageRef: "fedora-44-amd64-baremetal",
		},
		Phase: machine.PhaseProvisioning,
		Provision: &machine.ProvisionProgress{
			Active:          true,
			StartedAt:       &now,
			DeadlineAt:      &deadline,
			CompletionToken: "token-fedora-hv",
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
	req := httptest.NewRequest(http.MethodGet, "/pxe/nocloud/525400440047/user-data", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("mac")
	c.SetParamValues("525400440047")

	h := &Handler{machines: machineSvc}
	if err := h.PXENocloudUserData(c); err != nil {
		t.Fatalf("PXENocloudUserData: %v", err)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"/etc/NetworkManager/system-connections/gomi-bridge.nmconnection",
		"/etc/NetworkManager/system-connections/gomi-nic.nmconnection",
		"id=br-fedora",
		"type=bridge",
		"interface-name=br-fedora",
		"master=br-fedora",
		"slave-type=bridge",
		"mac-address=52:54:00:44:00:47",
		"address1=192.168.2.227/24",
		"nmcli connection up 'br-fedora'",
		"hv-registration-token",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected Fedora hypervisor user-data to contain %q, got:\n%s", want, body)
		}
	}
	if strings.Contains(body, "50-gomi-network.yaml") || strings.Contains(body, "netplan apply") {
		t.Fatalf("Fedora hypervisor must not receive netplan user-data, got:\n%s", body)
	}
}

func TestPXENocloudUserData_DHCPMachineInjectsWakeOnLANNetplan(t *testing.T) {
	backend := memory.New()
	machineSvc := machine.NewService(backend.Machines())
	now := time.Now().UTC()
	deadline := now.Add(30 * time.Minute)
	target := machine.Machine{
		Name:     "bm-dhcp-wol",
		Hostname: "bm-dhcp-wol",
		MAC:      "84:47:09:1f:1c:d6",
		Arch:     "amd64",
		Firmware: machine.FirmwareUEFI,
		IP:       "192.168.2.101",
		Power: power.PowerConfig{
			Type: power.PowerTypeWoL,
			WoL:  &power.WoLConfig{WakeMAC: "84:47:09:1f:1c:d6"},
		},
		Phase: machine.PhaseProvisioning,
		Provision: &machine.ProvisionProgress{
			Active:          true,
			StartedAt:       &now,
			DeadlineAt:      &deadline,
			CompletionToken: "token-dhcp-wol",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.Machines().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/pxe/nocloud/8447091f1cd6/user-data", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("mac")
	c.SetParamValues("8447091f1cd6")

	h := &Handler{machines: machineSvc}
	if err := h.PXENocloudUserData(c); err != nil {
		t.Fatalf("PXENocloudUserData: %v", err)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "50-gomi-network.yaml") {
		t.Fatalf("expected netplan write_files entry in user-data, got:\n%s", body)
	}
	if !strings.Contains(body, "permissions: \"0600\"") {
		t.Fatalf("expected restrictive netplan permissions, got:\n%s", body)
	}
	if !strings.Contains(body, "macaddress: 84:47:09:1f:1c:d6") {
		t.Fatalf("expected MAC match in netplan config, got:\n%s", body)
	}
	if !strings.Contains(body, "dhcp4: true") {
		t.Fatalf("expected dhcp4 enabled in netplan config, got:\n%s", body)
	}
	if strings.Contains(body, "addresses:") || strings.Contains(body, "dhcp4: false") {
		t.Fatalf("DHCP machine must not inject stale Machine.IP as static netplan config, got:\n%s", body)
	}
	if !strings.Contains(body, "wakeonlan: true") {
		t.Fatalf("expected wakeonlan enabled in netplan config, got:\n%s", body)
	}
	if !strings.Contains(body, "netplan apply") {
		t.Fatalf("expected netplan apply in runcmd, got:\n%s", body)
	}
	if !strings.Contains(body, "systemd-networkd-wait-online.service") {
		t.Fatalf("expected netplan runcmd to clear transient wait-online failures, got:\n%s", body)
	}
}

func TestInjectNetplanConfigFromParamsSkipsFedora(t *testing.T) {
	input := "#cloud-config\nhostname: fedora-node\n"
	got := injectNetplanConfigFromParams(input, netplanParams{
		IP:       "192.168.2.224",
		MAC:      "52:54:00:44:00:44",
		OSFamily: "fedora",
	}, nil)
	if got != input {
		t.Fatalf("fedora must not receive Ubuntu/Debian netplan config, got:\n%s", got)
	}
}
