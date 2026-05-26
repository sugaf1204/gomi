package pxehttp

import (
	"context"
	"github.com/labstack/echo/v4"
	"github.com/sugaf1204/gomi/internal/hypervisor"
	"github.com/sugaf1204/gomi/internal/infra/memory"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/power"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

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
		"xz-utils",
		"99-gomi-libvirt-bridge.conf",
		"net.bridge.bridge-nf-call-iptables = 0",
		"net.bridge.bridge-nf-call-arptables = 0",
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

func TestPXENocloudUserData_FedoraHypervisorUsesFedoraPackages(t *testing.T) {
	backend := memory.New()
	machineSvc := machine.NewService(backend.Machines())
	now := time.Now().UTC()

	target := machine.Machine{
		Name:       "fedora-node",
		Hostname:   "fedora-node",
		MAC:        "52:54:00:44:00:01",
		Arch:       "amd64",
		Firmware:   machine.FirmwareUEFI,
		Role:       machine.RoleHypervisor,
		BridgeName: "br0",
		OSPreset: machine.OSPreset{
			Family: machine.OSType("fedora"),
		},
		Phase: machine.PhaseProvisioning,
		Provision: &machine.ProvisionProgress{
			Active:          true,
			CompletionToken: "token-hv-fedora",
			Artifacts: map[string]string{
				machine.ProvisionArtifactHypervisorRegistrationToken: "hv-registration-token-fedora",
			},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.Machines().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/pxe/nocloud/525400440001/user-data", nil)
	req.Host = "192.168.2.254:8080"
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("mac")
	c.SetParamValues("525400440001")

	h := &Handler{machines: machineSvc}
	if err := h.PXENocloudUserData(c); err != nil {
		t.Fatalf("PXENocloudUserData: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"/api/v1/hypervisors/setup-and-register.sh",
		`ARCH=$(uname -m)`,
		"hv-registration-token-fedora",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected Fedora hypervisor user-data to contain %q, got:\n%s", want, body)
		}
	}
	for _, forbidden := range []string{
		"libvirt-daemon-system",
		"libvirt-clients",
		"cloud-image-utils",
		"xz-utils",
		"dpkg --print-architecture",
		"libvirt-daemon-driver-qemu",
		"qemu-system-x86-core",
		"packages: []",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("did not expect Debian-specific %q in Fedora user-data, got:\n%s", forbidden, body)
		}
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
