package api_test

import (
	"net/http"
	"strings"
	"testing"
)

func TestHypervisorCRUD(t *testing.T) {
	env := setupTestEnv(t)

	// POST /api/v1/hypervisors - Create
	hvBody := map[string]any{
		"name": "hv-01",
		"connection": map[string]any{
			"type": "tcp",
			"host": "127.0.0.1",
			"port": 16509,
		},
	}
	rec := doRequest(env.echo, http.MethodPost, "/api/v1/hypervisors", hvBody, env.token)
	requireStatus(t, rec, http.StatusCreated)
	created := parseBody(t, rec)
	if created["name"] != "hypervisors/hv-01" {
		t.Fatalf("expected name hv-01, got %v", created["name"])
	}

	// GET /api/v1/hypervisors - List
	rec = doRequest(env.echo, http.MethodGet, "/api/v1/hypervisors", nil, env.token)
	requireStatus(t, rec, http.StatusOK)
	body := parseBody(t, rec)
	items := listValues(t, body)
	if len(items) != 1 {
		t.Fatalf("expected 1 hypervisor, got %v", body)
	}

	// GET /api/v1/hypervisors/hv-01 - Get
	rec = doRequest(env.echo, http.MethodGet, "/api/v1/hypervisors/hv-01", nil, env.token)
	requireStatus(t, rec, http.StatusOK)
	got := parseBody(t, rec)
	if got["name"] != "hypervisors/hv-01" {
		t.Fatalf("expected name hv-01, got %v", got["name"])
	}

	// DELETE /api/v1/hypervisors/hv-01 - Delete
	rec = doRequest(env.echo, http.MethodDelete, "/api/v1/hypervisors/hv-01", nil, env.token)
	requireStatus(t, rec, http.StatusNoContent)

	// GET after delete - should 404
	rec = doRequest(env.echo, http.MethodGet, "/api/v1/hypervisors/hv-01", nil, env.token)
	requireStatus(t, rec, http.StatusNotFound)
}

// ---------------------------------------------------------------------------
// Hypervisor Registration Flow Tests
// ---------------------------------------------------------------------------

func TestHypervisorRegistration(t *testing.T) {
	env := setupTestEnv(t)

	// Create registration token.
	rec := doRequest(env.echo, http.MethodPost, "/api/v1/hypervisors/registration-tokens", nil, env.token)
	requireStatus(t, rec, http.StatusCreated)
	tokenBody := parseBody(t, rec)
	regToken, ok := tokenBody["token"].(string)
	if !ok || regToken == "" {
		t.Fatalf("expected non-empty token, got %v", tokenBody)
	}

	// Register hypervisor with valid token (unauthenticated endpoint).
	regReq := map[string]any{
		"token":    regToken,
		"hostname": "hv-registered-01",
		"connection": map[string]any{
			"type": "tcp",
			"host": "127.0.0.1",
			"port": 16509,
		},
		"capacity": map[string]any{
			"cpuCores": 8,
			"memoryMB": 16384,
		},
	}
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/hypervisors/register", regReq, "")
	requireStatus(t, rec, http.StatusCreated)
	registered := parseBody(t, rec)
	regHV, _ := registered["hypervisor"].(map[string]any)
	if regHV["name"] != "hypervisors/hv-registered-01" {
		t.Fatalf("expected name hv-registered-01, got %v", regHV["name"])
	}

	// Register with same token again - should fail (token already used).
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/hypervisors/register", regReq, "")
	requireStatus(t, rec, http.StatusBadRequest)
	errBody := parseBody(t, rec)
	if _, hasErr := errBody["error"]; !hasErr {
		t.Fatalf("expected error in response body for used token")
	}

	// Register with invalid token - should fail.
	regReq["token"] = "definitely-not-valid"
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/hypervisors/register", regReq, "")
	requireStatus(t, rec, http.StatusBadRequest)
}

