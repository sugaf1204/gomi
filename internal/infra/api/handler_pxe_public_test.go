package api_test

import (
	"net/http"
	"strings"
	"testing"
)

func TestHealthCheck(t *testing.T) {
	env := setupTestEnv(t)

	rec := doRequest(env.echo, http.MethodGet, "/healthz", nil, "")
	requireStatus(t, rec, http.StatusOK)
	body := parseBody(t, rec)
	if body["status"] != "ok" {
		t.Fatalf("expected status ok, got %v", body["status"])
	}
}

// ---------------------------------------------------------------------------
// PowerOnVM for nonexistent VM returns 404
// ---------------------------------------------------------------------------

func TestPowerOnNonexistentVM(t *testing.T) {
	env := setupTestEnv(t)

	rec := doRequest(env.echo, http.MethodPost, "/api/v1/virtual-machines/nonexistent:powerOn", nil, env.token)
	requireStatus(t, rec, http.StatusNotFound)
}

func TestReinstallPXEVM_UpdatesInstallConfigAndCloudInitRef(t *testing.T) {
	env := setupTestEnv(t)

	hvBody := map[string]any{
		"name": "hv-redeploy",
		"connection": map[string]any{
			"type": "tcp",
			"host": "127.0.0.1",
		},
	}
	rec := doRequest(env.echo, http.MethodPost, "/api/v1/hypervisors", hvBody, env.token)
	requireStatus(t, rec, http.StatusCreated)

	imgBody := map[string]any{
		"name":      "debian-13-amd64",
		"osFamily":  "debian",
		"osVersion": "13",
		"arch":      "amd64",
		"format":    "qcow2",
		"source":    "upload",
	}
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/os-images", imgBody, env.token)
	requireStatus(t, rec, http.StatusCreated)

	vmBody := map[string]any{
		"name":          "vm-redeploy",
		"hypervisorRef": "hv-redeploy",
		"resources": map[string]any{
			"cpuCores": 2,
			"memoryMB": 4096,
			"diskGB":   40,
		},
		"osImageRef": "debian-13-amd64",
	}
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/virtual-machines", vmBody, env.token)
	requireStatus(t, rec, http.StatusCreated)
	createdVM := parseBody(t, rec)
	createdInstallCfg, _ := createdVM["installConfig"].(map[string]any)
	if createdInstallCfg["type"] != "curtin" {
		t.Fatalf("expected Debian 13 VM installConfig.type=curtin, got %v", createdInstallCfg["type"])
	}

	ciBody := map[string]any{
		"name":     "ci-reinstall",
		"userData": "#cloud-config\nhostname: reinstall\n",
	}
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/cloud-init-templates", ciBody, env.token)
	requireStatus(t, rec, http.StatusCreated)

	rec = doRequest(env.echo, http.MethodPost, "/api/v1/subnets", map[string]any{
		"name": "vm-redeploy-net",
		"spec": map[string]any{
			"cidr":         "192.168.30.0/24",
			"dnsServers":   []string{"8.8.8.8"},
			"pxeInterface": "br-lab",
		},
	}, env.token)
	requireStatus(t, rec, http.StatusCreated)

	rec = doRequest(env.echo, http.MethodPost, "/api/v1/virtual-machines/vm-redeploy:redeploy", map[string]any{"confirm": "vm-redeploy"}, env.token)
	if rec.Code == http.StatusForbidden || rec.Code == http.StatusUnauthorized || rec.Code == http.StatusNotFound {
		t.Fatalf("redeploy route should exist and be writable, got status %d", rec.Code)
	}

	reinstallBody := map[string]any{
		"confirm":      "vm-redeploy",
		"cloudInitRef": "ci-reinstall",
		"installConfig": map[string]any{
			"type":   "preseed",
			"inline": "d-i passwd/username string redeploy-user",
		},
		"subnetRef":    "vm-redeploy-net",
		"ipAssignment": "static",
		"ip":           "192.168.30.77",
		"network": []map[string]any{
			{
				"name":      "default",
				"bridge":    "br-lab",
				"network":   "vm-redeploy-net",
				"ipAddress": "192.168.30.77",
			},
		},
		"advancedOptions": map[string]any{
			"cpuMode":    "host-passthrough",
			"diskDriver": "scsi",
			"ioThreads":  2,
		},
	}
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/virtual-machines/vm-redeploy:reinstall", reinstallBody, env.token)
	if rec.Code == http.StatusForbidden || rec.Code == http.StatusUnauthorized || rec.Code == http.StatusNotFound {
		t.Fatalf("reinstall route should exist and be writable, got status %d", rec.Code)
	}

	rec = doRequest(env.echo, http.MethodGet, "/api/v1/virtual-machines/vm-redeploy", nil, env.token)
	requireStatus(t, rec, http.StatusOK)
	body := parseBody(t, rec)
	installCfg, _ := body["installConfig"].(map[string]any)
	if installCfg["type"] != "curtin" {
		t.Fatalf("expected installConfig.type=curtin, got %v", installCfg["type"])
	}
	inline, _ := installCfg["inline"].(string)
	if !strings.Contains(inline, "redeploy-user") {
		t.Fatalf("expected inline install config to be preserved, got %q", inline)
	}
	cloudInitRefs, _ := body["cloudInitRefs"].([]any)
	if len(cloudInitRefs) == 0 || cloudInitRefs[0] != "cloudInitTemplates/ci-reinstall" {
		t.Fatalf("expected cloudInitRefs first item to be ci-reinstall, got %v", cloudInitRefs)
	}
	if body["lastDeployedCloudInitRef"] != "cloudInitTemplates/ci-reinstall" {
		t.Fatalf("expected lastDeployedCloudInitRef=ci-reinstall, got %v", body["lastDeployedCloudInitRef"])
	}
	if body["subnetRef"] != "subnets/vm-redeploy-net" {
		t.Fatalf("expected subnetRef=vm-redeploy-net, got %v", body["subnetRef"])
	}
	if body["ipAssignment"] != "static" {
		t.Fatalf("expected ipAssignment=static, got %v", body["ipAssignment"])
	}
	advancedOptions, _ := body["advancedOptions"].(map[string]any)
	if advancedOptions["cpuMode"] != "host-passthrough" {
		t.Fatalf("expected advancedOptions.cpuMode=host-passthrough, got %v", advancedOptions["cpuMode"])
	}
	if advancedOptions["diskDriver"] != "scsi" {
		t.Fatalf("expected advancedOptions.diskDriver=scsi, got %v", advancedOptions["diskDriver"])
	}
	if advancedOptions["ioThreads"] != float64(2) {
		t.Fatalf("expected advancedOptions.ioThreads=2, got %v", advancedOptions["ioThreads"])
	}
	network, _ := body["network"].([]any)
	if len(network) == 0 {
		t.Fatalf("expected redeploy to keep network config, got %v", body["network"])
	}
	nic, _ := network[0].(map[string]any)
	mac, _ := nic["mac"].(string)
	if !strings.HasPrefix(mac, "52:54:00:") {
		t.Fatalf("expected redeploy to persist a generated KVM MAC, got %q", mac)
	}

	rec = doRequest(env.echo, http.MethodGet, "/api/v1/audit-events?machine=vm-redeploy", nil, env.token)
	requireStatus(t, rec, http.StatusOK)
	auditBody := parseBody(t, rec)
	items := listValues(t, auditBody)
	found := false
	for _, item := range items {
		event, _ := item.(map[string]any)
		if event["action"] == "redeploy-vm" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected redeploy-vm audit event, got %v", items)
	}
}

