package subnet

import (
	"testing"
)

func TestValidateSubnet_Valid(t *testing.T) {
	s := Subnet{
		Name: "mgmt",
		Spec: SubnetSpec{
			CIDR:           "10.0.0.0/24",
			DefaultGateway: "10.0.0.1",
			DNSServers:     []string{"8.8.8.8"},
		},
	}
	if err := ValidateSubnet(s); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidateSubnet_MissingCIDR(t *testing.T) {
	s := Subnet{
		Name: "mgmt",
		Spec: SubnetSpec{},
	}
	if err := ValidateSubnet(s); err == nil {
		t.Fatal("expected error for missing CIDR")
	}
}

func TestValidateSubnet_MissingDNS(t *testing.T) {
	s := Subnet{
		Name: "mgmt",
		Spec: SubnetSpec{CIDR: "10.0.0.0/24"},
	}
	if err := ValidateSubnet(s); err == nil {
		t.Fatal("expected error for missing DNS servers")
	}
}

func TestValidateSubnet_InvalidCIDR(t *testing.T) {
	s := Subnet{
		Name: "mgmt",
		Spec: SubnetSpec{CIDR: "not-a-cidr"},
	}
	if err := ValidateSubnet(s); err == nil {
		t.Fatal("expected error for invalid CIDR")
	}
}

func TestValidateSubnet_InvalidGateway(t *testing.T) {
	s := Subnet{
		Name: "mgmt",
		Spec: SubnetSpec{CIDR: "10.0.0.0/24", DefaultGateway: "not-an-ip"},
	}
	if err := ValidateSubnet(s); err == nil {
		t.Fatal("expected error for invalid gateway")
	}
}

func TestValidateSubnet_InvalidDNS(t *testing.T) {
	s := Subnet{
		Name: "mgmt",
		Spec: SubnetSpec{CIDR: "10.0.0.0/24", DNSServers: []string{"not-an-ip"}},
	}
	if err := ValidateSubnet(s); err == nil {
		t.Fatal("expected error for invalid DNS")
	}
}
