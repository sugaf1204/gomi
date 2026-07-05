package api

import (
	"testing"
	"time"
)

func TestResolveVNCProxyHost(t *testing.T) {
	tests := []struct {
		name           string
		listen         string
		hypervisorHost string
		want           string
		wantErr        bool
	}{
		{name: "empty uses hypervisor host", listen: "", hypervisorHost: "192.0.2.10", want: "192.0.2.10"},
		{name: "wildcard ipv4 uses hypervisor host", listen: "0.0.0.0", hypervisorHost: "192.0.2.10", want: "192.0.2.10"},
		{name: "wildcard ipv6 uses hypervisor host", listen: "::", hypervisorHost: "2001:db8::10", want: "2001:db8::10"},
		{name: "loopback is rejected", listen: "127.0.0.1", hypervisorHost: "192.0.2.10", wantErr: true},
		{name: "specific listen address is preserved", listen: "192.0.2.20", hypervisorHost: "192.0.2.10", want: "192.0.2.20"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveVNCProxyHost(tt.listen, tt.hypervisorHost)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("resolveVNCProxyHost(%q, %q) returned nil error, want error", tt.listen, tt.hypervisorHost)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveVNCProxyHost(%q, %q) returned unexpected error: %v", tt.listen, tt.hypervisorHost, err)
			}
			if got != tt.want {
				t.Fatalf("resolveVNCProxyHost(%q, %q) = %q, want %q", tt.listen, tt.hypervisorHost, got, tt.want)
			}
		})
	}
}

func TestVMConsoleSessionValidation(t *testing.T) {
	s := &Server{consoleSessions: map[string]vmConsoleSession{}}
	s.storeVMConsoleSession("valid", vmConsoleSession{
		VMName:    "vm-1",
		ExpiresAt: time.Now().UTC().Add(time.Minute),
	})
	s.storeVMConsoleSession("expired", vmConsoleSession{
		VMName:    "vm-1",
		ExpiresAt: time.Now().UTC().Add(-time.Minute),
	})

	if !s.validateVMConsoleSession("vm-1", "valid") {
		t.Fatal("expected valid console token")
	}
	if s.validateVMConsoleSession("vm-2", "valid") {
		t.Fatal("expected token to be bound to the original VM")
	}
	if s.validateVMConsoleSession("vm-1", "expired") {
		t.Fatal("expected expired console token to be rejected")
	}
	if s.validateVMConsoleSession("vm-1", "") {
		t.Fatal("expected empty console token to be rejected")
	}
}
