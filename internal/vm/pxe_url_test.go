package vm

import (
	"testing"

	"github.com/sugaf1204/gomi/internal/hypervisor"
)

func TestResolvePXEBaseURL_UsesConfiguredBaseURL(t *testing.T) {
	d := &Deployer{PXEBaseURL: " http://pxe.example:8080/pxe/ "}

	got, err := d.resolvePXEBaseURL(hypervisor.Hypervisor{}, InstallConfigCurtin)
	if err != nil {
		t.Fatalf("resolvePXEBaseURL: %v", err)
	}
	if got != "http://pxe.example:8080/pxe" {
		t.Fatalf("expected configured base URL, got %q", got)
	}
}

func TestResolvePXEBaseURL_DerivesFromConcreteListenAddr(t *testing.T) {
	got, err := resolvePXEBaseURLFromListen("192.168.2.192:8080", func() (string, error) {
		t.Fatal("primary IP detector should not be called for concrete listen addr")
		return "", nil
	})
	if err != nil {
		t.Fatalf("resolvePXEBaseURLFromListen: %v", err)
	}
	if got != "http://192.168.2.192:8080/pxe" {
		t.Fatalf("expected listen-derived PXE URL, got %q", got)
	}
}

func TestResolvePXEBaseURL_DetectsPrimaryIPForWildcardListenAddr(t *testing.T) {
	got, err := resolvePXEBaseURLFromListen("0.0.0.0:8080", func() (string, error) {
		return "192.168.2.192", nil
	})
	if err != nil {
		t.Fatalf("resolvePXEBaseURLFromListen: %v", err)
	}
	if got != "http://192.168.2.192:8080/pxe" {
		t.Fatalf("expected detected primary IP PXE URL, got %q", got)
	}
}

func TestResolvePXEBaseURL_ErrorsForLoopbackListenAddr(t *testing.T) {
	if _, err := resolvePXEBaseURLFromListen("127.0.0.1:8080", func() (string, error) {
		return "192.168.2.192", nil
	}); err == nil {
		t.Fatal("expected loopback listen addr to require explicit PXE base URL")
	}
}
