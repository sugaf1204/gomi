package app

import (
	"fmt"
	"net"
	"strings"
)

func detectDefaultIface() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, i := range ifaces {
		if i.Flags&net.FlagLoopback != 0 || i.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, _ := i.Addrs()
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil {
				return i.Name
			}
		}
	}
	return ""
}

// detectIfaceIP returns the first IPv4 address on the given interface.
func detectIfaceIP(name string) net.IP {
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return nil
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return nil
	}
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil {
			return ipn.IP.To4()
		}
	}
	return nil
}

func (r *Runtime) resolvePXEHTTPBaseURL(serverIP net.IP) string {
	if strings.TrimSpace(r.Config.PXEHTTPBaseURL) != "" {
		return strings.TrimRight(r.Config.PXEHTTPBaseURL, "/")
	}
	port := listenPort(r.Config.ListenAddr)
	return fmt.Sprintf("http://%s:%s/pxe", serverIP.String(), port)
}

func (r *Runtime) currentBootHTTPBaseURL() string {
	if strings.TrimSpace(r.Config.BootHTTPBaseURL) != "" {
		return strings.TrimRight(r.Config.BootHTTPBaseURL, "/")
	}
	_, state := r.currentPXEState()
	if ip := net.ParseIP(strings.TrimSpace(state.serverIP)); ip != nil {
		return r.resolveBootHTTPBaseURL(ip)
	}
	if ip := listenAddrIP(r.Config.ListenAddr); ip != nil {
		return r.resolveBootHTTPBaseURL(ip)
	}
	if ip := r.detectBootHTTPServerIP(); ip != nil {
		return r.resolveBootHTTPBaseURL(ip)
	}
	return r.resolveBootHTTPBaseURL(net.IPv4(127, 0, 0, 1))
}

func (r *Runtime) resolveBootHTTPBaseURL(serverIP net.IP) string {
	if strings.TrimSpace(r.Config.BootHTTPBaseURL) != "" {
		return strings.TrimRight(r.Config.BootHTTPBaseURL, "/")
	}
	port := listenPort(r.Config.ListenAddr)
	return fmt.Sprintf("http://%s:%s", serverIP.String(), port)
}

func (r *Runtime) detectBootHTTPServerIP() net.IP {
	if iface := strings.TrimSpace(r.Config.DHCPIface); iface != "" {
		if ip := detectIfaceIP(iface); ip != nil {
			return ip
		}
	}
	if iface := detectDefaultIface(); iface != "" {
		if ip := detectIfaceIP(iface); ip != nil {
			return ip
		}
	}
	return nil
}

func listenAddrIP(addr string) net.IP {
	host, _, err := net.SplitHostPort(addr)
	if err != nil || strings.TrimSpace(host) == "" {
		return nil
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip == nil || ip.IsUnspecified() {
		return nil
	}
	return ip
}

func listenPort(addr string) string {
	if strings.HasPrefix(addr, ":") {
		p := strings.TrimPrefix(addr, ":")
		if p != "" {
			return p
		}
	}
	if host, port, err := net.SplitHostPort(addr); err == nil {
		if host != "" || port != "" {
			return port
		}
	}
	return "5392"
}
