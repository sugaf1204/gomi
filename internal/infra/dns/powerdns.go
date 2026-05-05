package dns

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/sugaf1204/gomi/internal/machine"
)

type PowerDNSClient struct {
	baseURL    string
	apiToken   string
	serverID   string
	httpClient *http.Client
}

func NewPowerDNSClient(baseURL, apiToken, serverID string) *PowerDNSClient {
	return &PowerDNSClient{
		baseURL:  strings.TrimRight(baseURL, "/"),
		apiToken: apiToken,
		serverID: serverID,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (p *PowerDNSClient) Enabled() bool {
	return p.baseURL != "" && p.apiToken != "" && p.serverID != ""
}

func ensureFQDN(name string) string {
	if strings.HasSuffix(name, ".") {
		return name
	}
	return name + "."
}

func (p *PowerDNSClient) UpsertMachineRecord(ctx context.Context, m machine.Machine) error {
	if !p.Enabled() {
		return nil
	}
	if strings.TrimSpace(m.IP) == "" || strings.TrimSpace(m.Network.Domain) == "" {
		return nil
	}
	zone := ensureFQDN(m.Network.Domain)
	host := ensureFQDN(m.Hostname + "." + m.Network.Domain)

	payload := map[string]any{
		"rrsets": []map[string]any{
			{
				"name":       host,
				"type":       "A",
				"ttl":        300,
				"changetype": "REPLACE",
				"records": []map[string]any{
					{"content": m.IP, "disabled": false},
				},
			},
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	url := fmt.Sprintf("%s/api/v1/servers/%s/zones/%s", p.baseURL, p.serverID, zone)
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, url, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("X-API-Key", p.apiToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("powerdns returned %d", resp.StatusCode)
	}
	return nil
}
