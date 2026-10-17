package memory

import (
	"context"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/vm"
	"reflect"
)

func (s *VMStore) UpdateProvisioningAttempt(_ context.Context, before, after vm.VirtualMachine) (bool, error) {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	current, ok := s.b.vms[before.Name]
	if !ok || before.Name != after.Name || before.Provisioning.CompletionToken != after.Provisioning.CompletionToken || before.Provisioning.AttemptID != after.Provisioning.AttemptID || !reflect.DeepEqual(current, before) {
		return false, nil
	}
	s.b.vms[after.Name] = after
	s.notify()
	return true, nil
}

func (s *MachineStore) UpdateProvisioningAttempt(_ context.Context, before, after machine.Machine) (bool, error) {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	current, ok := s.b.machines[before.Name]
	if !ok || before.Name != after.Name || before.Provision == nil || after.Provision == nil || before.Provision.CompletionToken != after.Provision.CompletionToken || before.Provision.AttemptID != after.Provision.AttemptID || !reflect.DeepEqual(current, before) {
		return false, nil
	}
	s.b.machines[after.Name] = after
	s.notify()
	return true, nil
}
