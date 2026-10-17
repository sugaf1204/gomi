package sql

import (
	"context"

	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/vm"
)

func (s *VMStore) UpdateProvisioningAttempt(ctx context.Context, before, after vm.VirtualMachine) (bool, error) {
	if before.Name != after.Name || before.Provisioning.CompletionToken != after.Provisioning.CompletionToken || before.Provisioning.AttemptID != after.Provisioning.AttemptID {
		return false, nil
	}
	spec, oldStatus, err := marshalVMColumns(before)
	if err != nil {
		return false, err
	}
	_, nextStatus, err := marshalVMColumns(after)
	if err != nil {
		return false, err
	}
	result, err := s.b.exec(ctx, `UPDATE virtual_machines SET status = ?, updated_at = ? WHERE name = ? AND spec = ? AND status = ? AND hypervisor_ref = ?`, nextStatus, after.UpdatedAt, before.Name, spec, oldStatus, before.HypervisorRef)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	if err == nil && count > 0 {
		s.notify()
	}
	return count > 0, err
}

func (s *MachineStore) UpdateProvisioningAttempt(ctx context.Context, before, after machine.Machine) (bool, error) {
	if before.Name != after.Name || before.Provision == nil || after.Provision == nil || before.Provision.CompletionToken != after.Provision.CompletionToken || before.Provision.AttemptID != after.Provision.AttemptID {
		return false, nil
	}
	spec, oldStatus, err := marshalMachineColumns(before)
	if err != nil {
		return false, err
	}
	_, nextStatus, err := marshalMachineColumns(after)
	if err != nil {
		return false, err
	}
	result, err := s.b.exec(ctx, `UPDATE machines SET status = ?, ip = ?, updated_at = ? WHERE name = ? AND spec = ? AND status = ? AND ip = ?`, nextStatus, after.IP, after.UpdatedAt, before.Name, spec, oldStatus, before.IP)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	if err == nil && count > 0 {
		s.notify()
	}
	return count > 0, err
}
