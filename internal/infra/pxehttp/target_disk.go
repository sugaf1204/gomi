package pxehttp

import (
	"fmt"
	"github.com/sugaf1204/gomi/internal/hwinfo"
	"github.com/sugaf1204/gomi/internal/machine"
	"path/filepath"
	"sort"
	"strings"
)

type selectedTargetDisk struct {
	Path string
	Info hwinfo.DiskInfo
}

func buildRootFSStorageConfig(targetDisk string, rootSizeMB int64, rootFilesystem string) *curtinStorage {
	return &curtinStorage{
		Storage: curtinStorageConfig{
			Version: 1,
			Config: []map[string]any{
				{
					"id":          "disk0",
					"type":        "disk",
					"path":        targetDisk,
					"ptable":      "gpt",
					"grub_device": true,
					"wipe":        "superblock-recursive",
				},
				{
					"id":     "part-bios",
					"type":   "partition",
					"device": "disk0",
					"number": 1,
					"size":   "1M",
					"flag":   "bios_grub",
				},
				{
					"id":     "part-efi",
					"type":   "partition",
					"device": "disk0",
					"number": 2,
					"size":   "512M",
					"flag":   "boot",
				},
				{
					"id":     "fmt-efi",
					"type":   "format",
					"volume": "part-efi",
					"fstype": "fat32",
					"label":  "EFI",
				},
				{
					"id":     "part-root",
					"type":   "partition",
					"device": "disk0",
					"number": 3,
					"size":   fmt.Sprintf("%dM", rootSizeMB),
				},
				{
					"id":     "fmt-root",
					"type":   "format",
					"volume": "part-root",
					"fstype": rootFilesystem,
					"label":  "rootfs",
				},
				{
					"id":     "mount-root",
					"type":   "mount",
					"device": "fmt-root",
					"path":   "/",
				},
				{
					"id":     "mount-efi",
					"type":   "mount",
					"device": "fmt-efi",
					"path":   "/boot/efi",
				},
			},
		},
		Grub: curtinGrub{
			InstallDevices: []string{targetDisk},
		},
	}
}

func rootFSRootPartitionSizeMB(disk hwinfo.DiskInfo) (int64, error) {
	if disk.SizeMB <= 0 {
		return 0, fmt.Errorf("target disk size is required for squashfs storage config")
	}
	rootSizeMB := disk.SizeMB - rootFSBIOSBootPartitionSizeMB - rootFSEFIPartitionSizeMB - rootFSPartitionReserveMB
	if rootSizeMB < rootFSMinimumRootPartitionSizeMB {
		return 0, fmt.Errorf("target disk is too small for squashfs install: size=%dMiB minimum=%dMiB", disk.SizeMB, rootFSMinimumRootPartitionSizeMB+rootFSBIOSBootPartitionSizeMB+rootFSEFIPartitionSizeMB+rootFSPartitionReserveMB)
	}
	return rootSizeMB, nil
}

func selectTargetDisk(m *machine.Machine, info *hwinfo.HardwareInfo) (string, error) {
	selected, err := selectTargetDiskInfo(m, info)
	if err != nil {
		return "", err
	}
	return selected.Path, nil
}

func selectTargetDiskInfo(m *machine.Machine, info *hwinfo.HardwareInfo) (selectedTargetDisk, error) {
	if info == nil {
		return selectedTargetDisk{}, fmt.Errorf("hardware inventory is required for target disk selection")
	}
	candidates := installableDiskCandidates(info)
	if m != nil {
		if disk := strings.TrimSpace(m.TargetDisk); disk != "" {
			return selectInventoryBackedTargetDiskOverrideInfo(disk, candidates)
		}
	}
	if len(candidates) == 0 {
		return selectedTargetDisk{}, fmt.Errorf("no installable target disk found")
	}
	if len(candidates) > 1 {
		return selectedTargetDisk{}, fmt.Errorf("ambiguous target disk: %d installable disks found", len(candidates))
	}
	disk := stableDiskPath(candidates[0])
	if !isWholeDiskPath(disk) {
		return selectedTargetDisk{}, fmt.Errorf("selected target disk is not a whole disk path: %s", disk)
	}
	return selectedTargetDisk{Path: disk, Info: candidates[0]}, nil
}

