package pxehttp

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/sugaf1204/gomi/internal/vm"
)

type pxeBootScriptContext struct {
	baseURL            string
	mac                string
	bootIF             string
	installCompleteURL string
	variant            string // "server" (default) or "desktop"
	serialConsole      bool
}

type pxeBootScriptProfile interface {
	Script(ctx pxeBootScriptContext) string
	NoCloudLineConfig(ctx pxeBootScriptContext) string
}

// profileKey creates a lookup key combining install type and variant.
type profileKey struct {
	installType vm.InstallConfigType
	variant     string
}

type pxeBootScriptProfiles struct {
	defaultProfile pxeBootScriptProfile
	byType         map[vm.InstallConfigType]pxeBootScriptProfile
	byKey          map[profileKey]pxeBootScriptProfile
}

func newPXEBootScriptProfiles(defaultProfile pxeBootScriptProfile, byType map[vm.InstallConfigType]pxeBootScriptProfile) pxeBootScriptProfiles {
	copyMap := make(map[vm.InstallConfigType]pxeBootScriptProfile, len(byType))
	for k, v := range byType {
		copyMap[k] = v
	}
	return pxeBootScriptProfiles{
		defaultProfile: defaultProfile,
		byType:         copyMap,
		byKey:          make(map[profileKey]pxeBootScriptProfile),
	}
}

func (p *pxeBootScriptProfiles) registerVariant(installType vm.InstallConfigType, variant string, profile pxeBootScriptProfile) {
	p.byKey[profileKey{installType: installType, variant: variant}] = profile
}

func (p pxeBootScriptProfiles) resolve(installType vm.InstallConfigType, variant string) pxeBootScriptProfile {
	if variant != "" {
		if profile, ok := p.byKey[profileKey{installType: installType, variant: variant}]; ok {
			return profile
		}
	}
	if profile, ok := p.byType[installType]; ok {
		return profile
	}
	return p.defaultProfile
}

func (p pxeBootScriptProfiles) Script(installType vm.InstallConfigType, ctx pxeBootScriptContext) string {
	return p.resolve(installType, ctx.variant).Script(ctx)
}

func (p pxeBootScriptProfiles) NoCloudLineConfig(installType vm.InstallConfigType, ctx pxeBootScriptContext) string {
	return p.resolve(installType, ctx.variant).NoCloudLineConfig(ctx)
}

type ubuntuCurtinPXEBootProfile struct{}

func (ubuntuCurtinPXEBootProfile) Script(ctx pxeBootScriptContext) string {
	return renderPXELocalBootScript(ctx.baseURL)
}

func (ubuntuCurtinPXEBootProfile) NoCloudLineConfig(ctx pxeBootScriptContext) string {
	token := macToken(ctx.mac)
	if token == "" {
		return ""
	}
	return fmt.Sprintf("ds=nocloud;s=%s/nocloud/%s/", ctx.baseURL, token)
}

type debianPreseedPXEBootProfile struct{}

func (debianPreseedPXEBootProfile) Script(ctx pxeBootScriptContext) string {
	cfgURL := "${base}/preseed.cfg"
	if normalized := normalizeMAC(ctx.mac); normalized != "" {
		cfgURL += "?mac=" + url.QueryEscape(normalized)
	}
	return fmt.Sprintf(`#!ipxe
dhcp
set base %s
kernel ${base}/files/debian/linux initrd=initrd.gz auto=true priority=critical url=%s netcfg/get_nameservers=8.8.8.8 console=ttyS0,115200n8 ---
initrd --name initrd.gz ${base}/files/debian/initrd.gz
boot || shell
`, ctx.baseURL, cfgURL)
}

func (debianPreseedPXEBootProfile) NoCloudLineConfig(_ pxeBootScriptContext) string {
	return ""
}

// debianDesktopPreseedPXEBootProfile is like debianPreseedPXEBootProfile but without serial console.
type debianDesktopPreseedPXEBootProfile struct{}

func (debianDesktopPreseedPXEBootProfile) Script(ctx pxeBootScriptContext) string {
	cfgURL := "${base}/preseed.cfg"
	if normalized := normalizeMAC(ctx.mac); normalized != "" {
		cfgURL += "?mac=" + url.QueryEscape(normalized)
	}
	return fmt.Sprintf(`#!ipxe
dhcp
set base %s
kernel ${base}/files/debian/linux initrd=initrd.gz auto=true priority=critical url=%s ---
initrd --name initrd.gz ${base}/files/debian/initrd.gz
boot || shell
`, ctx.baseURL, cfgURL)
}

func (debianDesktopPreseedPXEBootProfile) NoCloudLineConfig(_ pxeBootScriptContext) string {
	return ""
}

