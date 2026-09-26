package pxehttp

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/sugaf1204/gomi/internal/cloudinit"
	"github.com/sugaf1204/gomi/internal/resource"
	"github.com/sugaf1204/gomi/internal/vm"
)

// Only this package can grant access to protected templates. HTTP headers and
// provisioning completion tokens cannot enable this in-process capability.
type vmSeedContextKey struct{}

func (h *Handler) RenderVMSeed(ctx context.Context, v vm.VirtualMachine, base string) (map[string]string, error) {
	mac := v.PrimaryMAC()
	n := h.findHostByMAC(ctx, mac)
	stored, ok := n.(*vm.VirtualMachine)
	if !ok || stored.Name != v.Name {
		return nil, fmt.Errorf("VM seed target not found")
	}
	protected, err := h.isProtectedSeed(ctx, mac)
	if err != nil {
		return nil, err
	}
	if protected && !stored.IsProvisioningActive() {
		return nil, fmt.Errorf("VM seed provisioning is not active")
	}
	ctx = context.WithValue(ctx, vmSeedContextKey{}, true)
	body, err := h.renderNoCloudUserData(ctx, mac, base)
	if err != nil {
		return nil, err
	}
	hostname := sanitizeHostnameForLinux(stored.NodeDisplayName())
	if hostname == "" {
		hostname = "gomi-pxe"
	}
	return map[string]string{
		"user-data":      body,
		"meta-data":      fmt.Sprintf("instance-id: gomi-%s\nlocal-hostname: %s\n", macToken(mac), hostname),
		"vendor-data":    defaultNoCloudVendorData,
		"network-config": h.renderNoCloudNetworkConfig(ctx, mac),
	}, nil
}

func (h *Handler) isProtectedSeed(ctx context.Context, mac string) (bool, error) {
	n := h.findHostByMAC(ctx, mac)
	if n == nil || h.cloudInits == nil || n.CloudInitRefForDeploy() == "" {
		return false, nil
	}
	t, err := h.cloudInits.Get(ctx, n.CloudInitRefForDeploy())
	if errors.Is(err, resource.ErrNotFound) {
		return false, nil
	}
	return t.DeliveryMode == cloudinit.DeliveryVMSeed, err
}

// Guard every public boot configuration response, including those carrying a
// completion token, for a seed-only target. Ordinary PXE behavior is unchanged.
func (h *Handler) PublicBootData(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		mac := c.Param("mac")
		if mac == "" {
			mac = c.QueryParam("mac")
		}
		protected, err := h.isProtectedSeed(c.Request().Context(), mac)
		if err != nil {
			return c.NoContent(http.StatusServiceUnavailable)
		}
		if protected {
			return c.NoContent(http.StatusForbidden)
		}
		return next(c)
	}
}
