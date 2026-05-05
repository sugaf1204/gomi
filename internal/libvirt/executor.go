package libvirt

import (
	"context"
	"encoding/xml"
	"fmt"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	golibvirt "github.com/digitalocean/go-libvirt"
)

const (
	defaultLibvirtPort = 16509
	dialTimeout        = 10 * time.Second
)

// Executor manages libvirt domains on a remote hypervisor.
type Executor interface {
	// DefineDomain creates a new VM domain from config.
	DefineDomain(ctx context.Context, cfg DomainConfig) error
	// CreateVolume creates a storage volume in the default pool.
	CreateVolume(ctx context.Context, name string, sizeGB int, format string) error
	// CreateOverlayVolume creates a qcow2 volume backed by a base image (copy-on-write).
	CreateOverlayVolume(ctx context.Context, name string, sizeGB int, backingPath string, backingFormat string) error
	// DeleteVolume removes a storage volume from the default pool, if present.
	DeleteVolume(ctx context.Context, name string) error
	// StartDomain powers on a VM.
	StartDomain(ctx context.Context, name string) error
	// ShutdownDomain gracefully shuts down a VM.
	ShutdownDomain(ctx context.Context, name string) error
	// DestroyDomain forcefully stops a VM.
	DestroyDomain(ctx context.Context, name string) error
	// UndefineDomain removes a VM definition.
	UndefineDomain(ctx context.Context, name string) error
	// SetDomainBootDevice updates persistent boot device (e.g. "hd" or "network").
	SetDomainBootDevice(ctx context.Context, name string, bootDev string) error
	// DomainInfo gets current state of a domain.
	DomainInfo(ctx context.Context, name string) (*DomainInfo, error)
	// DomainInterfaces gets runtime network interface information of a domain.
	DomainInterfaces(ctx context.Context, name string) ([]InterfaceInfo, error)
	// DomainGraphicsInfo returns VNC/SPICE graphics connection info for a running domain.
	DomainGraphicsInfo(ctx context.Context, name string) (*GraphicsInfo, error)
	// MigrateDomain performs a live migration of a domain to a destination URI.
	MigrateDomain(ctx context.Context, name string, destURI string, flags golibvirt.DomainMigrateFlags) error
	// ListDomains lists all domains.
	ListDomains(ctx context.Context) ([]DomainInfo, error)
	// Close releases any underlying connections.
	Close() error
}

// rpcExecutor implements Executor using the libvirt TCP RPC protocol.
type rpcExecutor struct {
	l *golibvirt.Libvirt
}

// NewExecutor creates a new libvirt executor that connects to the hypervisor via TCP RPC.
// The caller should call Close() when done with the executor.
func NewExecutor(cfg LibvirtConfig) (Executor, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid libvirt config: %w", err)
	}

	port := cfg.Port
	if port == 0 {
		port = defaultLibvirtPort
	}

	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp", addr, dialTimeout)
	if err != nil {
		return nil, fmt.Errorf("dial libvirtd %s: %w", addr, err)
	}

	l := golibvirt.New(conn)
	if err := l.Connect(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("libvirt connect: %w", err)
	}

	return &rpcExecutor{l: l}, nil
}

func (e *rpcExecutor) CreateVolume(_ context.Context, name string, sizeGB int, format string) error {
	pool, err := e.l.StoragePoolLookupByName("default")
	if err != nil {
		return fmt.Errorf("lookup storage pool 'default': %w", err)
	}

	ext := ".qcow2"
	if format == "raw" {
		ext = ".img"
	}
	sizeBytes := int64(sizeGB) * 1024 * 1024 * 1024

	volXML := fmt.Sprintf(`<volume type='file'>
  <name>%s%s</name>
  <capacity unit='bytes'>%d</capacity>
  <target>
    <format type='%s'/>
  </target>
</volume>`, name, ext, sizeBytes, format)

	_, err = e.l.StorageVolCreateXML(pool, volXML, 0)
	if err != nil {
		return fmt.Errorf("create volume %s: %w", name, err)
	}
	return nil
}

func (e *rpcExecutor) CreateOverlayVolume(_ context.Context, name string, sizeGB int, backingPath string, backingFormat string) error {
	pool, err := e.l.StoragePoolLookupByName("default")
	if err != nil {
		return fmt.Errorf("lookup storage pool 'default': %w", err)
	}

	if backingFormat == "" {
		backingFormat = "qcow2"
	}
	sizeBytes := int64(sizeGB) * 1024 * 1024 * 1024

	volXML := fmt.Sprintf(`<volume type='file'>
  <name>%s.qcow2</name>
  <capacity unit='bytes'>%d</capacity>
  <target>
    <format type='qcow2'/>
  </target>
  <backingStore>
    <path>%s</path>
    <format type='%s'/>
  </backingStore>
</volume>`, name, sizeBytes, backingPath, backingFormat)

	_, err = e.l.StorageVolCreateXML(pool, volXML, 0)
	if err != nil {
		return fmt.Errorf("create overlay volume %s: %w", name, err)
	}
	return nil
}

func (e *rpcExecutor) DeleteVolume(_ context.Context, name string) error {
	pool, err := e.l.StoragePoolLookupByName("default")
	if err != nil {
		return fmt.Errorf("lookup storage pool 'default': %w", err)
	}

	candidates := []string{name + ".qcow2", name + ".img"}
	for _, volName := range candidates {
		vol, err := e.l.StorageVolLookupByName(pool, volName)
		if err != nil {
			continue
		}
		if err := e.l.StorageVolDelete(vol, 0); err != nil {
			return fmt.Errorf("delete volume %s: %w", volName, err)
		}
	}
	return nil
}