// ---------------------------------------------------------------------------
// VirtualMachine CRUD Tests
// ---------------------------------------------------------------------------

func TestVirtualMachineCRUD(t *testing.T) {
	env := setupTestEnv(t)

	// Create a hypervisor first (prerequisite).
	hvBody := map[string]any{
		"name": "hv-for-vm",
		"connection": map[string]any{
			"type": "tcp",
			"host": "127.0.0.1",
		},
	}
	rec := doRequest(env.echo, http.MethodPost, "/api/v1/hypervisors", hvBody, env.token)
	requireStatus(t, rec, http.StatusCreated)

	// Create an OS image prerequisite for PXE install type resolution.
	imgBody := map[string]any{
		"name":      "ubuntu-22.04",
		"osFamily":  "ubuntu",
		"osVersion": "22.04",
		"arch":      "amd64",
		"format":    "qcow2",
		"variant":   "cloud",
		"source":    "upload",
	}
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/os-images", imgBody, env.token)
	requireStatus(t, rec, http.StatusCreated)

	// POST /api/v1/virtual-machines - Create VM
	vmBody := map[string]any{
		"name":          "vm-01",
		"hypervisorRef": "hv-for-vm",
		"resources": map[string]any{
			"cpuCores": 2,
			"memoryMB": 4096,
			"diskGB":   40,
		},
		"osImageRef": "ubuntu-22.04",
	}
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/virtual-machines", vmBody, env.token)
	requireStatus(t, rec, http.StatusCreated)
	created := parseBody(t, rec)
	if created["name"] != "virtualMachines/vm-01" {
		t.Fatalf("expected name vm-01, got %v", created["name"])
	}
	installCfg, _ := created["installConfig"].(map[string]any)
	if installCfg["type"] != "curtin" {
		t.Fatalf("expected installConfig.type=curtin for ubuntu image, got %v", installCfg["type"])
	}
	provisioning, _ := created["provisioning"].(map[string]any)
	if active, _ := provisioning["active"].(bool); !active {
		t.Fatalf("expected provisioning.active=true, got %v", provisioning["active"])
	}
	if _, ok := provisioning["completionToken"]; ok {
		t.Fatalf("expected provisioning.completionToken to be redacted, got %v", provisioning)
	}

	bareMetalSquashFSBody := map[string]any{
		"name":      "ubuntu-22.04-baremetal",
		"osFamily":  "ubuntu",
		"osVersion": "22.04",
		"arch":      "amd64",
		"format":    "squashfs",
		"source":    "upload",
		"manifest": map[string]any{
			"capabilities": map[string]any{
				"deployTargets": []string{"baremetal"},
			},
			"root": map[string]any{
				"format": "squashfs",
				"path":   "rootfs.squashfs",
			},
		},
	}
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/os-images", bareMetalSquashFSBody, env.token)
	requireStatus(t, rec, http.StatusCreated)
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/virtual-machines", map[string]any{
		"name":          "vm-baremetal-squashfs",
		"hypervisorRef": "hv-for-vm",
		"resources": map[string]any{
			"cpuCores": 1,
			"memoryMB": 1024,
			"diskGB":   10,
		},
		"osImageRef": "ubuntu-22.04-baremetal",
	}, env.token)
	requireStatus(t, rec, http.StatusBadRequest)
	if !strings.Contains(rec.Body.String(), "cloudimage deployment requires qcow2 OS image") {
		t.Fatalf("expected VM qcow2 validation error, got: %s", rec.Body.String())
	}

	bareMetalQCOW2Body := map[string]any{
		"name":      "ubuntu-22.04-baremetal-qcow2",
		"osFamily":  "ubuntu",
		"osVersion": "22.04",
		"arch":      "amd64",
		"format":    "qcow2",
		"variant":   "baremetal",
		"source":    "upload",
		"manifest": map[string]any{
			"root": map[string]any{
				"format": "qcow2",
				"path":   "root.qcow2",
				"rootPartition": map[string]any{
					"number":     1,
					"filesystem": "ext4",
				},
			},
			"build": map[string]any{
				"modulePackages": []string{"linux-modules-extra-{kernel_release}"},
			},
		},
	}
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/os-images", bareMetalQCOW2Body, env.token)
	requireStatus(t, rec, http.StatusCreated)

	bareMetalQCOW2VMBody := map[string]any{
		"name":          "vm-baremetal-qcow2",
		"hypervisorRef": "hv-for-vm",
		"resources": map[string]any{
			"cpuCores": 1,
			"memoryMB": 1024,
			"diskGB":   10,
		},
		"osImageRef": "ubuntu-22.04-baremetal-qcow2",
	}
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/virtual-machines", bareMetalQCOW2VMBody, env.token)
	requireStatus(t, rec, http.StatusBadRequest)
	if !strings.Contains(rec.Body.String(), "cloudimage deployment requires cloud OS image variant, got baremetal") {
		t.Fatalf("expected cloud-variant validation error, got: %s", rec.Body.String())
	}

	// POST - Create VM referencing non-existent hypervisor
	badVMBody := map[string]any{
		"name":          "vm-bad-ref",
		"hypervisorRef": "nonexistent-hv",
		"resources": map[string]any{
			"cpuCores": 1,
			"memoryMB": 1024,
			"diskGB":   10,
		},
		"osImageRef": "ubuntu-22.04",
	}
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/virtual-machines", badVMBody, env.token)
	requireStatus(t, rec, http.StatusBadRequest)

	// GET /api/v1/virtual-machines - List
	rec = doRequest(env.echo, http.MethodGet, "/api/v1/virtual-machines", nil, env.token)
	requireStatus(t, rec, http.StatusOK)
	body := parseBody(t, rec)
	items := listValues(t, body)
	if len(items) != 1 {
		t.Fatalf("expected 1 vm, got %v", body)
	}

	// GET /api/v1/virtual-machines/vm-01 - Get
	rec = doRequest(env.echo, http.MethodGet, "/api/v1/virtual-machines/vm-01", nil, env.token)
	requireStatus(t, rec, http.StatusOK)
	got := parseBody(t, rec)
	if got["name"] != "virtualMachines/vm-01" {
		t.Fatalf("expected name vm-01, got %v", got["name"])
	}

	// POST /api/v1/virtual-machines/vm-01:powerOn - Power action
	// This will fail at the libvirt level (no real hypervisor), but should not 403
	// and should not panic. Expect an internal server error since there's no SSH key.
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/virtual-machines/vm-01:powerOn", nil, env.token)
	// Accept 500 (libvirt/SSH failure) but NOT 403/401
	if rec.Code == http.StatusForbidden || rec.Code == http.StatusUnauthorized {
		t.Fatalf("authenticated user should be allowed for power-on, got status %d", rec.Code)
	}

	// POST /api/v1/virtual-machines/vm-01:redeploy
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/virtual-machines/vm-01:redeploy", map[string]any{"confirm": "vm-01"}, env.token)
	if rec.Code == http.StatusForbidden || rec.Code == http.StatusUnauthorized || rec.Code == http.StatusNotFound {
		t.Fatalf("redeploy route should be reachable, got status %d", rec.Code)
	}

	// POST /api/v1/virtual-machines/vm-01:reinstall
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/virtual-machines/vm-01:reinstall", map[string]any{"confirm": "vm-01"}, env.token)
	if rec.Code == http.StatusForbidden || rec.Code == http.StatusUnauthorized || rec.Code == http.StatusNotFound {
		t.Fatalf("reinstall route should be reachable, got status %d", rec.Code)
	}

	// DELETE /api/v1/virtual-machines/vm-01 - Delete
	rec = doRequest(env.echo, http.MethodDelete, "/api/v1/virtual-machines/vm-01", nil, env.token)
	requireStatus(t, rec, http.StatusNoContent)

	// Verify deleted
	rec = doRequest(env.echo, http.MethodGet, "/api/v1/virtual-machines/vm-01", nil, env.token)
	requireStatus(t, rec, http.StatusNotFound)
}

