package runner

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestCollectInventoryFromProcAndSysFixtures(t *testing.T) {
	root := t.TempDir()
	procRoot := filepath.Join(root, "proc")
	sysRoot := filepath.Join(root, "sys")
	devRoot := filepath.Join(root, "dev")
	mustWrite(t, filepath.Join(procRoot, "sys/kernel/osrelease"), "6.8.0-test\n")
	mustWrite(t, filepath.Join(sysRoot, "firmware/efi/efivars/.keep"), "")
	mustWrite(t, filepath.Join(sysRoot, "block/vda/size"), "2097152\n")
	mustWrite(t, filepath.Join(sysRoot, "block/vda/removable"), "0\n")
	mustWrite(t, filepath.Join(sysRoot, "block/vda/queue/rotational"), "0\n")
	mustWrite(t, filepath.Join(sysRoot, "class/net/eth0/address"), "52:54:00:00:00:01\n")
	mustWrite(t, filepath.Join(sysRoot, "class/net/eth0/operstate"), "up\n")
	mustWrite(t, filepath.Join(sysRoot, "class/net/eth0/type"), "1\n")

	r := Runner{ProcRoot: procRoot, SysRoot: sysRoot, DevRoot: devRoot, TmpDir: filepath.Join(root, "tmp")}
	inv, err := r.CollectInventory()
	if err != nil {
		t.Fatal(err)
	}
	if inv.Runtime.KernelVersion != "6.8.0-test" {
		t.Fatalf("kernel version = %q", inv.Runtime.KernelVersion)
	}
	if inv.Boot.FirmwareMode != "uefi" || !inv.Boot.EFIVars {
		t.Fatalf("boot info = %+v", inv.Boot)
	}
	if len(inv.Disks) != 1 || inv.Disks[0].Name != "vda" || inv.Disks[0].SizeMB != 1024 {
		t.Fatalf("disks = %+v", inv.Disks)
	}
	if len(inv.NICs) != 1 || inv.NICs[0].Name != "eth0" || inv.NICs[0].MAC != "52:54:00:00:00:01" {
		t.Fatalf("nics = %+v", inv.NICs)
	}
}

func TestRuntimeConfigFromCmdlineUsesExplicitBootMAC(t *testing.T) {
	procRoot := filepath.Join(t.TempDir(), "proc")
	mustWrite(t, filepath.Join(procRoot, "cmdline"), "gomi.base=http://10.0.0.1:8080/pxe gomi.token=token gomi.boot_mac=52:54:00:aa:bb:cc BOOTIF=01-52-54-00-11-22-33\n")

	cfg, err := (&Runner{ProcRoot: procRoot}).runtimeConfigFromCmdline()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL.String() != "http://10.0.0.1:8080/pxe" || cfg.Token != "token" {
		t.Fatalf("config = %+v", cfg)
	}
	if cfg.BootMAC.String() != "52:54:00:aa:bb:cc" {
		t.Fatalf("boot mac = %s", cfg.BootMAC)
	}
}

func TestRuntimeConfigFromCmdlineUsesBOOTIF(t *testing.T) {
	procRoot := filepath.Join(t.TempDir(), "proc")
	mustWrite(t, filepath.Join(procRoot, "cmdline"), "gomi.base=http://10.0.0.1:8080/pxe gomi.token=token BOOTIF=01-52-54-00-aa-bb-cc\n")

	cfg, err := (&Runner{ProcRoot: procRoot}).runtimeConfigFromCmdline()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BootMAC.String() != "52:54:00:aa:bb:cc" {
		t.Fatalf("boot mac = %s", cfg.BootMAC)
	}
}

func TestRuntimeConfigFromCmdlineRejectsInvalidMAC(t *testing.T) {
	procRoot := filepath.Join(t.TempDir(), "proc")
	mustWrite(t, filepath.Join(procRoot, "cmdline"), "gomi.base=http://10.0.0.1:8080/pxe gomi.token=token gomi.boot_mac=invalid\n")

	if _, err := (&Runner{ProcRoot: procRoot}).runtimeConfigFromCmdline(); err == nil {
		t.Fatal("expected invalid MAC error")
	}
}

func TestPostInventoryDecodesJSONResponse(t *testing.T) {
	root := t.TempDir()
	procRoot := filepath.Join(root, "proc")
	sysRoot := filepath.Join(root, "sys")
	devRoot := filepath.Join(root, "dev")
	mustWrite(t, filepath.Join(procRoot, "sys/kernel/osrelease"), "6.8.0-test\n")
	mustWrite(t, filepath.Join(sysRoot, "block/vda/size"), "2097152\n")
	mustWrite(t, filepath.Join(sysRoot, "class/net/eth0/address"), "52:54:00:00:00:01\n")

	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/pxe/inventory" || req.URL.Query().Get("token") != "token" {
			t.Fatalf("unexpected request: %s", req.URL.String())
		}
		var payload map[string]any
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Fatalf("decode inventory: %v", err)
		}
		raw, err := json.Marshal(InventoryResponse{
			AttemptID:       "attempt",
			CurtinConfigURL: "http://gomi.test/pxe/curtin-config",
			EventsURL:       "http://gomi.test/pxe/deploy-events",
		})
		if err != nil {
			t.Fatal(err)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(bytes.NewReader(raw)),
			Header:     make(http.Header),
		}, nil
	})}
	base, err := url.Parse("http://gomi.test/pxe")
	if err != nil {
		t.Fatal(err)
	}

	resp, err := (&Runner{ProcRoot: procRoot, SysRoot: sysRoot, DevRoot: devRoot, TmpDir: filepath.Join(root, "tmp"), Client: client}).postInventory(RuntimeConfig{
		BaseURL: base,
		Token:   "token",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.CurtinConfigURL != "http://gomi.test/pxe/curtin-config" || resp.EventsURL != "http://gomi.test/pxe/deploy-events" {
		t.Fatalf("response = %+v", resp)
	}
}

func mustWrite(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