// ubuntuDesktopCurtinPXEBootProfile is like ubuntuCurtinPXEBootProfile but without serial console.
type ubuntuDesktopCurtinPXEBootProfile struct{}

func (ubuntuDesktopCurtinPXEBootProfile) Script(ctx pxeBootScriptContext) string {
	return renderPXELocalBootScript(ctx.baseURL)
}

func (ubuntuDesktopCurtinPXEBootProfile) NoCloudLineConfig(ctx pxeBootScriptContext) string {
	token := macToken(ctx.mac)
	if token == "" {
		return ""
	}
	return fmt.Sprintf("ds=nocloud;s=%s/nocloud/%s/", ctx.baseURL, token)
}

type linuxMachineCurtinPXEBootProfile struct{}

func (linuxMachineCurtinPXEBootProfile) Script(ctx pxeBootScriptContext) string {
	token, _ := parseTokenAndTypeFromURL(ctx.installCompleteURL)
	bootMAC := normalizeMAC(ctx.mac)
	bootParams := ""
	if ctx.bootIF != "" {
		bootParams += " BOOTIF=" + ctx.bootIF
	}
	if bootMAC != "" {
		bootParams += " gomi.boot_mac=" + bootMAC
	}
	consoleParams := "console=tty0"
	if ctx.serialConsole {
		consoleParams += " console=ttyS0,115200n8"
	}
	return fmt.Sprintf(`#!ipxe
echo GOMI: before dhcp
dhcp
set base %s
echo GOMI: before kernel
kernel ${base}/files/linux/boot-kernel initrd=boot-initrd ip=dhcp overlayroot=tmpfs:recurse=0 rw root=squash:${base}/files/linux/rootfs.squashfs gomi.base=${base} gomi.token=%s%s %s ---
echo GOMI: before initrd
initrd --name boot-initrd ${base}/files/linux/boot-initrd
echo GOMI: after initrd
imgstat
echo GOMI: booting
boot || shell
`, ctx.baseURL, token, bootParams, consoleParams)
}

func (linuxMachineCurtinPXEBootProfile) NoCloudLineConfig(_ pxeBootScriptContext) string {
	return ""
}

type ubuntuDesktopAutoinstallPXEBootProfile struct{}

func (ubuntuDesktopAutoinstallPXEBootProfile) Script(ctx pxeBootScriptContext) string {
	return fmt.Sprintf(`#!ipxe
dhcp
set base %s
kernel ${base}/files/ubuntu/vmlinuz initrd=initrd ip=dhcp url=${base}/files/ubuntu/ubuntu.iso autoinstall ds=nocloud;s=${base}/nocloud/%s/ ---
initrd ${base}/files/ubuntu/initrd
boot || shell
`, ctx.baseURL, macToken(ctx.mac))
}

func (ubuntuDesktopAutoinstallPXEBootProfile) NoCloudLineConfig(ctx pxeBootScriptContext) string {
	token := macToken(ctx.mac)
	if token == "" {
		return ""
	}
	return fmt.Sprintf("ds=nocloud;s=%s/nocloud/%s/", ctx.baseURL, token)
}

var defaultPXEBootScriptProfiles = func() pxeBootScriptProfiles {
	p := newPXEBootScriptProfiles(
		debianPreseedPXEBootProfile{},
		map[vm.InstallConfigType]pxeBootScriptProfile{
			vm.InstallConfigCurtin:  ubuntuCurtinPXEBootProfile{},
			vm.InstallConfigPreseed: debianPreseedPXEBootProfile{},
		},
	)
	// Desktop variant profiles (without serial console).
	p.registerVariant(vm.InstallConfigPreseed, "desktop", debianDesktopPreseedPXEBootProfile{})
	p.registerVariant(vm.InstallConfigCurtin, "desktop", ubuntuDesktopCurtinPXEBootProfile{})
	return p
}()

var defaultBareMetalPXEBootScriptProfiles = func() pxeBootScriptProfiles {
	p := newPXEBootScriptProfiles(
		linuxMachineCurtinPXEBootProfile{},
		map[vm.InstallConfigType]pxeBootScriptProfile{
			vm.InstallConfigCurtin:  linuxMachineCurtinPXEBootProfile{},
			vm.InstallConfigPreseed: debianPreseedPXEBootProfile{},
		},
	)
	p.registerVariant(vm.InstallConfigPreseed, "desktop", debianDesktopPreseedPXEBootProfile{})
	return p
}()

func bootIFParam(mac string) string {
	normalized := normalizeMAC(mac)
	if normalized == "" {
		return ""
	}
	return "01-" + strings.ReplaceAll(normalized, ":", "-")
}
