package vm

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/sugaf1204/gomi/internal/hypervisor"
	"github.com/sugaf1204/gomi/internal/libvirt"
	"github.com/sugaf1204/gomi/internal/osimage"
)

type Deployer struct {
	Hypervisors *hypervisor.Service
	OSImages    *osimage.Service
	VMs         *Service
	PXEBaseURL  string
	ListenAddr  string
}

func (d *Deployer) Deploy(ctx context.Context, created *VirtualMachine, pxeNoCloudFn func(base string, installType InstallConfigType, mac string) string) error {
	hv, err := d.Hypervisors.Get(ctx, created.HypervisorRef)
	if err != nil {
		log.Printf("deploy vm %s: resolve hypervisor %s: %v", created.Name, created.HypervisorRef, err)
		d.updatePhaseOnError(ctx, created, "resolve-hypervisor", err)
		return fmt.Errorf("resolve hypervisor %s: %w", created.HypervisorRef, err)
	}

	installType := resolveInstallType(*created)
	pxeBaseURL, err := d.resolvePXEBaseURL(hv, installType)
	if err != nil {
		log.Printf("deploy vm %s: resolve pxe base url: %v", created.Name, err)
		d.updatePhaseOnError(ctx, created, "resolve-pxe-base-url", err)
		return fmt.Errorf("resolve pxe base url: %w", err)
	}

	cfg := BuildLibvirtConfig(hv)
	exec, err := libvirt.NewExecutor(cfg)
	if err != nil {
		log.Printf("deploy vm %s: connect to hypervisor: %v", created.Name, err)
		d.updatePhaseOnError(ctx, created, "define", err)
		return fmt.Errorf("connect to hypervisor: %w", err)
	}
	defer exec.Close()

	diskFormat := "qcow2"
	bootDev := "network"

	if installType == InstallConfigCurtin {
		bootDev = "hd"
		backingPath, backingFormat, backingErr := d.prepareCloudImageBacking(ctx, exec, created.OSImageRef)
		if backingErr != nil {
			log.Printf("deploy vm %s: resolve cloud image backing: %v", created.Name, backingErr)
			d.updatePhaseOnError(ctx, created, "resolve-cloudimage", backingErr)
			return fmt.Errorf("resolve cloud image backing: %w", backingErr)
		}
		if err := exec.CreateOverlayVolume(ctx, created.Name, created.Resources.DiskGB, backingPath, backingFormat); err != nil {
			log.Printf("deploy vm %s: create overlay volume: %v", created.Name, err)
			d.updatePhaseOnError(ctx, created, "create-overlay-volume", err)
			return fmt.Errorf("create overlay volume: %w", err)
		}
	} else {
		if err := exec.CreateVolume(ctx, created.Name, created.Resources.DiskGB, diskFormat); err != nil {
			log.Printf("deploy vm %s: create volume: %v", created.Name, err)
			d.updatePhaseOnError(ctx, created, "create-volume", err)
			return fmt.Errorf("create volume: %w", err)
		}
	}

	domainCfg := BuildDomainConfig(*created, created.Name, bootDev, pxeBaseURL, pxeNoCloudFn)
	domainCfg.DiskFormat = diskFormat
	applyInstallStorageOverrides(&domainCfg, installType)
	if installType == InstallConfigCurtin {
		seedPath, seedErr := d.prepareNoCloudSeed(ctx, exec, *created, pxeBaseURL)
		if seedErr != nil {
			log.Printf("deploy vm %s: prepare nocloud seed: %v", created.Name, seedErr)
			d.updatePhaseOnError(ctx, created, "prepare-nocloud-seed", seedErr)
			return fmt.Errorf("prepare nocloud seed: %w", seedErr)
		}
		domainCfg.CloudInit = seedPath
		domainCfg.SMBIOSSerial = ""
	}

	if err := exec.DefineDomain(ctx, domainCfg); err != nil {
		log.Printf("deploy vm %s: define domain: %v", created.Name, err)
		d.updatePhaseOnError(ctx, created, "define", err)
		return fmt.Errorf("define domain: %w", err)
	}

	created.LibvirtDomain = created.Name
	created.CreatedOnHost = hv.Name
	if interfaces, ifaceErr := exec.DomainInterfaces(ctx, created.Name); ifaceErr == nil {
		netStatuses, ips := ConvertRuntimeInterfaces(interfaces)
		created.NetworkInterfaces = netStatuses
		created.IPAddresses = ips
	}

	if created.Phase != PhaseStopped {
		if err := exec.StartDomain(ctx, created.Name); err != nil {
			log.Printf("deploy vm %s: start domain: %v", created.Name, err)
			d.updatePhaseOnError(ctx, created, "start", err)
			return fmt.Errorf("start domain: %w", err)
		}
		if bootDev == "network" {
			if err := exec.SetDomainBootDevice(ctx, created.Name, "hd"); err != nil {
				log.Printf("deploy vm %s: set boot device hd: %v", created.Name, err)
				d.updatePhaseOnError(ctx, created, "set-boot-hd", err)
				return fmt.Errorf("set domain %s boot device to hd: %w", created.Name, err)
			}
		}
		targetPhase := PhaseProvisioning
		lastAction := "create+pxe"
		if bootDev == "hd" {
			lastAction = "create+cloudimage"
		}
		if updated, err := d.VMs.UpdateStatus(ctx, created.Name, targetPhase, lastAction, ""); err == nil {
			*created = updated
		}
	} else {
		if updated, err := d.VMs.UpdateStatus(ctx, created.Name, PhaseCreating, "define", ""); err == nil {
			*created = updated
		}
	}
	return nil
}

