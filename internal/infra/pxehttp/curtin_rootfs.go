package pxehttp

import (
	"fmt"
	"github.com/sugaf1204/gomi/internal/machine"
	"path/filepath"
	"strings"
)

type osInstallCapability struct {
	Family               string
	RootFilesystem       string
	GrubInstallCommand   string
	GrubEFIArgs          []string
	GrubMkconfigCommand  string
	GrubMkimageCommand   string
	GrubBIOSSetupCommand string
	GrubConfigPath       string
	SimpleGrubConfigPath string
	BootloaderID         string
	InstallRemovableEFI  bool
	NeedsESPGrubConfig   bool
	CopyEFIToFallback    bool
	EmbedBIOSBootstrap   bool
	SkipUEFIGrubInstall  bool
}

func installCapabilityForOSFamily(osFamily string) (osInstallCapability, error) {
	family := strings.ToLower(strings.TrimSpace(osFamily))
	switch family {
	case "fedora":
		return osInstallCapability{
			Family:               family,
			RootFilesystem:       "ext4",
			GrubInstallCommand:   "grub2-install",
			GrubMkconfigCommand:  "grub2-mkconfig",
			GrubMkimageCommand:   "grub2-mkimage",
			GrubBIOSSetupCommand: "grub2-bios-setup",
			GrubConfigPath:       "/boot/grub2/grub.cfg",
			SimpleGrubConfigPath: "/boot/grub2/gomi.cfg",
			BootloaderID:         "fedora",
			NeedsESPGrubConfig:   true,
			CopyEFIToFallback:    true,
			EmbedBIOSBootstrap:   true,
			SkipUEFIGrubInstall:  true,
		}, nil
	case "debian", "ubuntu":
		return osInstallCapability{
			Family:              family,
			RootFilesystem:      "ext4",
			GrubInstallCommand:  "grub-install",
			GrubMkconfigCommand: "grub-mkconfig",
			GrubConfigPath:      "/boot/grub/grub.cfg",
			BootloaderID:        family,
			InstallRemovableEFI: true,
		}, nil
	case "":
		return osInstallCapability{}, fmt.Errorf("osFamily is required for squashfs curtin deploy")
	default:
		return osInstallCapability{}, fmt.Errorf("unsupported OS family for squashfs curtin deploy: %s", family)
	}
}

func buildRootFSFstabCommand(rootFilesystem string) string {
	return fmt.Sprintf(`root_fstype=%s; root_opts=%s; printf '%%s\n' "LABEL=rootfs / $root_fstype $root_opts 0 1" 'LABEL=EFI /boot/efi vfat umask=0077 0 1' > "$TARGET_MOUNT_POINT/etc/fstab"`,
		shellQuote(rootFilesystem),
		shellQuote(rootFSFstabOptions(rootFilesystem)),
	)
}

func rootFSFstabOptions(rootFilesystem string) string {
	switch strings.ToLower(strings.TrimSpace(rootFilesystem)) {
	case "ext4":
		return "defaults,errors=remount-ro"
	default:
		return "defaults"
	}
}

func rootFSGrubModule(rootFilesystem string) string {
	switch strings.ToLower(strings.TrimSpace(rootFilesystem)) {
	case "xfs":
		return "xfs"
	case "btrfs":
		return "btrfs"
	default:
		return "ext2"
	}
}

