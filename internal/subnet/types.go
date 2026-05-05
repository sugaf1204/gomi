package subnet

import (
	"time"
)

type AddressRange struct {
	Start string `json:"start" yaml:"start"`
	End   string `json:"end" yaml:"end"`
}

type DHCPReservation struct {
	MAC      string `json:"mac" yaml:"mac"`
	IP       string `json:"ip" yaml:"ip"`
	Hostname string `json:"hostname,omitempty" yaml:"hostname,omitempty"`
}

type SubnetSpec struct {
	CIDR             string            `json:"cidr" yaml:"cidr"`
	PXEInterface     string            `json:"pxeInterface,omitempty" yaml:"pxeInterface,omitempty"`
	PXEAddressRange  *AddressRange     `json:"pxeAddressRange,omitempty" yaml:"pxeAddressRange,omitempty"`
	DefaultGateway   string            `json:"defaultGateway,omitempty" yaml:"defaultGateway,omitempty"`
	DNSServers       []string          `json:"dnsServers,omitempty" yaml:"dnsServers,omitempty"`
	DNSSearchDomains []string          `json:"dnsSearchDomains,omitempty" yaml:"dnsSearchDomains,omitempty"`
	VLANID           int               `json:"vlanId,omitempty" yaml:"vlanId,omitempty"`
	ReservedRanges   []AddressRange    `json:"reservedRanges,omitempty" yaml:"reservedRanges,omitempty"`
	Reservations     []DHCPReservation `json:"reservations,omitempty" yaml:"reservations,omitempty"`

	// DHCP options
	LeaseTime  int      `json:"leaseTime,omitempty" yaml:"leaseTime,omitempty"`
	DomainName string   `json:"domainName,omitempty" yaml:"domainName,omitempty"`
	NTPServers []string `json:"ntpServers,omitempty" yaml:"ntpServers,omitempty"`
}

type Subnet struct {
	Name string `json:"name"`

	Spec SubnetSpec `json:"spec"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}
