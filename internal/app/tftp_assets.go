package app

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
)

const uefiLocalBootGRUBConfig = "exit 1\n"

var uefiLocalBootGRUBCandidates = []string{
	"/usr/lib/grub/x86_64-efi/monolithic/grubnetx64.efi",
	"/usr/lib/grub/x86_64-efi-signed/grubnetx64.efi.signed",
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
	if err := os.MkdirAll(filepath.Join(root, "grub"), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(root, "grub", "grub.cfg"), []byte(uefiLocalBootGRUBConfig), 0o644); err != nil {
		return err
	}
	dst := filepath.Join(root, "grubnetx64.efi")
	for _, src := range uefiLocalBootGRUBCandidates {
		if err := copyFileIfChanged(src, dst, 0o644); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		}
		log.Printf("tftp: installed UEFI local boot GRUB asset %s from %s", dst, src)
		return nil
	}

	return fmt.Errorf("grubnetx64.efi not found; install grub-efi-amd64-signed or grub-efi-amd64-bin")
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
