package vm

import (
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/sugaf1204/gomi/internal/hypervisor"
	"github.com/sugaf1204/gomi/internal/infra/netdetect"
)

type primaryIPDetector func() (string, error)

func (d *Deployer) resolvePXEBaseURL(hv hypervisor.Hypervisor, installType InstallConfigType) (string, error) {
	if base := strings.TrimSpace(d.PXEBaseURL); base != "" {
		return strings.TrimRight(base, "/"), nil
	}
	base, err := resolvePXEBaseURLFromListen(d.ListenAddr, detectPrimaryIP)
	if err != nil && installType == InstallConfigCurtin {
		return "", err
	}
	return base, nil
}

func resolvePXEBaseURLFromListen(listenAddr string, detect primaryIPDetector) (string, error) {
	host, port, err := splitListenAddr(listenAddr)
	if err != nil {
		return "", err
	}
	if port == "" {
		port = "5392"
	}
	if host == "" || isUnspecifiedHost(host) {
		primaryIP, err := detect()
		if err != nil {
			return "", fmt.Errorf("detect primary IP for pxe.http_base_url: %w", err)
		}
		host = primaryIP
	}
	if isLoopbackHost(host) {
		return "", fmt.Errorf("pxe.http_base_url is required when listen_addr %q is loopback-only", listenAddr)
	}
	return "http://" + net.JoinHostPort(host, port) + "/pxe", nil
}

func detectPrimaryIP() (string, error) {
	detected, err := netdetect.Detect()
	if err != nil {
		return "", err
	}
	if detected == nil || strings.TrimSpace(detected.IPAddress) == "" {
		return "", errors.New("primary IPv4 address not found")
	}
	return strings.TrimSpace(detected.IPAddress), nil
}

func splitListenAddr(addr string) (string, string, error) {
	trimmed := strings.TrimSpace(addr)
	if trimmed == "" {
		return "", "5392", nil
	}
	if strings.HasPrefix(trimmed, ":") {
		return "", strings.TrimPrefix(trimmed, ":"), nil
	}
	host, port, err := net.SplitHostPort(trimmed)
	if err == nil {
		return strings.Trim(host, "[]"), port, nil
	}
	if strings.Contains(trimmed, ":") {
		return "", "", fmt.Errorf("invalid listen_addr %q: %w", listenAddrForError(trimmed), err)
	}
	return strings.Trim(trimmed, "[]"), "5392", nil
}

func isUnspecifiedHost(host string) bool {
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsUnspecified()
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(strings.Trim(host, "[]"), "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

func listenAddrForError(addr string) string {
	if addr == "" {
		return "<empty>"
	}
	return addr
}
