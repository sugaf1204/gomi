package vm

import (
	"bytes"
	"context"
	"fmt"
	"io"
	gohttp "net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/diskfs/go-diskfs/backend/file"
	"github.com/diskfs/go-diskfs/filesystem/iso9660"
)

func (d *Deployer) prepareNoCloudSeed(ctx context.Context, storage cloudImageStorage, v VirtualMachine, pxeBaseURL string) (string, error) {
	mac := vmPrimaryMAC(v)
	token := macToken(mac)
	if token == "" {
		return "", fmt.Errorf("primary MAC is required for NoCloud seed")
	}
	files, err := fetchNoCloudSeed(ctx, pxeBaseURL, token)
	if err != nil {
		return "", err
	}
	isoBytes, err := buildNoCloudISO(files)
	if err != nil {
		return "", err
	}
	volumeName := noCloudSeedVolumeName(v.Name)
	if err := storage.DeleteVolume(ctx, volumeName); err != nil {
		return "", fmt.Errorf("delete existing seed volume %s: %w", volumeName, err)
	}
	if err := storage.CreateVolumeFromReader(ctx, volumeName, int64(len(isoBytes)), "raw", bytes.NewReader(isoBytes)); err != nil {
		return "", fmt.Errorf("create seed volume %s: %w", volumeName, err)
	}
	return filepath.Join(hypervisorImageDir, noCloudSeedVolumeFileName(v.Name)), nil
}

func fetchNoCloudSeed(ctx context.Context, pxeBaseURL string, token string) (map[string]string, error) {
	base := strings.TrimRight(strings.TrimSpace(pxeBaseURL), "/")
	if base == "" {
		return nil, fmt.Errorf("pxe base URL is required for NoCloud seed")
	}
	client := &gohttp.Client{Timeout: 30 * time.Second}
	files := make(map[string]string, 4)
	for _, name := range []string{"user-data", "meta-data", "vendor-data", "network-config"} {
		req, err := gohttp.NewRequestWithContext(ctx, gohttp.MethodGet, base+"/nocloud/"+token+"/"+name, nil)
		if err != nil {
			return nil, fmt.Errorf("build NoCloud %s request: %w", name, err)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("fetch NoCloud %s: %w", name, err)
		}
		body, readErr := io.ReadAll(resp.Body)
		closeErr := resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read NoCloud %s: %w", name, readErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close NoCloud %s response: %w", name, closeErr)
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("fetch NoCloud %s: status %d: %s", name, resp.StatusCode, strings.TrimSpace(string(body)))
		}
		files[name] = string(body)
	}
	return files, nil
}

func buildNoCloudISO(files map[string]string) ([]byte, error) {
	workspace, err := os.MkdirTemp("", "gomi-nocloud-*")
	if err != nil {
		return nil, fmt.Errorf("create NoCloud workspace: %w", err)
	}
	defer os.RemoveAll(workspace)
	for _, name := range []string{"user-data", "meta-data", "vendor-data", "network-config"} {
		if err := os.WriteFile(filepath.Join(workspace, name), []byte(files[name]), 0o644); err != nil {
			return nil, fmt.Errorf("write NoCloud %s: %w", name, err)
		}
	}

	out, err := os.CreateTemp("", "gomi-nocloud-*.iso")
	if err != nil {
		return nil, fmt.Errorf("create NoCloud ISO: %w", err)
	}
	outPath := out.Name()
	defer os.Remove(outPath)
	defer out.Close()

	storage := file.New(out, false)
	fs, err := iso9660.Create(storage, 0, 0, 2048, workspace)
	if err != nil {
		return nil, fmt.Errorf("create ISO9660 filesystem: %w", err)
	}
	if err := fs.Finalize(iso9660.FinalizeOptions{
		RockRidge:        true,
		Joliet:           true,
		VolumeIdentifier: "cidata",
	}); err != nil {
		return nil, fmt.Errorf("finalize NoCloud ISO: %w", err)
	}
	if err := out.Sync(); err != nil {
		return nil, fmt.Errorf("sync NoCloud ISO: %w", err)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		return nil, fmt.Errorf("read NoCloud ISO: %w", err)
	}
	return data, nil
}

func noCloudSeedVolumeName(vmName string) string {
	return vmName + "-cidata"
}

func noCloudSeedVolumeFileName(vmName string) string {
	return noCloudSeedVolumeName(vmName) + ".raw"
}

func macToken(mac string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(mac)), ":", "")
}
