package pxehttp

import (
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/node"
	"github.com/sugaf1204/gomi/internal/vm"
)

func renderPXEInstallScriptWithVariant(base string, installType vm.InstallConfigType, mac, completeURL, variant string, n node.Node) string {
	profiles := defaultPXEBootScriptProfiles
	if _, ok := n.(*machine.Machine); ok {
		profiles = defaultBareMetalPXEBootScriptProfiles
	}
	return profiles.Script(installType, pxeBootScriptContext{
		baseURL:            base,
		mac:                mac,
		bootIF:             bootIFParam(mac),
		installCompleteURL: completeURL,
		variant:            variant,
		serialConsole:      envBool("GOMI_PXE_SERIAL_CONSOLE"),
	})
}

// RenderNoCloudLineConfig is exported so that api/vm.go can pass it as a callback to vm.Deployer.
func RenderNoCloudLineConfig(base string, installType vm.InstallConfigType, mac string) string {
	return defaultPXEBootScriptProfiles.NoCloudLineConfig(installType, pxeBootScriptContext{
		baseURL: base,
		mac:     mac,
	})
}

func renderPXELocalBootScript(_ string) string {
	return `#!ipxe
iseq ${platform} efi && goto local_efi || goto local_bios
:local_efi
sanboot --no-describe --drive 0 || exit 1
:local_bios
sanboot --no-describe --drive 0x80 || exit
`
}