func (d *Deployer) Redeploy(ctx context.Context, v VirtualMachine, pxeNoCloudFn func(base string, installType InstallConfigType, mac string) string) error {
	hv, err := d.Hypervisors.Get(ctx, v.HypervisorRef)
	if err != nil {
		return fmt.Errorf("resolve hypervisor %s: %w", v.HypervisorRef, err)
	}

	installType := resolveInstallType(v)
	pxeBaseURL, err := d.resolvePXEBaseURL(hv, installType)
	if err != nil {
		return fmt.Errorf("resolve pxe base url for %s: %w", v.Name, err)
	}

	domainName := strings.TrimSpace(v.LibvirtDomain)
	if domainName == "" {
		domainName = v.Name
	}

	cfg := BuildLibvirtConfig(hv)
	exec, err := libvirt.NewExecutor(cfg)
	if err != nil {
		return fmt.Errorf("connect to hypervisor %s: %w", hv.Name, err)
	}
	defer exec.Close()

	if err := exec.DestroyDomain(ctx, domainName); err != nil && !IsIgnorableDestroyError(err) {
		return fmt.Errorf("stop domain %s: %w", domainName, err)
	}

	if err := exec.UndefineDomain(ctx, domainName); err != nil && !IsIgnorableDestroyError(err) {
		return fmt.Errorf("undefine domain %s before pxe redeploy: %w", domainName, err)
	}

	bootDev := "network"
	if installType == InstallConfigCurtin {
		backingPath, backingFormat, err := d.prepareCloudImageBacking(ctx, exec, v.OSImageRef)
		if err != nil {
			return fmt.Errorf("resolve cloud image backing for %s: %w", domainName, err)
		}
		if err := exec.DeleteVolume(ctx, v.Name); err != nil {
			return fmt.Errorf("delete existing volume %s before cloudimage redeploy: %w", v.Name, err)
		}
		if err := exec.CreateOverlayVolume(ctx, v.Name, v.Resources.DiskGB, backingPath, backingFormat); err != nil {
			return fmt.Errorf("create overlay volume %s for cloudimage redeploy: %w", v.Name, err)
		}
		bootDev = "hd"
	}

	domainCfg := BuildDomainConfig(v, domainName, bootDev, pxeBaseURL, pxeNoCloudFn)
	applyInstallStorageOverrides(&domainCfg, installType)
	if installType == InstallConfigCurtin {
		seedPath, err := d.prepareNoCloudSeed(ctx, exec, v, pxeBaseURL)
		if err != nil {
			return fmt.Errorf("prepare nocloud seed for %s: %w", domainName, err)
		}
		domainCfg.CloudInit = seedPath
		domainCfg.SMBIOSSerial = ""
	}
	if err := exec.DefineDomain(ctx, domainCfg); err != nil {
		return fmt.Errorf("define domain %s for pxe redeploy: %w", domainName, err)
	}

	if err := exec.StartDomain(ctx, domainName); err != nil {
		return fmt.Errorf("start domain %s: %w", domainName, err)
	}

	if bootDev == "network" {
		if err := exec.SetDomainBootDevice(ctx, domainName, "hd"); err != nil {
			return fmt.Errorf("set domain %s boot device to hd: %w", domainName, err)
		}
	}
	return nil
}

func (d *Deployer) updatePhaseOnError(ctx context.Context, created *VirtualMachine, action string, deployErr error) {
	if updated, err := d.VMs.UpdateStatus(ctx, created.Name, PhaseError, action, deployErr.Error()); err == nil {
		*created = updated
	}
}

func resolveInstallType(v VirtualMachine) InstallConfigType {
	if v.InstallCfg == nil || strings.TrimSpace(string(v.InstallCfg.Type)) == "" {
		return InstallConfigPreseed
	}
	return v.InstallCfg.Type
}
