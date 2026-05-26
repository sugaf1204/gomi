package pxehttp

import (
	"context"
	"github.com/labstack/echo/v4"
	"github.com/sugaf1204/gomi/internal/infra/memory"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/sshkey"
	"github.com/sugaf1204/gomi/internal/vm"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

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
	if !strings.Contains(body, "curl -fsS --connect-timeout 5 --max-time 15") {
		t.Fatalf("expected bounded install-complete curl timeout, got: %s", body)
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

func TestPXENocloudUserData_MachineLoginUserPasswordEnablesSSHPWAuth(t *testing.T) {
	backend := memory.New()
	machineSvc := machine.NewService(backend.Machines())
	now := time.Now().UTC()
	target := machine.Machine{
		Name:     "bm-password",
		Hostname: "bm-password",
		MAC:      "52:54:00:aa:bb:cc",
		Arch:     "amd64",
		Firmware: machine.FirmwareUEFI,
		LoginUser: &machine.LoginUserSpec{
			Username: "gomi",
			Password: "gomi",
		},
		OSPreset: machine.OSPreset{
			Family: machine.OSTypeUbuntu,
		},
		Phase: machine.PhaseProvisioning,
		Provision: &machine.ProvisionProgress{
			Active:          true,
			CompletionToken: "token-password-login",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.Machines().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/pxe/nocloud/525400aabbcc/user-data", nil)
	req.Host = "192.168.2.254:8080"
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("mac")
	c.SetParamValues("525400aabbcc")

	h := &Handler{machines: machineSvc}
	if err := h.PXENocloudUserData(c); err != nil {
		t.Fatalf("PXENocloudUserData: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"name: gomi",
		"plain_text_passwd: gomi",
		"lock_passwd: false",
		"ssh_pwauth: true",
		"chpasswd:",
		"expire: false",
		"name: gomi",
		"password: gomi",
		"type: text",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected %q in user-data, got: %s", want, body)
		}
	}
}

func TestPXENocloudUserData_SelectedSSHKeysDefaultToOSUser(t *testing.T) {
	backend := memory.New()
	machineSvc := machine.NewService(backend.Machines())
	sshKeySvc := sshkey.NewService(backend.SSHKeys())
	now := time.Now().UTC()
	target := machine.Machine{
		Name:       "bm-default-key",
		Hostname:   "bm-default-key",
		MAC:        "52:54:00:aa:bb:dd",
		Arch:       "amd64",
		Firmware:   machine.FirmwareUEFI,
		SSHKeyRefs: []string{"alice"},
		OSPreset: machine.OSPreset{
			Family: machine.OSTypeUbuntu,
		},
		Phase: machine.PhaseProvisioning,
		Provision: &machine.ProvisionProgress{
			Active:          true,
			CompletionToken: "token-default-key",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.Machines().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}
	if err := backend.SSHKeys().Upsert(context.Background(), sshkey.SSHKey{Name: "alice", PublicKey: "ssh-ed25519 AAAA test"}); err != nil {
		t.Fatalf("upsert ssh key: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/pxe/nocloud/525400aabbdd/user-data", nil)
	req.Host = "192.168.2.254:8080"
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("mac")
	c.SetParamValues("525400aabbdd")

	h := &Handler{machines: machineSvc, sshkeys: sshKeySvc}
	if err := h.PXENocloudUserData(c); err != nil {
		t.Fatalf("PXENocloudUserData: %v", err)
	}
	cfg := parseCloudConfigBody(t, rec.Body.String())
	keys, ok := cfg["ssh_authorized_keys"].([]any)
	if !ok || len(keys) != 1 || keys[0] != "ssh-ed25519 AAAA test" {
		t.Fatalf("top-level ssh_authorized_keys = %#v, want selected key", cfg["ssh_authorized_keys"])
	}
}

func TestPXENocloudUserData_SelectedSSHKeysTargetLoginUserOnly(t *testing.T) {
	backend := memory.New()
	machineSvc := machine.NewService(backend.Machines())
	sshKeySvc := sshkey.NewService(backend.SSHKeys())
	now := time.Now().UTC()
	target := machine.Machine{
		Name:       "bm-login-key",
		Hostname:   "bm-login-key",
		MAC:        "52:54:00:aa:bb:ee",
		Arch:       "amd64",
		Firmware:   machine.FirmwareUEFI,
		SSHKeyRefs: []string{"alice"},
		LoginUser:  &machine.LoginUserSpec{Username: "admin"},
		OSPreset: machine.OSPreset{
			Family: machine.OSTypeUbuntu,
		},
		Phase: machine.PhaseProvisioning,
		Provision: &machine.ProvisionProgress{
			Active:          true,
			CompletionToken: "token-login-key",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.Machines().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}
	if err := backend.SSHKeys().Upsert(context.Background(), sshkey.SSHKey{Name: "alice", PublicKey: "ssh-ed25519 AAAA test"}); err != nil {
		t.Fatalf("upsert ssh key: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/pxe/nocloud/525400aabbee/user-data", nil)
	req.Host = "192.168.2.254:8080"
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("mac")
	c.SetParamValues("525400aabbee")

	h := &Handler{machines: machineSvc, sshkeys: sshKeySvc}
	if err := h.PXENocloudUserData(c); err != nil {
		t.Fatalf("PXENocloudUserData: %v", err)
	}
	cfg := parseCloudConfigBody(t, rec.Body.String())
	if _, has := cfg["ssh_authorized_keys"]; has {
		t.Fatalf("top-level ssh_authorized_keys must be absent when loginUser is set:\n%s", rec.Body.String())
	}
	users, ok := cfg["users"].([]any)
	if !ok || len(users) != 2 {
		t.Fatalf("users = %#v, want default plus login user", cfg["users"])
	}
	entry, ok := users[1].(map[string]any)
	if !ok {
		t.Fatalf("users[1] = %#v, want map", users[1])
	}
	keys, ok := entry["ssh_authorized_keys"].([]any)
	if !ok || len(keys) != 1 || keys[0] != "ssh-ed25519 AAAA test" {
		t.Fatalf("login user ssh_authorized_keys = %#v, want selected key", entry["ssh_authorized_keys"])
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
