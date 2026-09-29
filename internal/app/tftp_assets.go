package app

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const uefiLocalBootGRUBConfig = "exit 1\n"
const uefiLocalBootGRUBFile = "grub-localbootx64.efi"

var runGRUBMkstandalone = func(args ...string) ([]byte, error) {
	return exec.Command("/usr/bin/grub-mkstandalone", args...).CombinedOutput()
}

func buildUEFILocalBootGRUB(output, config string) error {
	combined, err := runGRUBMkstandalone(
		"-O", "x86_64-efi",
		"-o", output,
		"boot/grub/grub.cfg="+config,
	)
	if err != nil {
		return fmt.Errorf("build UEFI local-boot GRUB image: %w: %s", err, strings.TrimSpace(string(combined)))
	}
	return nil
}

type tftpBootAsset struct {
	dst        string
	candidates []string
	hint       string
}

var ipxeBootAssets = []tftpBootAsset{
	{
		dst:        "ipxe.efi",
		candidates: []string{"/usr/lib/ipxe/ipxe.efi"},
		hint:       "install ipxe",
	},
	{
		dst:        "undionly.kpxe",
		candidates: []string{"/usr/lib/ipxe/undionly.kpxe"},
		hint:       "install ipxe",
	},
}

func ensureTFTPBootAssets(tftpRoot string) error {
	if err := ensureUEFILocalBootGRUBAssets(tftpRoot); err != nil {
		return err
	}
	return ensureIPXEBootAssets(tftpRoot)
}

func ensureUEFILocalBootGRUBAssets(tftpRoot string) error {
	root := strings.TrimSpace(tftpRoot)
	if root == "" {
		return nil
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}

	config, err := os.CreateTemp(root, ".grub-localboot-*.cfg")
	if err != nil {
		return err
	}
	configPath := config.Name()
	defer os.Remove(configPath)
	if _, err := config.WriteString(uefiLocalBootGRUBConfig); err != nil {
		config.Close()
		return err
	}
	if err := config.Close(); err != nil {
		return err
	}

	output, err := os.CreateTemp(root, ".grub-localboot-*.efi")
	if err != nil {
		return err
	}
	outputPath := output.Name()
	if err := output.Close(); err != nil {
		os.Remove(outputPath)
		return err
	}
	defer os.Remove(outputPath)

	if err := buildUEFILocalBootGRUB(outputPath, configPath); err != nil {
		return err
	}
	info, err := os.Stat(outputPath)
	if err != nil {
		return err
	}
	if info.Size() == 0 {
		return fmt.Errorf("built UEFI local-boot GRUB image is empty")
	}
	if err := os.Chmod(outputPath, 0o644); err != nil {
		return err
	}
	dst := filepath.Join(root, uefiLocalBootGRUBFile)
	if err := os.Rename(outputPath, dst); err != nil {
		return err
	}
	log.Printf("tftp: installed self-contained UEFI local boot GRUB asset %s", dst)
	return nil
}

func ensureIPXEBootAssets(tftpRoot string) error {
	root := strings.TrimSpace(tftpRoot)
	if root == "" {
		return nil
	}
	for _, asset := range ipxeBootAssets {
		if err := installTFTPBootAsset(root, asset); err != nil {
			return err
		}
	}
	return nil
}

func installTFTPBootAsset(tftpRoot string, asset tftpBootAsset) error {
	dst := filepath.Join(tftpRoot, asset.dst)
	for _, src := range asset.candidates {
		if err := copyFileIfChanged(src, dst, 0o644); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		}
		log.Printf("tftp: installed PXE boot asset %s from %s", dst, src)
		return nil
	}
	return fmt.Errorf("%s not found; %s", asset.dst, asset.hint)
}

func copyFileIfChanged(src, dst string, mode fs.FileMode) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if current, err := os.ReadFile(dst); err == nil && bytes.Equal(current, data) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(dst, data, mode); err != nil {
		return err
	}
	return nil
}