func selectInventoryBackedTargetDiskOverride(disk string, candidates []hwinfo.DiskInfo) (string, error) {
	selected, err := selectInventoryBackedTargetDiskOverrideInfo(disk, candidates)
	if err != nil {
		return "", err
	}
	return selected.Path, nil
}

func selectInventoryBackedTargetDiskOverrideInfo(disk string, candidates []hwinfo.DiskInfo) (selectedTargetDisk, error) {
	if !isWholeDiskPath(disk) {
		return selectedTargetDisk{}, fmt.Errorf("targetDisk must be a whole disk path: %s", disk)
	}
	for _, candidate := range candidates {
		for _, path := range diskInventoryPaths(candidate) {
			if path == disk {
				return selectedTargetDisk{Path: disk, Info: candidate}, nil
			}
		}
	}
	return selectedTargetDisk{}, fmt.Errorf("targetDisk is not present in current hardware inventory: %s", disk)
}

func installableDiskCandidates(info *hwinfo.HardwareInfo) []hwinfo.DiskInfo {
	if info == nil {
		return nil
	}
	candidates := make([]hwinfo.DiskInfo, 0, len(info.Disks))
	for _, disk := range info.Disks {
		if disk.Removable {
			continue
		}
		name := strings.TrimSpace(disk.Name)
		if name == "" && strings.TrimSpace(disk.Path) != "" {
			name = filepath.Base(strings.TrimSpace(disk.Path))
		}
		if isIgnoredBlockDevice(name) {
			continue
		}
		if disk.Type != "" && disk.Type != "disk" {
			continue
		}
		candidates = append(candidates, disk)
	}
	return candidates
}

func diskPathInInventory(path string, disks []hwinfo.DiskInfo) bool {
	path = strings.TrimSpace(path)
	for _, disk := range disks {
		for _, candidate := range diskInventoryPaths(disk) {
			if candidate == path {
				return true
			}
		}
	}
	return false
}

func diskInventoryPaths(d hwinfo.DiskInfo) []string {
	paths := make([]string, 0, 2+len(d.ByID)+len(d.ByPath))
	if path := strings.TrimSpace(d.Path); path != "" {
		paths = append(paths, path)
	}
	if name := strings.TrimSpace(d.Name); name != "" {
		if strings.HasPrefix(name, "/dev/") {
			paths = append(paths, name)
		} else {
			paths = append(paths, "/dev/"+name)
		}
	}
	paths = append(paths, sortedNonEmpty(d.ByID)...)
	paths = append(paths, sortedNonEmpty(d.ByPath)...)
	return paths
}

func stableDiskPath(d hwinfo.DiskInfo) string {
	if strings.TrimSpace(d.Path) != "" {
		return d.Path
	}
	if byID := sortedNonEmpty(d.ByID); len(byID) > 0 {
		return byID[0]
	}
	if byPath := sortedNonEmpty(d.ByPath); len(byPath) > 0 {
		return byPath[0]
	}
	name := strings.TrimSpace(d.Name)
	if strings.HasPrefix(name, "/dev/") {
		return name
	}
	return "/dev/" + name
}

func sortedNonEmpty(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}

func isIgnoredBlockDevice(name string) bool {
	name = strings.TrimPrefix(strings.TrimSpace(name), "/dev/")
	if name == "" {
		return true
	}
	for _, prefix := range []string{"loop", "ram", "sr", "fd", "dm-", "md", "zram"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func isWholeDiskPath(path string) bool {
	return machine.IsWholeDiskPath(path)
}