func buildRootFSBootloaderCommand(cap osInstallCapability, targetDisk string, firmware machine.Firmware, rootFilesystem string) string {
	biosInstall := fmt.Sprintf(`chroot "$TARGET_MOUNT_POINT" %s --target=i386-pc --recheck %s`,
		shellQuote(cap.GrubInstallCommand),
		shellQuote(targetDisk),
	)
	simpleConfig := buildRootFSSimpleGrubConfigCommand(cap)
	if firmware == machine.FirmwareBIOS {
		if cap.EmbedBIOSBootstrap {
			return fmt.Sprintf(`set -e; for d in dev proc sys run; do mountpoint -q "$TARGET_MOUNT_POINT/$d" || mount --bind "/$d" "$TARGET_MOUNT_POINT/$d"; done; chroot "$TARGET_MOUNT_POINT" %s --target=i386-pc --recheck %s; %s; chroot "$TARGET_MOUNT_POINT" %s -O i386-pc -p /boot/grub2 -c /tmp/gomi-grub-bootstrap.cfg -o /boot/grub2/i386-pc/core.img biosdisk part_gpt %s search search_label configfile normal linux gzio; chroot "$TARGET_MOUNT_POINT" %s -d /boot/grub2/i386-pc %s`,
				shellQuote(cap.GrubInstallCommand),
				shellQuote(targetDisk),
				simpleConfig,
				shellQuote(cap.GrubMkimageCommand),
				shellQuote(rootFSGrubModule(rootFilesystem)),
				shellQuote(cap.GrubBIOSSetupCommand),
				shellQuote(targetDisk),
			)
		}
		return fmt.Sprintf(`set -e; for d in dev proc sys run; do mountpoint -q "$TARGET_MOUNT_POINT/$d" || mount --bind "/$d" "$TARGET_MOUNT_POINT/$d"; done; chroot "$TARGET_MOUNT_POINT" %s -o %s; %s`,
			shellQuote(cap.GrubMkconfigCommand),
			shellQuote(cap.GrubConfigPath),
			biosInstall,
		)
	}
	efiArgs := ""
	for _, arg := range cap.GrubEFIArgs {
		if strings.TrimSpace(arg) != "" {
			efiArgs += " " + shellQuote(arg)
		}
	}
	espConfig := ":"
	if cap.NeedsESPGrubConfig {
		configFile := `\$prefix/grub.cfg`
		if cap.SimpleGrubConfigPath != "" {
			configFile = `\$prefix/` + filepath.Base(cap.SimpleGrubConfigPath)
		}
		espConfig = fmt.Sprintf(`bootloader_id=%s; root_uuid="$(blkid -s UUID -o value "$(findmnt -no SOURCE "$TARGET_MOUNT_POINT")")"; cfg="search --no-floppy --fs-uuid --set=root ${root_uuid}\nset prefix=(\$root)/boot/grub2\nconfigfile %s\n"; mkdir -p "$TARGET_MOUNT_POINT/boot/efi/EFI/$bootloader_id" "$TARGET_MOUNT_POINT/boot/efi/EFI/BOOT"; printf '%%b' "$cfg" > "$TARGET_MOUNT_POINT/boot/efi/EFI/$bootloader_id/grub.cfg"; printf '%%b' "$cfg" > "$TARGET_MOUNT_POINT/boot/efi/EFI/BOOT/grub.cfg"`,
			shellQuote(cap.BootloaderID),
			configFile,
		)
	}
	removableArg := ""
	if cap.InstallRemovableEFI {
		removableArg = " --removable"
	}
	fallbackCopy := ":"
	if cap.CopyEFIToFallback {
		fallbackCopy = fmt.Sprintf(`bootloader_id=%s; packaged_efi="$(find "$TARGET_MOUNT_POINT/usr/lib/efi/grub2" -path "*/EFI/$bootloader_id/grubx64.efi" -type f 2>/dev/null | head -n 1 || true)"; if [ -n "$packaged_efi" ]; then mkdir -p "$TARGET_MOUNT_POINT/boot/efi/EFI/$bootloader_id" "$TARGET_MOUNT_POINT/boot/efi/EFI/BOOT"; cp "$packaged_efi" "$TARGET_MOUNT_POINT/boot/efi/EFI/$bootloader_id/grubx64.efi"; cp "$packaged_efi" "$TARGET_MOUNT_POINT/boot/efi/EFI/BOOT/BOOTX64.EFI"; elif [ -f "$TARGET_MOUNT_POINT/boot/efi/EFI/$bootloader_id/grubx64.efi" ]; then mkdir -p "$TARGET_MOUNT_POINT/boot/efi/EFI/BOOT"; cp "$TARGET_MOUNT_POINT/boot/efi/EFI/$bootloader_id/grubx64.efi" "$TARGET_MOUNT_POINT/boot/efi/EFI/BOOT/BOOTX64.EFI"; fi`,
			shellQuote(cap.BootloaderID),
		)
	}
	uefiInstall := fmt.Sprintf(`chroot "$TARGET_MOUNT_POINT" %s --target=x86_64-efi --efi-directory=/boot/efi --bootloader-id=%s%s --recheck%s`,
		shellQuote(cap.GrubInstallCommand),
		shellQuote(cap.BootloaderID),
		removableArg,
		efiArgs,
	)
	if cap.SkipUEFIGrubInstall {
		uefiInstall = ":"
	}
	return fmt.Sprintf(`set -e; for d in dev proc sys run; do mountpoint -q "$TARGET_MOUNT_POINT/$d" || mount --bind "/$d" "$TARGET_MOUNT_POINT/$d"; done; %s; %s; chroot "$TARGET_MOUNT_POINT" %s -o %s; %s; %s`,
		simpleConfig,
		uefiInstall,
		shellQuote(cap.GrubMkconfigCommand),
		shellQuote(cap.GrubConfigPath),
		espConfig,
		fallbackCopy,
	)
}

func buildRootFSSimpleGrubConfigCommand(cap osInstallCapability) string {
	if cap.SimpleGrubConfigPath == "" {
		return ":"
	}
	return fmt.Sprintf(`chroot "$TARGET_MOUNT_POINT" sh -c 'set -e; config_path="$1"; k="$(ls -1 /boot/vmlinuz-* | sort -V | tail -n 1)"; [ -n "$k" ]; v="${k#/boot/vmlinuz-}"; mkdir -p "$(dirname "$config_path")"; cat > "$config_path" <<EOF
set timeout=3
set default=0
search --no-floppy --label rootfs --set=root
menuentry "Linux" {
    linux /boot/vmlinuz-$v root=LABEL=rootfs rw console=tty0 console=ttyS0,115200n8
    initrd /boot/initramfs-$v.img
}
EOF
cat > /tmp/gomi-grub-bootstrap.cfg <<EOF
search --no-floppy --label rootfs --set=root
set prefix=(\$root)/boot/grub2
configfile (\$root)$config_path
EOF' sh %s`,
		shellQuote(cap.SimpleGrubConfigPath),
	)
}
