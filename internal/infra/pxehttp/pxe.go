package pxehttp

import (
	"context"
	"fmt"
	"github.com/labstack/echo/v4"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/node"
	"github.com/sugaf1204/gomi/internal/vm"
	"net"
	gohttp "net/http"
	"strings"
)

type pxeTarget struct {
	node            node.Node
	installType     vm.InstallConfigType
	variant         string
	osFamily        string
	completedRootFS bool
	diskImageDeploy bool
}

func (h *Handler) PXEBootScript(c echo.Context) error {
	base := h.resolvePXEBaseURL(c)
	rawMAC := c.QueryParam("mac")
	ctx := c.Request().Context()
	target, provisioning, err := h.resolvePXETarget(ctx, rawMAC)
	if err != nil {
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}

	if !provisioning {
		script := renderPXELocalBootScript(base)
		h.recordPXEBootScriptMarker(ctx, target, false)
		return c.Blob(gohttp.StatusOK, "text/plain; charset=utf-8", []byte(script))
	}

	mac := normalizeMAC(rawMAC)
	token := pxeTargetToken(target)
	completeURL := buildPXEInstallCompleteURL(base, token, target.installType)
	script := renderPXEInstallScriptWithVariant(base, target.installType, mac, completeURL, target.variant, target.node)
	h.recordPXEBootScriptMarker(ctx, target, true)
	return c.Blob(gohttp.StatusOK, "text/plain; charset=utf-8", []byte(script))
}

func (h *Handler) PXEPreseed(c echo.Context) error {
	rawMAC := c.QueryParam("mac")
	target, _, err := h.resolvePXETarget(c.Request().Context(), rawMAC)
	if err != nil {
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}

	base := h.resolvePXEBaseURL(c)
	token := pxeTargetToken(target)
	completeURL := buildPXEInstallCompleteURL(base, token, vm.InstallConfigPreseed)

	var body string
	if inline, found, err := h.resolvePXEInstallInline(c.Request().Context(), rawMAC, vm.InstallConfigPreseed); err != nil {
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	} else if found {
		body = inline
	} else {
		body = defaultDebianPreseed
	}
	hostname := pxeTargetHostname(target)
	body = injectPreseedHostname(body, hostname)
	return c.Blob(gohttp.StatusOK, "text/plain; charset=utf-8", []byte(injectPreseedCompletion(body, completeURL, hostname)))
}

func (h *Handler) PXENocloudUserData(c echo.Context) error {
	body, err := h.renderNoCloudUserData(c.Request().Context(), c.Param("mac"), h.resolvePXEBaseURL(c))
	if err != nil {
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	return c.Blob(gohttp.StatusOK, "text/plain; charset=utf-8", []byte(body))
}

func (h *Handler) renderNoCloudUserData(ctx context.Context, rawMAC, base string) (string, error) {
	target, _, err := h.resolvePXETarget(ctx, rawMAC)
	if err != nil {
		return "", err
	}

	token := pxeTargetToken(target)
	sourceType := normalizePXEUserDataInstallType(target.installType)
	completeURL := buildPXEInstallCompleteURL(base, token, sourceType)
	hostname := pxeTargetHostname(target)

	var body string
	if inline, found, err := h.resolvePXEInstallInline(ctx, rawMAC, sourceType); err != nil {
		return "", err
	} else if found {
		body = inline
	} else {
		body = defaultPXEUserDataByInstallType(sourceType)
	}

	completeRetries := 60
	if isDebianOSFamily(target.osFamily) {
		completeRetries = 600
	}
	result, err := injectCloudConfigCompletion(body, completeURL, hostname, completeRetries)
	if err != nil {
		return "", err
	}

	// Inject registered SSH keys and any per-target login user. Without a
	// login user, keys go to the distribution default user; with one, only
	// that user receives the keys.
	result = h.injectSSHKeysAndLoginUser(ctx, result, target.node)

	if m, ok := target.node.(*machine.Machine); ok {
		result = injectWoLShutdownAgent(result, base, m)
	}

	if m, ok := target.node.(*machine.Machine); ok && m.Role == machine.RoleHypervisor {
		registrationToken, err := h.ensureHypervisorRegistrationToken(ctx, m)
		if err != nil {
			return "", err
		}
		result = injectHypervisorSetup(result, base, m.Name, registrationToken, target.osFamily)
	}

	if target.node != nil && strings.TrimSpace(target.node.PrimaryMAC()) != "" && !target.diskImageDeploy {
		if m, ok := target.node.(*machine.Machine); ok && m.Role == machine.RoleHypervisor {
			result = injectBridgedHostNetworkConfig(result, m, target.osFamily, base, h.resolveSubnetSpec(ctx, target.node))
		} else {
			result = injectHostNetworkConfig(result, target.node, target.osFamily, base, h.resolveSubnetSpec(ctx, target.node))
		}
	}

	result = withDeployCloudInitDefaults(result, target.completedRootFS)
	if m, ok := target.node.(*machine.Machine); ok && m.SealedBootstrap != nil && !m.SealedBootstrap.Cleanup {
		return markSealedCompletion(result)
	}
	return result, nil
}

func (h *Handler) PXENocloudMetaData(c echo.Context) error {
	rawMAC := c.Param("mac")
	ctx := c.Request().Context()
	hostname := "gomi-pxe"

	var target node.Node
	if n := h.findHostByMAC(ctx, rawMAC); n != nil {
		target = n
		if name := sanitizeHostnameForLinux(n.NodeDisplayName()); name != "" {
			hostname = name
		}
	}

	body := fmt.Sprintf("instance-id: gomi-%s\nlocal-hostname: %s\n", macToken(rawMAC), hostname)
	if m, ok := target.(*machine.Machine); ok && isCAPIProvisioning(m) {
		if ip := net.ParseIP(strings.TrimSpace(m.IP)); ip != nil && ip.To4() != nil {
			body += fmt.Sprintf("gomi_ipv4: %s\ngomi_uplink: %s\n", ip.String(), capiUplinkName)
		}
	}
	return c.Blob(gohttp.StatusOK, "text/plain; charset=utf-8", []byte(body))
}

func (h *Handler) PXENocloudVendorData(c echo.Context) error {
	return c.Blob(gohttp.StatusOK, "text/plain; charset=utf-8", []byte(defaultNoCloudVendorData))
}
