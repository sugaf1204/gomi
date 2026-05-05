package vm

import (
	"context"
	"fmt"
	"math/rand/v2"
	"time"

	golibvirt "github.com/digitalocean/go-libvirt"

	"github.com/sugaf1204/gomi/internal/hypervisor"
	"github.com/sugaf1204/gomi/internal/libvirt"
)

type Migrator struct {
	Hypervisors *hypervisor.Service
	VMs         *Service
}

func (m *Migrator) Migrate(ctx context.Context, v VirtualMachine, targetHVName string) (VirtualMachine, error) {
	sourceHV, err := m.Hypervisors.Get(ctx, v.HypervisorRef)
	if err != nil {
		return v, fmt.Errorf("resolve source hypervisor: %w", err)
	}

	if targetHVName == "" {
		hvList, err := m.Hypervisors.List(ctx)
		if err != nil {
			return v, fmt.Errorf("list hypervisors: %w", err)
		}
		var candidates []hypervisor.Hypervisor
		for _, h := range hvList {
			if h.Name == sourceHV.Name {
				continue
			}
			if h.Phase != hypervisor.PhaseReady {
				continue
			}
			if AvailableMemory(h) > 0 {
				candidates = append(candidates, h)
			}
		}
		if len(candidates) == 0 {
			return v, fmt.Errorf("no suitable target hypervisors available for migration")
		}
		var totalWeight int64
		for _, h := range candidates {
			totalWeight += AvailableMemory(h)
		}
		pick := rand.Int64N(totalWeight)
		var cumulative int64
		for _, h := range candidates {
			cumulative += AvailableMemory(h)
			if pick < cumulative {
				targetHVName = h.Name
				break
			}
		}
		if targetHVName == "" {
			targetHVName = candidates[len(candidates)-1].Name
		}
	}

	if targetHVName == sourceHV.Name {
		return v, fmt.Errorf("target hypervisor must be different from source")
	}

	targetHV, err := m.Hypervisors.Get(ctx, targetHVName)
	if err != nil {
		return v, fmt.Errorf("resolve target hypervisor: %w", err)
	}

	if updated, err := m.VMs.UpdateStatus(ctx, v.Name, PhaseMigrating, "migrate", ""); err == nil {
		v = updated
	}

	srcCfg := BuildLibvirtConfig(sourceHV)
	exec, err := libvirt.NewExecutor(srcCfg)
	if err != nil {
		m.VMs.UpdateStatus(ctx, v.Name, PhaseError, "migrate", fmt.Sprintf("connect to source: %v", err))
		return v, fmt.Errorf("connect to source hypervisor: %w", err)
	}
	defer exec.Close()

	domainName := v.LibvirtDomain
	if domainName == "" {
		domainName = v.Name
	}

	destURI := fmt.Sprintf("qemu+tcp://%s/system", targetHV.Connection.Host)

	golibvirtFlags := golibvirt.MigrateLive |
		golibvirt.MigratePeer2peer |
		golibvirt.MigratePersistDest |
		golibvirt.MigrateUndefineSource |
		golibvirt.MigrateUnsafe

	if migrateErr := exec.MigrateDomain(ctx, domainName, destURI, golibvirtFlags); migrateErr != nil {
		m.VMs.UpdateStatus(ctx, v.Name, PhaseError, "migrate", migrateErr.Error())
		return v, fmt.Errorf("migration failed: %w", migrateErr)
	}

	v.HypervisorRef = targetHVName
	v.HypervisorName = targetHVName
	v.CreatedOnHost = targetHVName
	v.Phase = PhaseRunning
	v.LastPowerAction = "migrate"
	v.LastError = ""
	v.UpdatedAt = time.Now().UTC()
	if err := m.VMs.Store().Upsert(ctx, v); err != nil {
		return v, fmt.Errorf("update vm after migration: %w", err)
	}

	return v, nil
}