func TestCreateVirtualMachine_AllowsNonUbuntuCloudImagesAndRejectsBareMetalTargets(t *testing.T) {
	env := setupTestEnv(t)

	hvBody := map[string]any{
		"name": "hv-multi-os-vm",
		"connection": map[string]any{
			"type": "tcp",
			"host": "127.0.0.1",
		},
	}
	rec := doRequest(env.echo, http.MethodPost, "/api/v1/hypervisors", hvBody, env.token)
	requireStatus(t, rec, http.StatusCreated)

	for _, img := range []map[string]any{
		{
			"name":      "debian-13-cloud",
			"osFamily":  "debian",
			"osVersion": "13",
			"arch":      "amd64",
			"format":    "qcow2",
			"variant":   "cloud",
			"source":    "upload",
		},
		{
			"name":      "fedora-44-cloud",
			"osFamily":  "fedora",
			"osVersion": "44",
			"arch":      "amd64",
			"format":    "qcow2",
			"variant":   "cloud",
			"source":    "upload",
		},
		{
			"name":      "rhel-10-cloud",
			"osFamily":  "rhel",
			"osVersion": "10",
			"arch":      "amd64",
			"format":    "qcow2",
			"source":    "upload",
			"manifest": map[string]any{
				"capabilities": map[string]any{
					"deployTargets": []string{"vm"},
				},
				"root": map[string]any{
					"format": "qcow2",
					"path":   "root.qcow2",
				},
			},
		},
	} {
		rec = doRequest(env.echo, http.MethodPost, "/api/v1/os-images", img, env.token)
		requireStatus(t, rec, http.StatusCreated)

		name := "vm-" + img["name"].(string)
		vmBody := map[string]any{
			"name":          name,
			"hypervisorRef": "hv-multi-os-vm",
			"resources": map[string]any{
				"cpuCores": 1,
				"memoryMB": 1024,
				"diskGB":   10,
			},
			"osImageRef": img["name"],
		}
		rec = doRequest(env.echo, http.MethodPost, "/api/v1/virtual-machines", vmBody, env.token)
		requireStatus(t, rec, http.StatusCreated)
		created := parseBody(t, rec)
		installCfg, _ := created["installConfig"].(map[string]any)
		if installCfg["type"] != "curtin" {
			t.Fatalf("expected %s installConfig.type=curtin, got %v", name, installCfg["type"])
		}
	}

	bareMetalOnly := map[string]any{
		"name":      "debian-13-baremetal-qcow2",
		"osFamily":  "debian",
		"osVersion": "13",
		"arch":      "amd64",
		"format":    "qcow2",
		"source":    "upload",
		"manifest": map[string]any{
			"capabilities": map[string]any{
				"deployTargets": []string{"baremetal"},
			},
			"root": map[string]any{
				"format": "qcow2",
				"path":   "root.qcow2",
				"rootPartition": map[string]any{
					"number":     1,
					"filesystem": "ext4",
				},
			},
		},
	}
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/os-images", bareMetalOnly, env.token)
	requireStatus(t, rec, http.StatusCreated)
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/virtual-machines", map[string]any{
		"name":          "vm-debian-baremetal-only",
		"hypervisorRef": "hv-multi-os-vm",
		"resources": map[string]any{
			"cpuCores": 1,
			"memoryMB": 1024,
			"diskGB":   10,
		},
		"osImageRef": "debian-13-baremetal-qcow2",
	}, env.token)
	requireStatus(t, rec, http.StatusBadRequest)
	if !strings.Contains(rec.Body.String(), "cloudimage deployment requires vm-capable OS image") {
		t.Fatalf("expected vm-capable validation error, got: %s", rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// CloudInitTemplate CRUD Tests
// ---------------------------------------------------------------------------
