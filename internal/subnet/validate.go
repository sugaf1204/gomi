package subnet

import (
	"errors"
	"fmt"
	"net"
	"strings"
)

func ValidateSubnet(s Subnet) error {
	if strings.TrimSpace(s.Name) == "" {
		return errors.New("name is required")
	}
	if strings.TrimSpace(s.Spec.CIDR) == "" {
		return errors.New("spec.cidr is required")
	}
	if len(s.Spec.DNSServers) == 0 {
		return errors.New("spec.dnsServers is required")
	}
	_, _, err := net.ParseCIDR(s.Spec.CIDR)
	if err != nil {
		return fmt.Errorf("spec.cidr is invalid: %w", err)
	}
	if gw := strings.TrimSpace(s.Spec.DefaultGateway); gw != "" {
		if net.ParseIP(gw) == nil {
			return fmt.Errorf("spec.defaultGateway is invalid: %s", gw)
		}
	}
	for _, dns := range s.Spec.DNSServers {
		if net.ParseIP(dns) == nil {
			return fmt.Errorf("spec.dnsServers contains invalid IP: %s", dns)
		}
	}
	if r := s.Spec.PXEAddressRange; r != nil {
		if net.ParseIP(r.Start) == nil {
			return fmt.Errorf("spec.pxeAddressRange.start is invalid: %s", r.Start)
		}
		if net.ParseIP(r.End) == nil {
			return fmt.Errorf("spec.pxeAddressRange.end is invalid: %s", r.End)
		}
	}
	for i, r := range s.Spec.ReservedRanges {
		if net.ParseIP(r.Start) == nil {
			return fmt.Errorf("spec.reservedRanges[%d].start is invalid: %s", i, r.Start)
		}
		if net.ParseIP(r.End) == nil {
			return fmt.Errorf("spec.reservedRanges[%d].end is invalid: %s", i, r.End)
		}
	}

	// DHCP options validation
	if lt := s.Spec.LeaseTime; lt != 0 && (lt < 60 || lt > 604800) {
		return fmt.Errorf("spec.leaseTime must be 0 (default) or between 60 and 604800 seconds: %d", lt)
	}
	if dn := strings.TrimSpace(s.Spec.DomainName); dn != "" {
		if !isValidDomainName(dn) {
			return fmt.Errorf("spec.domainName is invalid: %s", dn)
		}
	}
	for _, ntp := range s.Spec.NTPServers {
		if net.ParseIP(ntp) == nil {
			return fmt.Errorf("spec.ntpServers contains invalid IP: %s", ntp)
		}
	}

	return nil
}

// isValidDomainName performs a basic domain name validation.
func isValidDomainName(name string) bool {
	if len(name) > 253 {
		return false
	}
	labels := strings.Split(name, ".")
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 {
			return false
		}
		for i, c := range label {
			if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' {
				continue
			}
			if c == '-' && i > 0 && i < len(label)-1 {
				continue
			}
			return false
		}
	}
	return true
}
