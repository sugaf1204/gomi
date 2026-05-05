package discovery

import "strings"

func generateName(mac, clientHostname string) string {
	if h := strings.TrimSpace(clientHostname); h != "" {
		return strings.ToLower(h)
	}
	return strings.ReplaceAll(strings.ToLower(mac), ":", "-")
}
