package hypervisor

import (
	"testing"
)

func validHypervisor() Hypervisor {
	return Hypervisor{
		Name: "hv-01",
		Connection: ConnectionSpec{
			Type: ConnectionSSH,
			Host: "192.168.1.100",
			Port: 22,
			User: "root",
		},
	}
}

func TestValidateHypervisor_Valid(t *testing.T) {
	if err := ValidateHypervisor(validHypervisor()); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidateHypervisor_MissingName(t *testing.T) {
	h := validHypervisor()
	h.Name = ""
	if err := ValidateHypervisor(h); err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestValidateHypervisor_MissingHost(t *testing.T) {
	h := validHypervisor()
	h.Connection.Host = ""
	if err := ValidateHypervisor(h); err == nil {
		t.Fatal("expected error for missing host")
	}
}

func TestValidateRegisterRequest_Valid(t *testing.T) {
	req := RegisterRequest{
		Token:    "test-token",
		Hostname: "hv-01",
		Connection: ConnectionSpec{Host: "192.168.1.100"},
		Capacity: ResourceInfo{CPUCores: 8, MemoryMB: 16384},
	}
	if err := ValidateRegisterRequest(req); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidateRegisterRequest_MissingToken(t *testing.T) {
	req := RegisterRequest{
		Hostname:   "hv-01",
		Connection: ConnectionSpec{Host: "192.168.1.100"},
		Capacity:   ResourceInfo{CPUCores: 8, MemoryMB: 16384},
	}
	if err := ValidateRegisterRequest(req); err == nil {
		t.Fatal("expected error for missing token")
	}
}

func TestValidateRegisterRequest_InvalidCPU(t *testing.T) {
	req := RegisterRequest{
		Token:      "test-token",
		Hostname:   "hv-01",
		Connection: ConnectionSpec{Host: "192.168.1.100"},
		Capacity:   ResourceInfo{CPUCores: 0, MemoryMB: 16384},
	}
	if err := ValidateRegisterRequest(req); err == nil {
		t.Fatal("expected error for zero cpuCores")
	}
}
