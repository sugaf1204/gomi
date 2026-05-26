package pxehttp

import (
	"context"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/vm"
	"regexp"
	"strings"
)

func (h *Handler) findVirtualMachineByProvisionToken(ctx context.Context, token string) (*vm.VirtualMachine, error) {
	normalized := strings.TrimSpace(token)
	if normalized == "" || h.vms == nil {
		return nil, nil
	}
	items, err := h.vms.List(ctx)
	if err != nil {
		return nil, err
	}
	for i := range items {
		if strings.TrimSpace(items[i].Provisioning.CompletionToken) == normalized {
			copy := items[i]
			return &copy, nil
		}
	}
	return nil, nil
}

func (h *Handler) findMachineByProvisionToken(ctx context.Context, token string) (*machine.Machine, error) {
	normalized := strings.TrimSpace(token)
	if normalized == "" || h.machines == nil {
		return nil, nil
	}
	items, err := h.machines.List(ctx)
	if err != nil {
		return nil, err
	}
	for i := range items {
		if items[i].Provision == nil {
			continue
		}
		if strings.TrimSpace(items[i].Provision.CompletionToken) == normalized {
			copy := items[i]
			return &copy, nil
		}
	}
	return nil, nil
}

func (h *Handler) findVirtualMachineByMAC(ctx context.Context, rawMAC string) (*vm.VirtualMachine, bool, error) {
	normalized := normalizeMAC(rawMAC)
	token := macToken(rawMAC)
	if normalized == "" && token == "" {
		return nil, false, nil
	}
	if h.vms == nil {
		return nil, true, nil
	}

	items, err := h.vms.List(ctx)
	if err != nil {
		return nil, true, err
	}
	for i := range items {
		if vmHasMAC(items[i], normalized, token) {
			copy := items[i]
			return &copy, true, nil
		}
	}
	return nil, true, nil
}

func vmHasMAC(vmi vm.VirtualMachine, normalized, token string) bool {
	matches := func(raw string) bool {
		candidate := normalizeMAC(raw)
		if normalized != "" && candidate == normalized {
			return true
		}
		if token != "" && macToken(candidate) == token {
			return true
		}
		return false
	}

	for _, nic := range vmi.Network {
		if matches(nic.MAC) {
			return true
		}
	}
	for _, nic := range vmi.NetworkInterfaces {
		if matches(nic.MAC) {
			return true
		}
	}
	return false
}

func (h *Handler) findMachineByMAC(ctx context.Context, rawMAC string) (*machine.Machine, bool, error) {
	normalized := normalizeMAC(rawMAC)
	token := macToken(rawMAC)
	if normalized == "" && token == "" {
		return nil, false, nil
	}
	if h.machines == nil {
		return nil, true, nil
	}

	items, err := h.machines.List(ctx)
	if err != nil {
		return nil, true, err
	}
	for i := range items {
		if machineHasMAC(items[i], normalized, token) {
			copy := items[i]
			return &copy, true, nil
		}
	}
	return nil, true, nil
}

func machineHasMAC(m machine.Machine, normalized, token string) bool {
	candidate := normalizeMAC(m.MAC)
	if normalized != "" && candidate == normalized {
		return true
	}
	if token != "" && macToken(candidate) == token {
		return true
	}
	return false
}

var nonHexPattern = regexp.MustCompile(`[^0-9a-f]`)

func normalizeMAC(raw string) string {
	m := strings.ToLower(strings.TrimSpace(raw))
	if m == "" {
		return ""
	}
	m = strings.ReplaceAll(m, "-", ":")
	if strings.Count(m, ":") == 5 {
		return m
	}
	token := macToken(m)
	if len(token) != 12 {
		return ""
	}
	parts := make([]string, 0, 6)
	for i := 0; i < len(token); i += 2 {
		parts = append(parts, token[i:i+2])
	}
	return strings.Join(parts, ":")
}

func macToken(raw string) string {
	m := strings.ToLower(strings.TrimSpace(raw))
	if m == "" {
		return ""
	}
	return nonHexPattern.ReplaceAllString(m, "")
}

func (h *Handler) leaseIPsByMAC(ctx context.Context) (map[string]string, error) {
	if h.leaseStore == nil {
		return nil, nil
	}
	leases, err := h.leaseStore.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(leases))
	for _, lease := range leases {
		mac := strings.ToLower(strings.TrimSpace(lease.MAC))
		ip := strings.TrimSpace(lease.IP)
		if mac == "" || ip == "" {
			continue
		}
		out[mac] = ip
	}
	return out, nil
}
