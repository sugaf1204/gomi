package pxehttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/sugaf1204/gomi/internal/subnet"
	"github.com/sugaf1204/gomi/internal/vm"
)

// fakeVMServiceForStaticIP implements the minimum needed for the PXE handler.
type fakeVMServiceForStaticIP struct {
	items []vm.VirtualMachine
}

func (f *fakeVMServiceForStaticIP) List(_ context.Context) ([]vm.VirtualMachine, error) {
	return f.items, nil
}
func (f *fakeVMServiceForStaticIP) Get(_ context.Context, name string) (vm.VirtualMachine, error) {
	for _, v := range f.items {
		if v.Name == name {
			return v, nil
		}
	}
	return vm.VirtualMachine{}, nil
}
func (f *fakeVMServiceForStaticIP) Create(_ context.Context, v vm.VirtualMachine) (vm.VirtualMachine, error) {
	return v, nil
}
func (f *fakeVMServiceForStaticIP) Delete(_ context.Context, _ string) error { return nil }
func (f *fakeVMServiceForStaticIP) UpdateStatus(_ context.Context, _ string, _ vm.Phase, _, _ string) (vm.VirtualMachine, error) {
	return vm.VirtualMachine{}, nil
}
func (f *fakeVMServiceForStaticIP) Store() vm.Store { return f }
func (f *fakeVMServiceForStaticIP) ListByHypervisor(_ context.Context, _ string) ([]vm.VirtualMachine, error) {
	return nil, nil
}
func (f *fakeVMServiceForStaticIP) Upsert(_ context.Context, _ vm.VirtualMachine) error {
	return nil
}

// fakeSubnetStoreForStaticIP returns a default subnet.
type fakeSubnetStoreForStaticIP struct {
	subnets []subnet.Subnet
}

func (f *fakeSubnetStoreForStaticIP) Get(_ context.Context, name string) (subnet.Subnet, error) {
	for _, s := range f.subnets {
		if s.Name == name {
			return s, nil
		}
	}
	if len(f.subnets) > 0 {
		return f.subnets[0], nil
	}
	return subnet.Subnet{}, nil
}
func (f *fakeSubnetStoreForStaticIP) List(_ context.Context) ([]subnet.Subnet, error) {
	return f.subnets, nil
}
func (f *fakeSubnetStoreForStaticIP) Upsert(_ context.Context, _ subnet.Subnet) error { return nil }
func (f *fakeSubnetStoreForStaticIP) Delete(_ context.Context, _ string) error        { return nil }

func TestPXENocloudUserData_StaticIPVM_InjectsNetplan(t *testing.T) {
	now := time.Now().UTC()
	deadline := now.Add(30 * time.Minute)

	testVM := vm.VirtualMachine{
		Name:          "test-static-vm",
		HypervisorRef: "hv1",
		IPAssignment:  vm.IPAssignmentStatic,
		Network: []vm.NetworkInterface{
			{
				Bridge:    "br-eth0",
				MAC:       "52:54:00:aa:bb:cc",
				IPAddress: "10.0.0.100",
			},
		},
		InstallCfg: &vm.InstallConfig{
			Type: vm.InstallConfigCurtin,
		},
		Provisioning: vm.ProvisioningStatus{
			Active:          true,
			StartedAt:       &now,
			DeadlineAt:      &deadline,
			CompletionToken: "test-token-123",
		},
	}

	vmSvc := vm.NewService(&fakeVMServiceForStaticIP{items: []vm.VirtualMachine{testVM}})
	subnetStore := &fakeSubnetStoreForStaticIP{
		subnets: []subnet.Subnet{
			{
				Name: "default",
				Spec: subnet.SubnetSpec{
					CIDR:           "10.0.0.0/24",
					DefaultGateway: "10.0.0.1",
					DNSServers:     []string{"8.8.8.8", "8.8.4.4"},
				},
			},
		},
	}

	h := &Handler{
		vms:     vmSvc,
		subnets: subnetStore,
	}

	// Simulate cloud-init fetching user-data via MAC token
	macToken := "525400aabbcc" // normalized hex of 52:54:00:aa:bb:cc
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/pxe/nocloud/"+macToken+"/user-data", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("mac")
	c.SetParamValues(macToken)

	if err := h.PXENocloudUserData(c); err != nil {
		t.Fatalf("PXENocloudUserData returned error: %v", err)
	}

	body := rec.Body.String()

	// Verify the response contains netplan static IP configuration
	if !strings.Contains(body, "99-gomi-network.yaml") {
		t.Fatalf("expected netplan write_files entry in user-data, got:\n%s", body)
	}
	if !strings.Contains(body, "10.0.0.100/24") {
		t.Fatalf("expected static IP 10.0.0.100/24 in netplan config, got:\n%s", body)
	}
	if !strings.Contains(body, "52:54:00:aa:bb:cc") {
		t.Fatalf("expected MAC address in netplan config, got:\n%s", body)
	}
	if !strings.Contains(body, "10.0.0.1") {
		t.Fatalf("expected gateway 10.0.0.1 in netplan config, got:\n%s", body)
	}
	if !strings.Contains(body, "8.8.8.8") {
		t.Fatalf("expected DNS server in netplan config, got:\n%s", body)
	}
	if !strings.Contains(body, "dhcp4: false") {
		t.Fatalf("expected dhcp4: false in netplan config, got:\n%s", body)
	}
	if !strings.Contains(body, "netplan apply") {
		t.Fatalf("expected netplan apply in runcmd, got:\n%s", body)
	}

	t.Logf("user-data output:\n%s", body)
}
