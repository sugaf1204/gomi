package pxehttp

import (
	"context"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/node"
	"github.com/sugaf1204/gomi/internal/osimage"
	"github.com/sugaf1204/gomi/internal/resource"
	"github.com/sugaf1204/gomi/internal/vm"
	"net/url"
	"strings"
)

func (h *Handler) resolvePXETarget(ctx context.Context, rawMAC string) (pxeTarget, bool, error) {
	normalized := normalizeMAC(rawMAC)
	token := macToken(rawMAC)
	if normalized == "" && token == "" {
		return pxeTarget{installType: vm.InstallConfigPreseed}, true, nil
	}

	targetVM, _, vmErr := h.findVirtualMachineByMAC(ctx, rawMAC)
	if vmErr != nil {
		return pxeTarget{installType: vm.InstallConfigPreseed}, false, vmErr
	}
	if targetVM != nil && targetVM.IsProvisioningActive() {
		return pxeTarget{
			node:        targetVM,
			installType: vm.InstallConfigType(targetVM.PXEInstallType()),
			variant:     h.resolveOSImageVariant(ctx, targetVM.OSImageVariantRef()),
			osFamily:    h.resolveOSImageFamily(ctx, targetVM.OSImageVariantRef()),
		}, true, nil
	}

	targetMachine, _, machineErr := h.findMachineByMAC(ctx, rawMAC)
	if machineErr != nil {
		return pxeTarget{installType: vm.InstallConfigPreseed}, false, machineErr
	}
	if targetMachine != nil && targetMachine.IsProvisioningActive() {
		completedRootFS := h.machineUsesCompletedRootFS(ctx, targetMachine)
		diskImageDeploy := h.machineUsesDiskImage(ctx, targetMachine)
		if machineImageApplied(targetMachine) {
			return pxeTarget{
				node:            targetMachine,
				installType:     vm.InstallConfigCurtin,
				variant:         h.resolveOSImageVariant(ctx, targetMachine.OSImageVariantRef()),
				osFamily:        string(targetMachine.OSPreset.Family),
				completedRootFS: completedRootFS,
				diskImageDeploy: diskImageDeploy,
			}, false, nil
		}
		return pxeTarget{
			node:            targetMachine,
			installType:     vm.InstallConfigType(targetMachine.PXEInstallType()),
			variant:         h.resolveOSImageVariant(ctx, targetMachine.OSImageVariantRef()),
			osFamily:        string(targetMachine.OSPreset.Family),
			completedRootFS: completedRootFS,
			diskImageDeploy: diskImageDeploy,
		}, true, nil
	}

	return pxeTarget{installType: vm.InstallConfigPreseed}, false, nil
}

func (h *Handler) machineUsesDiskImage(ctx context.Context, m *machine.Machine) bool {
	if h.osimages == nil || m == nil || strings.TrimSpace(m.OSPreset.ImageRef) == "" {
		return false
	}
	img, err := h.osimages.Get(ctx, strings.TrimSpace(m.OSPreset.ImageRef))
	if err != nil {
		return false
	}
	return osimage.EffectiveImageFormat(img) == osimage.FormatQCOW2 &&
		osimage.SupportsDeploymentTarget(img, osimage.DeploymentTargetBareMetal)
}

func (h *Handler) machineUsesCompletedRootFS(ctx context.Context, m *machine.Machine) bool {
	if machineImageApplied(m) {
		return true
	}
	if h.osimages == nil || m == nil || strings.TrimSpace(m.OSPreset.ImageRef) == "" {
		return false
	}
	img, err := h.osimages.Get(ctx, strings.TrimSpace(m.OSPreset.ImageRef))
	if err != nil {
		return false
	}
	if img.Manifest != nil && img.Manifest.Root.Format != "" {
		return img.Manifest.Root.Format == osimage.FormatSquashFS
	}
	return img.Format == osimage.FormatSquashFS
}

func (h *Handler) resolveOSImageVariant(ctx context.Context, osImageRef string) string {
	if h.osimages == nil || strings.TrimSpace(osImageRef) == "" {
		return ""
	}
	img, err := h.osimages.Get(ctx, strings.TrimSpace(osImageRef))
	if err != nil {
		return ""
	}
	return string(img.Variant)
}

