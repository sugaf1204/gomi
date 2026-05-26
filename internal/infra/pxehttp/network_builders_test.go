package pxehttp

import (
	"github.com/sugaf1204/gomi/internal/subnet"
	"gopkg.in/yaml.v3"
	"strings"
	"testing"
)

func TestBuildNetworkConfig_DHCP(t *testing.T) {
	got := buildNetworkConfig("84:47:09:1f:1c:d6", "", nil)
	if !strings.Contains(got, "renderer: networkd") {
		t.Fatalf("expected networkd renderer, got:\n%s", got)
	}
	if !strings.Contains(got, "macaddress: 84:47:09:1f:1c:d6") {
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
	if !strings.Contains(got, "macaddress: 84:47:09:1f:1c:d6") {
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

func TestBuildBridgedNetworkConfig_Static(t *testing.T) {
	spec := &subnet.SubnetSpec{
		CIDR:           "192.168.2.0/24",
		DefaultGateway: "192.168.2.1",
		DNSServers:     []string{"192.168.2.1"},
	}
	got := buildBridgedNetworkConfig("84:47:09:1f:1c:d6", "br-test", "192.168.2.101", spec)

	var cfg map[string]any
	if err := yaml.Unmarshal([]byte(got), &cfg); err != nil {
		t.Fatalf("yaml.Unmarshal: %v\n%s", err, got)
	}

	bridges, ok := cfg["bridges"].(map[string]any)
	if !ok {
		t.Fatalf("expected bridges in config, got: %#v", cfg)
	}
	bridge, ok := bridges["br-test"].(map[string]any)
	if !ok {
		t.Fatalf("expected br-test bridge in config, got: %#v", bridges)
	}
	if bridge["macaddress"] != "84:47:09:1f:1c:d6" {
		t.Fatalf("expected bridge macaddress, got: %#v", bridge["macaddress"])
	}
	if !strings.Contains(got, "192.168.2.101/24") {
		t.Fatalf("expected static bridge IP, got:\n%s", got)
	}
	if !strings.Contains(got, "192.168.2.1") {
		t.Fatalf("expected gateway or dns in bridged config, got:\n%s", got)
	}
}