func TestPXENocloudUserData_FallsBackToCloudInitTemplate(t *testing.T) {
	env := setupTestEnv(t)

	hvBody := map[string]any{
		"name": "hv-pxe-fallback",
		"connection": map[string]any{
			"type": "tcp",
			"host": "127.0.0.1",
		},
	}
	rec := doRequest(env.echo, http.MethodPost, "/api/v1/hypervisors", hvBody, env.token)
	requireStatus(t, rec, http.StatusCreated)

	imgBody := map[string]any{
		"name":      "ubuntu-pxe-fallback",
		"osFamily":  "ubuntu",
		"osVersion": "24.04",
		"arch":      "amd64",
		"format":    "qcow2",
		"source":    "upload",
	}
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/os-images", imgBody, env.token)
	requireStatus(t, rec, http.StatusCreated)

	ciBody := map[string]any{
		"name":     "ci-pxe-fallback",
		"userData": "#cloud-config\nusers:\n  - name: fallback-user\n",
	}
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/cloud-init-templates", ciBody, env.token)
	requireStatus(t, rec, http.StatusCreated)

	ciBody = map[string]any{
		"name":     "ci-pxe-priority",
		"userData": "#cloud-config\nusers:\n  - name: priority-user\n",
	}
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/cloud-init-templates", ciBody, env.token)
	requireStatus(t, rec, http.StatusCreated)

	vmBody := map[string]any{
		"name":          "vm-pxe-fallback",
		"hypervisorRef": "hv-pxe-fallback",
		"resources": map[string]any{
			"cpuCores": 2,
			"memoryMB": 4096,
			"diskGB":   40,
		},
		"osImageRef":    "ubuntu-pxe-fallback",
		"cloudInitRefs": []string{"ci-pxe-fallback"},
		"network": []map[string]any{
			{
				"name": "eth0",
				"mac":  "52:54:00:de:ad:be",
			},
		},
		// installConfig.type is inferred from osImageRef — no explicit type needed.
	}
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/virtual-machines", vmBody, env.token)
	requireStatus(t, rec, http.StatusCreated)

	rec = doRequest(env.echo, http.MethodGet, "/pxe/nocloud/525400deadbe/user-data", nil, "")
	requireStatus(t, rec, http.StatusOK)
	if !strings.Contains(rec.Body.String(), "fallback-user") {
		t.Fatalf("expected cloud-init template fallback user-data, got: %s", rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// List returns empty array when no resources exist
// ---------------------------------------------------------------------------

func TestListEmpty(t *testing.T) {
	env := setupTestEnv(t)

	endpoints := []string{
		"/api/v1/hypervisors",
		"/api/v1/virtual-machines",
		"/api/v1/cloud-init-templates",
		"/api/v1/os-images",
	}

	for _, ep := range endpoints {
		rec := doRequest(env.echo, http.MethodGet, ep, nil, env.token)
		requireStatus(t, rec, http.StatusOK)
		body := parseBody(t, rec)
		items := listValues(t, body)
		if len(items) != 0 {
			t.Fatalf("[%s] expected 0 items, got %d", ep, len(items))
		}
	}
}

// ---------------------------------------------------------------------------
// Public endpoints: register script does not require auth
// ---------------------------------------------------------------------------

func TestSetupAndRegisterScriptPublic(t *testing.T) {
	env := setupTestEnv(t)

	rec := doRequest(env.echo, http.MethodGet, "/api/v1/hypervisors/setup-and-register.sh", nil, "")
	requireStatus(t, rec, http.StatusOK)
	if rec.Body.Len() == 0 {
		t.Fatal("expected non-empty script body")
	}
	body := rec.Body.String()
	if !strings.Contains(body, "qemu-system") {
		t.Fatalf("expected setup script to install qemu-system, got:\n%s", body)
	}
	for _, want := range []string{
		`qemu_system_pkg="qemu-system-$(uname -m)-core"`,
		`x86_64|amd64) qemu_system_pkg="qemu-system-x86-core"`,
		`aarch64|arm64) qemu_system_pkg="qemu-system-aarch64-core"`,
		`dnf -y install libvirt-daemon libvirt-daemon-driver-qemu libvirt-client "$qemu_system_pkg" virt-install cloud-utils-cloud-localds curl jq zstd xz`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected setup script to support architecture-aware Fedora/dnf libvirt package %q, got:\n%s", want, body)
		}
	}
	if !strings.Contains(body, "zstd") {
		t.Fatalf("expected setup script to install zstd for artifact image sync, got:\n%s", body)
	}
	if strings.Contains(body, "qemu-kvm") {
		t.Fatalf("setup script must not request obsolete qemu-kvm package, got:\n%s", body)
	}
	if !strings.Contains(body, `HOSTNAME="${GOMI_HOSTNAME:-$(hostname -f)}"`) {
		t.Fatalf("expected setup script to support GOMI_HOSTNAME override, got:\n%s", body)
	}
	if !strings.Contains(body, `auth_tcp = "none"`) {
		t.Fatalf("expected setup script to configure unauthenticated libvirt TCP, got:\n%s", body)
	}
	for _, want := range []string{
		"99-gomi-libvirt-bridge.conf",
		"net.bridge.bridge-nf-call-iptables = 0",
		"net.bridge.bridge-nf-call-arptables = 0",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected setup script to configure libvirt bridge netfilter %q, got:\n%s", want, body)
		}
	}
	if !strings.Contains(body, `/files/gomi-hypervisor.service`) {
		t.Fatalf("expected setup script to install packaged hypervisor unit file, got:\n%s", body)
	}
	if strings.Contains(body, "cat > /etc/systemd/system/gomi-hypervisor.service") {
		t.Fatalf("setup script must not inline the hypervisor unit file, got:\n%s", body)
	}
}

func TestPXEInstallCompletePublic(t *testing.T) {
	env := setupTestEnv(t)

	rec := doRequest(env.echo, http.MethodPost, "/pxe/install-complete", nil, "")
	requireStatus(t, rec, http.StatusBadRequest)
	body := parseBody(t, rec)
	if body["error"] != "token is required" {
		t.Fatalf("expected token required error, got %v", body["error"])
	}
}

// ---------------------------------------------------------------------------
// Create user
// ---------------------------------------------------------------------------
