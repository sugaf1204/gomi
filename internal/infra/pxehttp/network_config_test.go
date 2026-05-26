package pxehttp

import (
	"context"
	"github.com/labstack/echo/v4"
	"github.com/sugaf1204/gomi/internal/infra/memory"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/osimage"
	"github.com/sugaf1204/gomi/internal/vm"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

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

func TestPXENocloudNetworkConfig_FedoraMachineStaticUsesNetworkdRenderer(t *testing.T) {
	backend := memory.New()
	machineSvc := machine.NewService(backend.Machines())
	now := time.Now().UTC()
	target := machine.Machine{
		Name:         "bm-fedora-static",
		Hostname:     "bm-fedora-static",
		MAC:          "52:54:00:44:00:44",
		Arch:         "amd64",
		Firmware:     machine.FirmwareUEFI,
		IP:           "192.168.2.224",
		IPAssignment: machine.IPAssignmentModeStatic,
		OSPreset: machine.OSPreset{
			Family:   machine.OSType("fedora"),
			Version:  "44",
			ImageRef: "fedora-44-amd64-baremetal",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.Machines().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/pxe/nocloud/525400440044/network-config", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("mac")
	c.SetParamValues("525400440044")

	h := &Handler{machines: machineSvc}
	if err := h.PXENocloudNetworkConfig(c); err != nil {
		t.Fatalf("PXENocloudNetworkConfig: %v", err)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"renderer: networkd",
		`macaddress: "52:54:00:44:00:44"`,
		"192.168.2.224/24",
		"dhcp4: false",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected fedora network-config to contain %q, got:\n%s", want, body)
		}
	}
	if strings.Contains(body, "renderer: NetworkManager") {
		t.Fatalf("fedora network-config must not force NetworkManager, got:\n%s", body)
	}
}

func TestPXENocloudNetworkConfig_FedoraVMStaticUsesNetworkdRenderer(t *testing.T) {
	backend := memory.New()
	vmSvc := vm.NewService(backend.VMs())
	osImageSvc := osimage.NewService(backend.OSImages())
	now := time.Now().UTC()
	if err := backend.OSImages().Upsert(context.Background(), osimage.OSImage{
		Name:      "fedora-44-amd64-baremetal",
		OSFamily:  "fedora",
		OSVersion: "44",
		Arch:      "amd64",
		Ready:     true,
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("upsert os image: %v", err)
	}
	target := vm.VirtualMachine{
		Name:         "vm-fedora-static",
		OSImageRef:   "fedora-44-amd64-baremetal",
		IPAssignment: vm.IPAssignmentStatic,
		Network: []vm.NetworkInterface{
			{MAC: "52:54:00:44:00:45", IPAddress: "192.168.2.225"},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.VMs().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert vm: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/pxe/nocloud/525400440045/network-config", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("mac")
	c.SetParamValues("525400440045")

	h := &Handler{vms: vmSvc, osimages: osImageSvc}
	if err := h.PXENocloudNetworkConfig(c); err != nil {
		t.Fatalf("PXENocloudNetworkConfig: %v", err)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"renderer: networkd",
		`macaddress: "52:54:00:44:00:45"`,
		"192.168.2.225/24",
		"dhcp4: false",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected fedora VM network-config to contain %q, got:\n%s", want, body)
		}
	}
	if strings.Contains(body, "renderer: NetworkManager") {
		t.Fatalf("fedora VM network-config must not force NetworkManager, got:\n%s", body)
	}
}

func TestPXENocloudUserData_FedoraMachineStaticWritesNetworkManagerKeyfile(t *testing.T) {
	backend := memory.New()
	machineSvc := machine.NewService(backend.Machines())
	now := time.Now().UTC()
	deadline := now.Add(30 * time.Minute)
	target := machine.Machine{
		Name:         "bm-fedora-nm",
		Hostname:     "bm-fedora-nm",
		MAC:          "52:54:00:44:00:46",
		Arch:         "amd64",
		Firmware:     machine.FirmwareUEFI,
		IP:           "192.168.2.226",
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
			CompletionToken: "token-fedora-nm",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.Machines().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/pxe/nocloud/525400440046/user-data", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("mac")
	c.SetParamValues("525400440046")

	h := &Handler{machines: machineSvc}
	if err := h.PXENocloudUserData(c); err != nil {
		t.Fatalf("PXENocloudUserData: %v", err)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"/etc/NetworkManager/system-connections/gomi-nic.nmconnection",
		"mac-address=52:54:00:44:00:46",
		"address1=192.168.2.226/24",
		"nmcli connection reload",
		"nmcli connection up 'gomi-nic'",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected Fedora user-data to contain %q, got:\n%s", want, body)
		}
	}
	if strings.Contains(body, "99-gomi-network.yaml") || strings.Contains(body, "netplan apply") {
		t.Fatalf("Fedora must not receive netplan user-data, got:\n%s", body)
	}
	if strings.Contains(body, "resize_rootfs: false") {
		t.Fatalf("non-completed-rootfs Fedora deploy must not disable resize_rootfs, got:\n%s", body)
	}
}

func TestPXENocloudUserData_FedoraVMStaticWritesNetworkManagerKeyfile(t *testing.T) {
	backend := memory.New()
	vmSvc := vm.NewService(backend.VMs())
	osImageSvc := osimage.NewService(backend.OSImages())
	now := time.Now().UTC()
	if err := backend.OSImages().Upsert(context.Background(), osimage.OSImage{
		Name:      "fedora-44-amd64-cloud",
		OSFamily:  "fedora",
		OSVersion: "44",
		Arch:      "amd64",
		Ready:     true,
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("upsert os image: %v", err)
	}
	target := vm.VirtualMachine{
		Name:         "vm-fedora-nm",
		OSImageRef:   "fedora-44-amd64-cloud",
		IPAssignment: vm.IPAssignmentStatic,
		Network: []vm.NetworkInterface{
			{MAC: "52:54:00:44:00:50", IPAddress: "192.168.2.230"},
		},
		Phase: vm.PhaseProvisioning,
		Provisioning: vm.ProvisioningStatus{
			Active:          true,
			CompletionToken: "token-vm-fedora-nm",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.VMs().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert vm: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/pxe/nocloud/525400440050/user-data", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("mac")
	c.SetParamValues("525400440050")

	h := &Handler{vms: vmSvc, osimages: osImageSvc}
	if err := h.PXENocloudUserData(c); err != nil {
		t.Fatalf("PXENocloudUserData: %v", err)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"/etc/NetworkManager/system-connections/gomi-nic.nmconnection",
		"mac-address=52:54:00:44:00:50",
		"address1=192.168.2.230/24",
		"nmcli connection reload",
		"nmcli connection up 'gomi-nic'",
		"install-complete?token=token-vm-fedora-nm",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected Fedora VM user-data to contain %q, got:\n%s", want, body)
		}
	}
	if strings.Contains(body, "99-gomi-network.yaml") || strings.Contains(body, "netplan apply") {
		t.Fatalf("Fedora VM must not receive netplan user-data, got:\n%s", body)
	}
}

func TestPXENocloudNetworkConfig_DebianVMStaticUsesCloudInitV1Ifupdown(t *testing.T) {
	backend := memory.New()
	vmSvc := vm.NewService(backend.VMs())
	osImageSvc := osimage.NewService(backend.OSImages())
	now := time.Now().UTC()
	if err := backend.OSImages().Upsert(context.Background(), osimage.OSImage{
		Name:      "debian-12-amd64-cloud",
		OSFamily:  "debian",
		OSVersion: "12",
		Arch:      "amd64",
		Ready:     true,
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("upsert os image: %v", err)
	}
	target := vm.VirtualMachine{
		Name:         "vm-debian-ifupdown",
		OSImageRef:   "debian-12-amd64-cloud",
		IPAssignment: vm.IPAssignmentStatic,
		Network: []vm.NetworkInterface{
			{MAC: "52:54:00:44:00:51", IPAddress: "192.168.2.231"},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.VMs().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert vm: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/pxe/nocloud/525400440051/network-config", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("mac")
	c.SetParamValues("525400440051")

	h := &Handler{vms: vmSvc, osimages: osImageSvc}
	if err := h.PXENocloudNetworkConfig(c); err != nil {
		t.Fatalf("PXENocloudNetworkConfig: %v", err)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"version: 1",
		"type: physical",
		"name: eth0",
		`mac_address: "52:54:00:44:00:51"`,
		"type: static",
		"address: 192.168.2.231/24",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected Debian VM network-config to contain %q, got:\n%s", want, body)
		}
	}
	if strings.Contains(body, "renderer: networkd") || strings.Contains(body, "ethernets:") {
		t.Fatalf("Debian VM cloud image must not receive netplan v2 network-config, got:\n%s", body)
	}
}

func TestPXENocloudUserData_DebianVMStaticUsesIfupdown(t *testing.T) {
	backend := memory.New()
	vmSvc := vm.NewService(backend.VMs())
	osImageSvc := osimage.NewService(backend.OSImages())
	now := time.Now().UTC()
	if err := backend.OSImages().Upsert(context.Background(), osimage.OSImage{
		Name:      "debian-12-amd64-cloud",
		OSFamily:  "debian",
		OSVersion: "12",
		Arch:      "amd64",
		Ready:     true,
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("upsert os image: %v", err)
	}
	target := vm.VirtualMachine{
		Name:         "vm-debian-ifupdown",
		OSImageRef:   "debian-12-amd64-cloud",
		IPAssignment: vm.IPAssignmentStatic,
		Network: []vm.NetworkInterface{
			{MAC: "52:54:00:44:00:51", IPAddress: "192.168.2.231"},
		},
		Phase: vm.PhaseProvisioning,
		Provisioning: vm.ProvisioningStatus{
			Active:          true,
			CompletionToken: "token-vm-debian-ifupdown",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.VMs().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert vm: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/pxe/nocloud/525400440051/user-data", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("mac")
	c.SetParamValues("525400440051")

	h := &Handler{vms: vmSvc, osimages: osImageSvc}
	if err := h.PXENocloudUserData(c); err != nil {
		t.Fatalf("PXENocloudUserData: %v", err)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"/usr/local/sbin/gomi-apply-debian-ifupdown",
		"/etc/network/interfaces.d/99-gomi.cfg",
		"target_mac='52:54:00:44:00:51'",
		"target_ip='192.168.2.231'",
		"address $target_ip/$prefix_len",
		"install-complete?token=token-vm-debian-ifupdown",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected Debian VM user-data to contain %q, got:\n%s", want, body)
		}
	}
	if strings.Contains(body, "netplan") || strings.Contains(body, "gomi-network-rollback.timer") {
		t.Fatalf("Debian VM cloud image must not receive netplan user-data, got:\n%s", body)
	}
}
