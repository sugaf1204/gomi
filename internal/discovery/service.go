package discovery

import (
	"context"
	"errors"
	"time"

	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/resource"
)

type Service struct {
	machines machine.Store
}

func NewService(machines machine.Store) *Service {
	return &Service{machines: machines}
}

func (s *Service) HandlePXEBoot(ctx context.Context, mac, clientHostname, arch, firmware string) (*machine.Machine, error) {
	existing, err := s.machines.GetByMAC(ctx, mac)
	if err == nil {
		return &existing, nil
	}
	if !errors.Is(err, resource.ErrNotFound) {
		return nil, err
	}

	name := generateName(mac, clientHostname)
	now := time.Now().UTC()

	fw := machine.FirmwareUEFI
	if firmware == "bios" {
		fw = machine.FirmwareBIOS
	}

	m := machine.Machine{
		Name:      name,
		Hostname:  name,
		MAC:       mac,
		Arch:      arch,
		Firmware:  fw,
		Phase:     machine.PhaseDiscovered,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.machines.Upsert(ctx, m); err != nil {
		return nil, err
	}
	return &m, nil
}
