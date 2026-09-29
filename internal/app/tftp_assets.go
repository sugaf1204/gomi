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
	if err := removeLegacyUEFILocalBootGRUBAssets(tftpRoot); err != nil {
		return err
	}
	return ensureIPXEBootAssets(tftpRoot)
}

func removeLegacyUEFILocalBootGRUBAssets(tftpRoot string) error {
	root := strings.TrimSpace(tftpRoot)
	if root == "" {
		return nil
	}
	for _, path := range []string{filepath.Join(root, "grubnetx64.efi"), filepath.Join(root, "grub")} {
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("remove legacy UEFI local boot asset %s: %w", path, err)
		}
	}
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
