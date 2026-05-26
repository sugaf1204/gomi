package libvirt

import (
	"context"
	"fmt"
	"sort"
	"strings"

	golibvirt "github.com/digitalocean/go-libvirt"
)

func (e *rpcExecutor) DomainInfo(_ context.Context, name string) (*DomainInfo, error) {
	domain, err := e.l.DomainLookupByName(name)
	if err != nil {
		return nil, fmt.Errorf("lookup domain %s: %w", name, err)
	}

	rState, _, _, _, _, err := e.l.DomainGetInfo(domain)
	if err != nil {
		return nil, fmt.Errorf("get domain info %s: %w", name, err)
	}

	return &DomainInfo{
		Name:  name,
		UUID:  uuidToString(domain.UUID),
		State: mapDomainState(rState),
	}, nil
}

func (e *rpcExecutor) DomainInterfaces(_ context.Context, name string) ([]InterfaceInfo, error) {
	domain, err := e.l.DomainLookupByName(name)
	if err != nil {
		return nil, fmt.Errorf("lookup domain %s: %w", name, err)
	}

	byKey := map[string]*InterfaceInfo{}

	xmlDesc, err := e.l.DomainGetXMLDesc(domain, 0)
	if err == nil {
		for _, nic := range parseDomainInterfacesFromXML(xmlDesc) {
			key := nic.MAC
			if key == "" {
				key = nic.Name
			}
			if key == "" {
				continue
			}
			copy := nic
			byKey[key] = &copy
		}
	}

	sources := []golibvirt.DomainInterfaceAddressesSource{
		golibvirt.DomainInterfaceAddressesSrcAgent,
		golibvirt.DomainInterfaceAddressesSrcLease,
		golibvirt.DomainInterfaceAddressesSrcArp,
	}
	for _, source := range sources {
		ifaces, err := e.l.DomainInterfaceAddresses(domain, uint32(source), 0)
		if err != nil {
			continue
		}
		for _, iface := range ifaces {
			name := strings.TrimSpace(iface.Name)
			mac := optStringValue(iface.Hwaddr)
			key := mac
			if key == "" {
				key = name
			}
			if key == "" {
				continue
			}
			current, ok := byKey[key]
			if !ok {
				current = &InterfaceInfo{}
				byKey[key] = current
			}
			if current.Name == "" {
				current.Name = name
			}
			if current.MAC == "" {
				current.MAC = mac
			}
			for _, addr := range iface.Addrs {
				ip := strings.TrimSpace(addr.Addr)
				if ip == "" {
					continue
				}
				appendUnique(&current.IPAddresses, ip)
			}
		}
	}

	result := make([]InterfaceInfo, 0, len(byKey))
	for _, nic := range byKey {
		sort.Strings(nic.IPAddresses)
		result = append(result, *nic)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Name != result[j].Name {
			return result[i].Name < result[j].Name
		}
		return result[i].MAC < result[j].MAC
	})
	return result, nil
}

func (e *rpcExecutor) DomainGraphicsInfo(_ context.Context, name string) (*GraphicsInfo, error) {
	domain, err := e.l.DomainLookupByName(name)
	if err != nil {
		return nil, fmt.Errorf("lookup domain %s: %w", name, err)
	}
	// Flag 0 = running XML (not DomainXMLInactive), which contains the actual VNC port.
	xmlDesc, err := e.l.DomainGetXMLDesc(domain, 0)
	if err != nil {
		return nil, fmt.Errorf("get domain xml %s: %w", name, err)
	}
	return parseDomainGraphicsFromXML(xmlDesc)
}

func (e *rpcExecutor) ListDomains(_ context.Context) ([]DomainInfo, error) {
	flags := golibvirt.ConnectListDomainsActive | golibvirt.ConnectListDomainsInactive
	domains, _, err := e.l.ConnectListAllDomains(1, flags)
	if err != nil {
		return nil, fmt.Errorf("list domains: %w", err)
	}

	var result []DomainInfo
	for _, d := range domains {
		rState, _, _, _, _, err := e.l.DomainGetInfo(d)
		if err != nil {
			continue
		}
		result = append(result, DomainInfo{
			Name:  d.Name,
			UUID:  uuidToString(d.UUID),
			State: mapDomainState(rState),
		})
	}
	return result, nil
}

// mapDomainState converts a libvirt domain state integer to DomainState.
func mapDomainState(state uint8) DomainState {
	switch golibvirt.DomainState(state) {
	case golibvirt.DomainRunning:
		return StateRunning
	case golibvirt.DomainShutoff:
		return StateShutoff
	case golibvirt.DomainPaused:
		return StatePaused
	case golibvirt.DomainCrashed:
		return StateCrashed
	default:
		return StateUnknown
	}
}

// uuidToString formats a [32]byte UUID into standard UUID string format.
func uuidToString(uuid golibvirt.UUID) string {
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		uuid[0:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:16])
}

func optStringValue(values golibvirt.OptString) string {
	if len(values) == 0 {
		return ""
	}
	return strings.TrimSpace(values[0])
}

func appendUnique(items *[]string, value string) {
	for _, current := range *items {
		if current == value {
			return
		}
	}
	*items = append(*items, value)
}
