package gomi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	infrav1 "github.com/sugaf1204/gomi/providers/cluster-api/api/v1alpha1"
)

type Client struct {
	base, token string
	http        *http.Client
}

func New(server, token string) (*Client, error) {
	u, err := url.Parse(server)
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, fmt.Errorf("server must be an HTTP(S) origin URL")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, fmt.Errorf("credentials Secret token is empty")
	}
	return &Client{base: strings.TrimRight(server, "/"), token: token, http: &http.Client{Timeout: 2 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

type APIError struct{ Status int }

func (e *APIError) Error() string         { return fmt.Sprintf("GOMI API returned HTTP %d", e.Status) }
func IsStatus(err error, status int) bool { e, ok := err.(*APIError); return ok && e.Status == status }
func (c *Client) request(ctx context.Context, method, path string, input, output any) error {
	var body io.Reader
	if input != nil {
		b, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+"/api/v1"+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("GOMI request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &APIError{resp.StatusCode}
	}
	if output != nil {
		return json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(output)
	}
	return nil
}

type Template struct {
	DeliveryMode string `json:"deliveryMode"`
	Name         string `json:"name"`
	UserData     string `json:"userData"`
	Description  string `json:"description"`
}
type VM struct {
	CloudInitRefs []string `json:"cloudInitRefs"`
	Name          string   `json:"name"`
	CloudInitRef  string   `json:"cloudInitRef"`
	Phase         string   `json:"phase"`
	IPAddresses   []string `json:"ipAddresses"`
	Provisioning  struct {
		Active      bool       `json:"active"`
		CompletedAt *time.Time `json:"completedAt"`
	} `json:"provisioning"`
}

func (c *Client) GetVM(ctx context.Context, id string) (*VM, error) {
	var v VM
	err := c.request(ctx, "GET", "/virtual-machines/"+url.PathEscape(id), nil, &v)
	return &v, err
}
func (c *Client) CreateVM(ctx context.Context, id string, spec infrav1.VirtualMachineSpec) error {
	b, err := json.Marshal(spec)
	if err != nil {
		return err
	}
	var s map[string]any
	if err = json.Unmarshal(b, &s); err != nil {
		return err
	}
	s["cloudInitRef"] = id
	s["powerControlMethod"] = "libvirt"
	return c.request(ctx, "POST", "/virtual-machines", map[string]any{"virtualMachineId": id, "virtualMachine": s}, nil)
}
func (c *Client) GetTemplate(ctx context.Context, id string) (*Template, error) {
	var t Template
	err := c.request(ctx, "GET", "/cloud-init-templates/"+url.PathEscape(id), nil, &t)
	return &t, err
}
func (c *Client) CreateTemplate(ctx context.Context, t Template) error {
	return c.request(ctx, "POST", "/cloud-init-templates", t, nil)
}
func (c *Client) DeleteVM(ctx context.Context, id string) error {
	return c.remove(ctx, "/virtual-machines/"+url.PathEscape(id))
}
func (c *Client) DeleteTemplate(ctx context.Context, id string) error {
	return c.remove(ctx, "/cloud-init-templates/"+url.PathEscape(id))
}
func (c *Client) remove(ctx context.Context, path string) error {
	err := c.request(ctx, "DELETE", path, nil, nil)
	if IsStatus(err, 404) {
		return nil
	}
	return err
}

// OwnedBy accepts GOMI's canonical refs array and the legacy singleton response.
func (v *VM) OwnedBy(id string) bool {
	if len(v.CloudInitRefs) > 0 {
		return len(v.CloudInitRefs) == 1 && strings.TrimPrefix(v.CloudInitRefs[0], "cloudInitTemplates/") == id
	}
	return strings.TrimPrefix(v.CloudInitRef, "cloudInitTemplates/") == id
}

// Probe before transmitting bootstrap secrets: older servers ignore unknown
// template fields and would otherwise expose them through public PXE routes.
func (c *Client) RequireVMSeedTemplates(ctx context.Context) error {
	var caps struct {
		VMSeedTemplates bool `json:"vmSeedTemplates"`
	}
	if err := c.request(ctx, "GET", "/capabilities", nil, &caps); err != nil {
		return err
	}
	if !caps.VMSeedTemplates {
		return fmt.Errorf("GOMI server does not support protected VM seed templates")
	}
	return nil
}
