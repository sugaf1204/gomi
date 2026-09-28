package gomi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
)

type BareMetalHost struct {
	Name      string `json:"name"`
	Pool      string `json:"pool"`
	Owner     string `json:"owner"`
	State     string `json:"state"`
	PublicKey string `json:"publicKey"`
	IPAddress string `json:"ipAddress"`
}

func (c *Client) RequireBareMetal(ctx context.Context) error {
	var caps struct {
		Supported bool `json:"bareMetalSealedBootstrap"`
	}
	if err := c.request(ctx, "GET", "/capabilities", nil, &caps); err != nil {
		return err
	}
	if !caps.Supported {
		return fmt.Errorf("GOMI lacks sealed bare-metal provisioning")
	}
	return nil
}
func (c *Client) RequireBareMetalProvisioningTemplates(ctx context.Context) error {
	var caps struct {
		Supported bool `json:"bareMetalProvisioningTemplates"`
	}
	if err := c.request(ctx, "GET", "/capabilities", nil, &caps); err != nil {
		return err
	}
	if !caps.Supported {
		return fmt.Errorf("GOMI lacks bare-metal provisioning templates")
	}
	return nil
}
func (c *Client) GetBareMetal(ctx context.Context, owner string) (*BareMetalHost, error) {
	var h BareMetalHost
	err := c.request(ctx, "GET", "/bare-metal-claims/"+url.PathEscape(owner), nil, &h)
	return &h, err
}
func (c *Client) AcquireBareMetal(ctx context.Context, owner, pool string) error {
	return c.request(ctx, "POST", "/bare-metal-claims", map[string]string{"owner": owner, "pool": pool}, nil)
}
func (c *Client) DeployBareMetal(ctx context.Context, owner, image, cloudInitRef string, envelope []byte) error {
	return c.request(ctx, "POST", "/bare-metal-claims/"+url.PathEscape(owner)+"/deploy", struct {
		OSImageRef   string          `json:"osImageRef"`
		CloudInitRef string          `json:"cloudInitRef,omitempty"`
		Envelope     json.RawMessage `json:"envelope"`
	}{image, cloudInitRef, envelope}, nil)
}
func (c *Client) ReleaseBareMetal(ctx context.Context, owner string) error {
	return c.remove(ctx, "/bare-metal-claims/"+url.PathEscape(owner))
}