func (e *rpcExecutor) DefineDomain(_ context.Context, cfg DomainConfig) error {
	xmlStr, err := GenerateDomainXML(cfg)
	if err != nil {
		return fmt.Errorf("generate domain xml: %w", err)
	}

	_, err = e.l.DomainDefineXMLFlags(xmlStr, 0)
	if err != nil {
		return fmt.Errorf("define domain: %w", err)
	}
	return nil
}

func (e *rpcExecutor) StartDomain(_ context.Context, name string) error {
	domain, err := e.l.DomainLookupByName(name)
	if err != nil {
		return fmt.Errorf("lookup domain %s: %w", name, err)
	}
	if err := e.l.DomainCreate(domain); err != nil {
		return fmt.Errorf("start domain %s: %w", name, err)
	}
	return nil
}

func (e *rpcExecutor) ShutdownDomain(_ context.Context, name string) error {
	domain, err := e.l.DomainLookupByName(name)
	if err != nil {
		return fmt.Errorf("lookup domain %s: %w", name, err)
	}
	if err := e.l.DomainShutdown(domain); err != nil {
		return fmt.Errorf("shutdown domain %s: %w", name, err)
	}
	return nil
}

func (e *rpcExecutor) DestroyDomain(_ context.Context, name string) error {
	domain, err := e.l.DomainLookupByName(name)
	if err != nil {
		return fmt.Errorf("lookup domain %s: %w", name, err)
	}
	if err := e.l.DomainDestroy(domain); err != nil {
		return fmt.Errorf("destroy domain %s: %w", name, err)
	}
	return nil
}

func (e *rpcExecutor) UndefineDomain(_ context.Context, name string) error {
	domain, err := e.l.DomainLookupByName(name)
	if err != nil {
		return fmt.Errorf("lookup domain %s: %w", name, err)
	}
	if err := e.l.DomainUndefineFlags(domain, golibvirt.DomainUndefineManagedSave|golibvirt.DomainUndefineSnapshotsMetadata); err != nil {
		return fmt.Errorf("undefine domain %s: %w", name, err)
	}
	return nil
}

func (e *rpcExecutor) SetDomainBootDevice(_ context.Context, name string, bootDev string) error {
	domain, err := e.l.DomainLookupByName(name)
	if err != nil {
		return fmt.Errorf("lookup domain %s: %w", name, err)
	}
	xmlDesc, err := e.l.DomainGetXMLDesc(domain, 0)
	if err != nil {
		return fmt.Errorf("get domain xml %s: %w", name, err)
	}
	updatedXML, err := rewriteDomainBootDeviceXML(xmlDesc, bootDev)
	if err != nil {
		return fmt.Errorf("rewrite boot device %s: %w", name, err)
	}
	if _, err := e.l.DomainDefineXMLFlags(updatedXML, 0); err != nil {
		return fmt.Errorf("define domain with boot device %s: %w", name, err)
	}
	return nil
}

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

func (e *rpcExecutor) MigrateDomain(_ context.Context, name string, destURI string, flags golibvirt.DomainMigrateFlags) error {
	domain, err := e.l.DomainLookupByName(name)
	if err != nil {
		return fmt.Errorf("lookup domain %s: %w", name, err)
	}
	if _, err := e.l.DomainMigratePerform3Params(
		domain,
		golibvirt.OptString{destURI},
		nil, // params
		nil, // cookie
		flags,
	); err != nil {
		return fmt.Errorf("migrate domain %s to %s: %w", name, destURI, err)
	}
	return nil
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

func (e *rpcExecutor) Close() error {
	return e.l.Disconnect()
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

type domainXMLDesc struct {
	Devices struct {
		Interfaces []struct {
			Target struct {
				Dev string `xml:"dev,attr"`
			} `xml:"target"`
			MAC struct {
				Address string `xml:"address,attr"`
			} `xml:"mac"`
		} `xml:"interface"`
	} `xml:"devices"`
}

func parseDomainInterfacesFromXML(raw string) []InterfaceInfo {
	var desc domainXMLDesc
	if err := xml.Unmarshal([]byte(raw), &desc); err != nil {
		return nil
	}
	out := make([]InterfaceInfo, 0, len(desc.Devices.Interfaces))
	for _, iface := range desc.Devices.Interfaces {
		out = append(out, InterfaceInfo{
			Name: strings.TrimSpace(iface.Target.Dev),
			MAC:  strings.ToLower(strings.TrimSpace(iface.MAC.Address)),
		})
	}
	return out
}

var (
	bootTagPattern = regexp.MustCompile(`<boot\s+dev=['"][^'"]+['"]\s*(?:/>|></boot>)`)
	osTagPattern   = regexp.MustCompile(`(?s)<os>.*?</os>`)
)

func rewriteDomainBootDeviceXML(rawXML string, bootDev string) (string, error) {
	switch bootDev {
	case "hd", "network":
	default:
		return "", fmt.Errorf("unsupported boot device: %s", bootDev)
	}

	osLoc := osTagPattern.FindStringIndex(rawXML)
	if len(osLoc) != 2 {
		return "", fmt.Errorf("os tag not found")
	}
	osSection := rawXML[osLoc[0]:osLoc[1]]
	newBootTag := fmt.Sprintf("<boot dev='%s'/>", bootDev)

	if bootTagPattern.MatchString(osSection) {
		updatedOS := bootTagPattern.ReplaceAllString(osSection, newBootTag)
		return rawXML[:osLoc[0]] + updatedOS + rawXML[osLoc[1]:], nil
	}

	closing := strings.LastIndex(osSection, "</os>")
	if closing < 0 {
		return "", fmt.Errorf("os closing tag not found")
	}
	updatedOS := osSection[:closing] + "    " + newBootTag + "\n  " + osSection[closing:]
	return rawXML[:osLoc[0]] + updatedOS + rawXML[osLoc[1]:], nil
}
