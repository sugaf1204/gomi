package netdetect

import (
	"bufio"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

type DetectedNetwork struct {
	InterfaceName string
	IPAddress     string
	CIDR          string
	Gateway       string
	DNSServers    []string
	SearchDomains []string
}

func Detect() (*DetectedNetwork, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	var result DetectedNetwork
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil || len(addrs) == 0 {
			continue
		}
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			if ipNet.IP.To4() == nil {
				continue
			}
			result.InterfaceName = iface.Name
			result.IPAddress = ipNet.IP.To4().String()
			ones, bits := ipNet.Mask.Size()
			result.CIDR = ipNet.IP.Mask(ipNet.Mask).String() + "/" + itoa(ones, bits)
			break
		}
		if result.CIDR != "" {
			break
		}
	}

	result.Gateway = detectGateway()
	dns, search := parseResolvConf()
	result.DNSServers = dns
	result.SearchDomains = search

	return &result, nil
}

func itoa(ones, _ int) string {
	s := ""
	n := ones
	if n == 0 {
		return "0"
	}
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}

func detectGateway() string {
	switch runtime.GOOS {
	case "linux":
		out, err := exec.Command("ip", "route", "show", "default").Output()
		if err != nil {
			return ""
		}
		fields := strings.Fields(string(out))
		for i, f := range fields {
			if f == "via" && i+1 < len(fields) {
				return fields[i+1]
			}
		}
	case "darwin":
		out, err := exec.Command("route", "-n", "get", "default").Output()
		if err != nil {
			return ""
		}
		for _, line := range strings.Split(string(out), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "gateway:") {
				return strings.TrimSpace(strings.TrimPrefix(line, "gateway:"))
			}
		}
	}
	return ""
}

func parseResolvConf() (dnsServers []string, searchDomains []string) {
	dnsServers, searchDomains = parseResolvConfFile("/etc/resolv.conf")

	// If all nameservers are systemd-resolved stubs (127.0.0.53, 127.0.0.1),
	// resolve the actual upstream DNS from systemd-resolved's own resolv.conf.
	if len(dnsServers) > 0 && allLocalStubs(dnsServers) {
		upstream, upSearch := resolveSystemdUpstream()
		if len(upstream) > 0 {
			dnsServers = upstream
		}
		if len(upSearch) > 0 {
			searchDomains = upSearch
		}
	}

	return dnsServers, searchDomains
}

func parseResolvConfFile(path string) (dnsServers []string, searchDomains []string) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "nameserver":
			dnsServers = append(dnsServers, fields[1])
		case "search", "domain":
			searchDomains = append(searchDomains, fields[1:]...)
		}
	}
	return dnsServers, searchDomains
}

// allLocalStubs returns true if every DNS server is a loopback stub
// (systemd-resolved uses 127.0.0.53; some configs use 127.0.0.1).
func allLocalStubs(servers []string) bool {
	for _, s := range servers {
		if s != "127.0.0.53" && s != "127.0.0.1" {
			return false
		}
	}
	return true
}

// resolveSystemdUpstream reads actual upstream DNS from systemd-resolved's
// resolv.conf, then falls back to parsing resolvectl output.
func resolveSystemdUpstream() (dnsServers []string, searchDomains []string) {
	// Method 1: /run/systemd/resolve/resolv.conf contains the real upstream servers
	dnsServers, searchDomains = parseResolvConfFile("/run/systemd/resolve/resolv.conf")
	if len(dnsServers) > 0 && !allLocalStubs(dnsServers) {
		return dnsServers, searchDomains
	}

	// Method 2: parse resolvectl status output
	out, err := exec.Command("resolvectl", "status").Output()
	if err != nil {
		return nil, nil
	}
	dnsServers = nil
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "DNS Servers:") || strings.HasPrefix(line, "Current DNS Server:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				for _, s := range strings.Fields(parts[1]) {
					ip := net.ParseIP(s)
					if ip != nil && !ip.IsLoopback() {
						dnsServers = append(dnsServers, s)
					}
				}
			}
		}
	}
	return dnsServers, searchDomains
}