func (h *Handler) resolveOSImageFamily(ctx context.Context, osImageRef string) string {
	if h.osimages == nil || strings.TrimSpace(osImageRef) == "" {
		return ""
	}
	img, err := h.osimages.Get(ctx, strings.TrimSpace(osImageRef))
	if err != nil {
		return ""
	}
	return img.OSFamily
}

func (h *Handler) resolveNetworkConfigRenderer(ctx context.Context, n node.Node) string {
	return networkConfigRendererForOSFamily(h.resolveNodeOSFamily(ctx, n))
}

func (h *Handler) resolveNodeOSFamily(ctx context.Context, n node.Node) string {
	switch t := n.(type) {
	case *machine.Machine:
		if family := strings.TrimSpace(string(t.OSPreset.Family)); family != "" {
			return family
		}
		return h.resolveOSImageFamily(ctx, t.OSImageVariantRef())
	case *vm.VirtualMachine:
		return h.resolveOSImageFamily(ctx, t.OSImageVariantRef())
	default:
		if n != nil {
			return h.resolveOSImageFamily(ctx, n.OSImageVariantRef())
		}
		return ""
	}
}

func networkConfigRendererForOSFamily(osFamily string) string {
	return networkConfigRendererNetworkd
}

func osImageVariantIsDesktop(variant string) bool {
	return strings.ToLower(strings.TrimSpace(variant)) == string(osimage.VariantDesktop)
}

func (h *Handler) findHostByMAC(ctx context.Context, rawMAC string) node.Node {
	if targetVM, foundMAC, err := h.findVirtualMachineByMAC(ctx, rawMAC); err == nil && targetVM != nil && foundMAC {
		return targetVM
	}
	if targetMachine, foundMAC, err := h.findMachineByMAC(ctx, rawMAC); err == nil && targetMachine != nil && foundMAC {
		return targetMachine
	}
	return nil
}

func normalizePXEUserDataInstallType(installType vm.InstallConfigType) vm.InstallConfigType {
	switch installType {
	case vm.InstallConfigCurtin:
		return vm.InstallConfigCurtin
	default:
		return vm.InstallConfigCurtin
	}
}

func (h *Handler) resolvePXEInstallInline(ctx context.Context, rawMAC string, expectedType vm.InstallConfigType) (string, bool, error) {
	n := h.findHostByMAC(ctx, rawMAC)
	if n == nil {
		return "", false, nil
	}

	if inline := n.CloudInitInline(resource.InstallType(expectedType)); inline != "" {
		return inline, true, nil
	}

	if !supportsCloudInitFallbackInstallType(expectedType) {
		return "", false, nil
	}

	ref := n.CloudInitRefForDeploy()
	if ref == "" {
		return "", false, nil
	}
	userData, found, err := h.resolveCloudInitUserData(ctx, ref)
	if err != nil || !found {
		return userData, found, err
	}
	return userData, true, nil
}

func supportsCloudInitFallbackInstallType(installType vm.InstallConfigType) bool {
	return installType == vm.InstallConfigCurtin
}

func pxeTargetToken(target pxeTarget) string {
	if target.node == nil {
		return ""
	}
	return target.node.ProvisionToken()
}

func pxeTargetHostname(target pxeTarget) string {
	if target.node == nil {
		return ""
	}
	return target.node.NodeDisplayName()
}

func parseTokenAndTypeFromURL(completeURL string) (string, string) {
	u, err := url.Parse(completeURL)
	if err != nil {
		return "", ""
	}
	return u.Query().Get("token"), u.Query().Get("type")
}

func buildPXEInstallCompleteURL(base, token string, source vm.InstallConfigType) string {
	if strings.TrimSpace(token) == "" {
		return ""
	}
	q := url.Values{}
	q.Set("token", strings.TrimSpace(token))
	if source != "" {
		q.Set("type", string(source))
	}
	return strings.TrimRight(base, "/") + "/install-complete?" + q.Encode()
}
