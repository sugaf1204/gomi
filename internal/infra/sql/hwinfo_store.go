package sql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/sugaf1204/gomi/internal/hwinfo"
	"github.com/sugaf1204/gomi/internal/resource"
)

type HWInfoStore struct{ b *Backend }

var _ hwinfo.Store = (*HWInfoStore)(nil)

// hwinfoSpecJSON is the internal JSON shape stored in the spec column.
type hwinfoSpecJSON struct {
	MachineName string             `json:"machineName"`
	AttemptID   string             `json:"attemptId,omitempty"`
	CPU         hwinfo.CPUInfo     `json:"cpu"`
	Memory      hwinfo.MemoryInfo  `json:"memory"`
	Disks       []hwinfo.DiskInfo  `json:"disks,omitempty"`
	NICs        []hwinfo.NICInfo   `json:"nics,omitempty"`
	BIOS        hwinfo.BIOSInfo    `json:"bios,omitempty"`
	PCI         []hwinfo.PCIDevice `json:"pci,omitempty"`
	USB         []hwinfo.USBDevice `json:"usb,omitempty"`
	GPUs        []hwinfo.GPUInfo   `json:"gpus,omitempty"`
	Sensors     []hwinfo.Sensor    `json:"sensors,omitempty"`
	Boot        hwinfo.BootInfo    `json:"boot,omitempty"`
	Runtime     hwinfo.RuntimeInfo `json:"runtime,omitempty"`
}

func (s *HWInfoStore) Upsert(ctx context.Context, info hwinfo.HardwareInfo) error {
	specJSON, err := marshalJSON(hwinfoSpecJSON{
		MachineName: info.MachineName,
		AttemptID:   info.AttemptID,
		CPU:         info.CPU,
		Memory:      info.Memory,
		Disks:       info.Disks,
		NICs:        info.NICs,
		BIOS:        info.BIOS,
		PCI:         info.PCI,
		USB:         info.USB,
		GPUs:        info.GPUs,
		Sensors:     info.Sensors,
		Boot:        info.Boot,
		Runtime:     info.Runtime,
	})
	if err != nil {
		return err
	}

	_, err = s.b.exec(ctx, `
		INSERT INTO hardware_info (machine_name, spec, created_at, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (machine_name) DO UPDATE SET
			spec = EXCLUDED.spec,
			updated_at = EXCLUDED.updated_at`,
		info.MachineName,
		specJSON, info.CreatedAt, info.UpdatedAt,
	)
	return err
}

func (s *HWInfoStore) Get(ctx context.Context, machineName string) (hwinfo.HardwareInfo, error) {
	var info hwinfo.HardwareInfo
	var specJSON string
	err := s.b.queryRow(ctx,
		`SELECT machine_name, spec, created_at, updated_at
		 FROM hardware_info WHERE machine_name = ?`,
		machineName,
	).Scan(
		&info.MachineName,
		&specJSON, &info.CreatedAt, &info.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return hwinfo.HardwareInfo{}, resource.ErrNotFound
	}
	if err != nil {
		return hwinfo.HardwareInfo{}, err
	}

	info.Name = machineName

	var spec hwinfoSpecJSON
	if err := json.Unmarshal([]byte(specJSON), &spec); err != nil {
		return hwinfo.HardwareInfo{}, err
	}
	info.AttemptID = spec.AttemptID
	info.CPU = spec.CPU
	info.Memory = spec.Memory
	info.Disks = spec.Disks
	info.NICs = spec.NICs
	info.BIOS = spec.BIOS
	info.PCI = spec.PCI
	info.USB = spec.USB
	info.GPUs = spec.GPUs
	info.Sensors = spec.Sensors
	info.Boot = spec.Boot
	info.Runtime = spec.Runtime

	return info, nil
}

func (s *HWInfoStore) Delete(ctx context.Context, machineName string) error {
	result, err := s.b.exec(ctx,
		`DELETE FROM hardware_info WHERE machine_name = ?`,
		machineName,
	)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return resource.ErrNotFound
	}
	return nil
}
