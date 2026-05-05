package discovery

import "testing"

func TestGenerateName_HostnamePreferred(t *testing.T) {
	name := generateName("aa:bb:cc:dd:ee:01", "MyHost")
	if name != "myhost" {
		t.Fatalf("expected myhost, got %s", name)
	}
}

func TestGenerateName_MACFallback(t *testing.T) {
	name := generateName("AA:BB:CC:DD:EE:01", "")
	if name != "aa-bb-cc-dd-ee-01" {
		t.Fatalf("expected aa-bb-cc-dd-ee-01, got %s", name)
	}
}
