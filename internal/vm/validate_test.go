package vm

import (
	"testing"
)

func validVM() VirtualMachine {
	return VirtualMachine{
		Name:          "vm-01",
		HypervisorRef: "hv-01",
		Resources:     ResourceSpec{CPUCores: 2, MemoryMB: 2048, DiskGB: 20},
		OSImageRef:    "ubuntu-24.04",
	}
}

func TestValidateVirtualMachine_Valid(t *testing.T) {
	if err := ValidateVirtualMachine(validVM()); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidateVirtualMachine_MissingName(t *testing.T) {
	v := validVM()
	v.Name = ""
	if err := ValidateVirtualMachine(v); err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestValidateVirtualMachine_EmptyHypervisorRefAllowed(t *testing.T) {
	v := validVM()
	v.HypervisorRef = ""
	// hypervisorRef can be empty when auto-placement is used.
	if err := ValidateVirtualMachine(v); err != nil {
		t.Fatalf("expected no error for empty hypervisorRef (auto-placement), got %v", err)
	}
}

func TestValidateVirtualMachine_InvalidCPU(t *testing.T) {
	v := validVM()
	v.Resources.CPUCores = 0
	if err := ValidateVirtualMachine(v); err == nil {
		t.Fatal("expected error for zero cpuCores")
	}
}

func TestValidateVirtualMachine_OSImageRefRequired(t *testing.T) {
	v := validVM()
	v.OSImageRef = ""
	if err := ValidateVirtualMachine(v); err == nil {
		t.Fatal("expected error for missing osImageRef")
	}
}

func TestValidateVirtualMachine_InstallConfigWithInlineRequiresType(t *testing.T) {
	v := validVM()
	v.InstallCfg = &InstallConfig{Inline: "d-i passwd/username string custom"}
	if err := ValidateVirtualMachine(v); err == nil {
		t.Fatal("expected error when installConfig.inline is set without type")
	}
}

func TestValidateVirtualMachine_InvalidInstallConfigType(t *testing.T) {
	v := validVM()
	v.InstallCfg = &InstallConfig{Type: InstallConfigType("kickstart"), Inline: "foo"}
	if err := ValidateVirtualMachine(v); err == nil {
		t.Fatal("expected error for unsupported installConfig.type")
	}
}

func TestValidateVirtualMachine_CurtinInstallConfigType(t *testing.T) {
	v := validVM()
	v.InstallCfg = &InstallConfig{Type: InstallConfigCurtin}
	if err := ValidateVirtualMachine(v); err != nil {
		t.Fatalf("expected curtin installConfig.type to be accepted, got %v", err)
	}
}

func TestValidateVirtualMachine_LegacyCloudInitRefAccepted(t *testing.T) {
	v := validVM()
	v.CloudInitRef = "ci-legacy"
	if err := ValidateVirtualMachine(v); err != nil {
		t.Fatalf("expected no error for legacy cloudInitRef, got %v", err)
	}
}

func TestValidateVirtualMachine_DuplicateCloudInitRefs(t *testing.T) {
	v := validVM()
	v.CloudInitRefs = []string{"ci-01", "ci-01"}
	if err := ValidateVirtualMachine(v); err == nil {
		t.Fatal("expected error for duplicate cloudInitRefs")
	}
}

func TestValidateVirtualMachine_DuplicateLegacyAndCloudInitRefs(t *testing.T) {
	v := validVM()
	v.CloudInitRef = "ci-legacy"
	v.CloudInitRefs = []string{"ci-legacy", "ci-02"}
	if err := ValidateVirtualMachine(v); err == nil {
		t.Fatal("expected error for duplicate cloudInitRef and cloudInitRefs")
	}
}

func TestValidateVirtualMachine_AdvancedOptions_Valid(t *testing.T) {
	v := validVM()
	v.AdvancedOptions = &AdvancedOptions{
		CPUPinning:    map[int]string{0: "0", 1: "2"},
		CPUMode:       CPUModeHostPassthrough,
		IOThreads:     2,
		DiskDriver:    DiskDriverVirtio,
		DiskFormat:    "qcow2",
		NetMultiqueue: 4,
	}
	if err := ValidateVirtualMachine(v); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidateVirtualMachine_AdvancedOptions_InvalidCPUMode(t *testing.T) {
	v := validVM()
	v.AdvancedOptions = &AdvancedOptions{CPUMode: CPUMode("custom")}
	if err := ValidateVirtualMachine(v); err == nil {
		t.Fatal("expected error for unsupported cpuMode")
	}
}

func TestValidateVirtualMachine_AdvancedOptions_NegativeIOThreads(t *testing.T) {
	v := validVM()
	v.AdvancedOptions = &AdvancedOptions{IOThreads: -1}
	if err := ValidateVirtualMachine(v); err == nil {
		t.Fatal("expected error for negative ioThreads")
	}
}

func TestValidateVirtualMachine_AdvancedOptions_NegativeNetMultiqueue(t *testing.T) {
	v := validVM()
	v.AdvancedOptions = &AdvancedOptions{NetMultiqueue: -1}
	if err := ValidateVirtualMachine(v); err == nil {
		t.Fatal("expected error for negative netMultiqueue")
	}
}

func TestValidateVirtualMachine_AdvancedOptions_InvalidDiskDriver(t *testing.T) {
	v := validVM()
	v.AdvancedOptions = &AdvancedOptions{DiskDriver: DiskDriver("nvme")}
	if err := ValidateVirtualMachine(v); err == nil {
		t.Fatal("expected error for unsupported disk driver")
	}
}

func TestValidateVirtualMachine_AdvancedOptions_InvalidDiskFormat(t *testing.T) {
	v := validVM()
	v.AdvancedOptions = &AdvancedOptions{DiskFormat: "vmdk"}
	if err := ValidateVirtualMachine(v); err == nil {
		t.Fatal("expected error for unsupported disk format")
	}
}

func TestValidateVirtualMachine_AdvancedOptions_CPUPinningOutOfRange(t *testing.T) {
	v := validVM()
	v.AdvancedOptions = &AdvancedOptions{
		CPUPinning: map[int]string{5: "0"}, // vm has 2 cores, so vcpu 5 is out of range
	}
	if err := ValidateVirtualMachine(v); err == nil {
		t.Fatal("expected error for cpu pinning out of range")
	}
}

func TestValidateVirtualMachine_AdvancedOptions_EmptyAllowed(t *testing.T) {
	v := validVM()
	v.AdvancedOptions = &AdvancedOptions{}
	if err := ValidateVirtualMachine(v); err != nil {
		t.Fatalf("expected no error for empty advanced options, got %v", err)
	}
}

func TestValidateVirtualMachine_AdvancedOptions_SCSIDiskDriver(t *testing.T) {
	v := validVM()
	v.AdvancedOptions = &AdvancedOptions{DiskDriver: DiskDriverSCSI}
	if err := ValidateVirtualMachine(v); err != nil {
		t.Fatalf("expected scsi disk driver to be accepted, got %v", err)
	}
}

func TestValidateVirtualMachine_AdvancedOptions_RawDiskFormatRejected(t *testing.T) {
	v := validVM()
	v.AdvancedOptions = &AdvancedOptions{DiskFormat: "raw"}
	if err := ValidateVirtualMachine(v); err == nil {
		t.Fatal("expected raw disk format to be rejected")
	}
}
