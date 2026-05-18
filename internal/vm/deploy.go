package vm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"log"
	"net"
	gohttp "net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sugaf1204/gomi/internal/hypervisor"
	"github.com/sugaf1204/gomi/internal/infra/netdetect"
	"github.com/sugaf1204/gomi/internal/libvirt"
	"github.com/sugaf1204/gomi/internal/osimage"
	"github.com/sugaf1204/gomi/internal/resource"
)

const hypervisorImageDir = "/var/lib/libvirt/images"

var cloudImageBackingLocks sync.Map

type Deployer struct {
	Hypervisors *hypervisor.Service
	OSImages    *osimage.Service
	VMs         *Service
	PXEBaseURL  string
	ListenAddr  string
}

type primaryIPDetector func() (string, error)

type cloudImageStorage interface {
	VolumeExists(ctx context.Context, name string, format string) (bool, error)
	CreateVolumeFromReader(ctx context.Context, name string, sizeBytes int64, format string, r io.Reader) error
	DeleteVolume(ctx context.Context, name string) error
}

func (d *Deployer) Deploy(ctx context.Context, created *VirtualMachine, pxeNoCloudFn func(base string, installType InstallConfigType, mac string) string) error {
	hv, err := d.Hypervisors.Get(ctx, created.HypervisorRef)
	if err != nil {
		log.Printf("deploy vm %s: resolve hypervisor %s: %v", created.Name, created.HypervisorRef, err)
		d.updatePhaseOnError(ctx, created, "resolve-hypervisor", err)
		return fmt.Errorf("resolve hypervisor %s: %w", created.HypervisorRef, err)
	}

	installType := resolveInstallType(*created)
	pxeBaseURL, err := d.resolvePXEBaseURL(hv, installType)
	if err != nil {
		log.Printf("deploy vm %s: resolve pxe base url: %v", created.Name, err)
		d.updatePhaseOnError(ctx, created, "resolve-pxe-base-url", err)
		return fmt.Errorf("resolve pxe base url: %w", err)
	}

	cfg := BuildLibvirtConfig(hv)
	exec, err := libvirt.NewExecutor(cfg)
	if err != nil {
		log.Printf("deploy vm %s: connect to hypervisor: %v", created.Name, err)
		d.updatePhaseOnError(ctx, created, "define", err)
		return fmt.Errorf("connect to hypervisor: %w", err)
	}
	defer exec.Close()

	diskFormat := "qcow2"
	bootDev := "network"

	if installType == InstallConfigCurtin {
		bootDev = "hd"
		backingPath, backingFormat, backingErr := d.prepareCloudImageBacking(ctx, exec, created.OSImageRef)
		if backingErr != nil {
			log.Printf("deploy vm %s: resolve cloud image backing: %v", created.Name, backingErr)
			d.updatePhaseOnError(ctx, created, "resolve-cloudimage", backingErr)
			return fmt.Errorf("resolve cloud image backing: %w", backingErr)
		}
		if err := exec.CreateOverlayVolume(ctx, created.Name, created.Resources.DiskGB, backingPath, backingFormat); err != nil {
			log.Printf("deploy vm %s: create overlay volume: %v", created.Name, err)
			d.updatePhaseOnError(ctx, created, "create-overlay-volume", err)
			return fmt.Errorf("create overlay volume: %w", err)
		}
	} else {
		if err := exec.CreateVolume(ctx, created.Name, created.Resources.DiskGB, diskFormat); err != nil {
			log.Printf("deploy vm %s: create volume: %v", created.Name, err)
			d.updatePhaseOnError(ctx, created, "create-volume", err)
			return fmt.Errorf("create volume: %w", err)
		}
	}

	domainCfg := BuildDomainConfig(*created, created.Name, bootDev, pxeBaseURL, pxeNoCloudFn)
	domainCfg.DiskFormat = diskFormat
	applyInstallStorageOverrides(&domainCfg, installType)

	if err := exec.DefineDomain(ctx, domainCfg); err != nil {
		log.Printf("deploy vm %s: define domain: %v", created.Name, err)
		d.updatePhaseOnError(ctx, created, "define", err)
		return fmt.Errorf("define domain: %w", err)
	}

	created.LibvirtDomain = created.Name
	created.CreatedOnHost = hv.Name
	if interfaces, ifaceErr := exec.DomainInterfaces(ctx, created.Name); ifaceErr == nil {
		netStatuses, ips := ConvertRuntimeInterfaces(interfaces)
		created.NetworkInterfaces = netStatuses
		created.IPAddresses = ips
	}

	if created.Phase != PhaseStopped {
		if err := exec.StartDomain(ctx, created.Name); err != nil {
			log.Printf("deploy vm %s: start domain: %v", created.Name, err)
			d.updatePhaseOnError(ctx, created, "start", err)
			return fmt.Errorf("start domain: %w", err)
		}
		if bootDev == "network" {
			if err := exec.SetDomainBootDevice(ctx, created.Name, "hd"); err != nil {
				log.Printf("deploy vm %s: set boot device hd: %v", created.Name, err)
				d.updatePhaseOnError(ctx, created, "set-boot-hd", err)
				return fmt.Errorf("set domain %s boot device to hd: %w", created.Name, err)
			}
		}
		targetPhase := PhaseProvisioning
		lastAction := "create+pxe"
		if bootDev == "hd" {
			lastAction = "create+cloudimage"
		}
		if updated, err := d.VMs.UpdateStatus(ctx, created.Name, targetPhase, lastAction, ""); err == nil {
			*created = updated
		}
	} else {
		if updated, err := d.VMs.UpdateStatus(ctx, created.Name, PhaseCreating, "define", ""); err == nil {
			*created = updated
		}
	}
	return nil
}

func (d *Deployer) Redeploy(ctx context.Context, v VirtualMachine, pxeNoCloudFn func(base string, installType InstallConfigType, mac string) string) error {
	hv, err := d.Hypervisors.Get(ctx, v.HypervisorRef)
	if err != nil {
		return fmt.Errorf("resolve hypervisor %s: %w", v.HypervisorRef, err)
	}

	installType := resolveInstallType(v)
	pxeBaseURL, err := d.resolvePXEBaseURL(hv, installType)
	if err != nil {
		return fmt.Errorf("resolve pxe base url for %s: %w", v.Name, err)
	}

	domainName := strings.TrimSpace(v.LibvirtDomain)
	if domainName == "" {
		domainName = v.Name
	}

	cfg := BuildLibvirtConfig(hv)
	exec, err := libvirt.NewExecutor(cfg)
	if err != nil {
		return fmt.Errorf("connect to hypervisor %s: %w", hv.Name, err)
	}
	defer exec.Close()

	if err := exec.DestroyDomain(ctx, domainName); err != nil && !IsIgnorableDestroyError(err) {
		return fmt.Errorf("stop domain %s: %w", domainName, err)
	}

	if err := exec.UndefineDomain(ctx, domainName); err != nil && !IsIgnorableDestroyError(err) {
		return fmt.Errorf("undefine domain %s before pxe redeploy: %w", domainName, err)
	}

	bootDev := "network"
	if installType == InstallConfigCurtin {
		backingPath, backingFormat, err := d.prepareCloudImageBacking(ctx, exec, v.OSImageRef)
		if err != nil {
			return fmt.Errorf("resolve cloud image backing for %s: %w", domainName, err)
		}
		if err := exec.DeleteVolume(ctx, v.Name); err != nil {
			return fmt.Errorf("delete existing volume %s before cloudimage redeploy: %w", v.Name, err)
		}
		if err := exec.CreateOverlayVolume(ctx, v.Name, v.Resources.DiskGB, backingPath, backingFormat); err != nil {
			return fmt.Errorf("create overlay volume %s for cloudimage redeploy: %w", v.Name, err)
		}
		bootDev = "hd"
	}

	domainCfg := BuildDomainConfig(v, domainName, bootDev, pxeBaseURL, pxeNoCloudFn)
	applyInstallStorageOverrides(&domainCfg, installType)
	if err := exec.DefineDomain(ctx, domainCfg); err != nil {
		return fmt.Errorf("define domain %s for pxe redeploy: %w", domainName, err)
	}

	if err := exec.StartDomain(ctx, domainName); err != nil {
		return fmt.Errorf("start domain %s: %w", domainName, err)
	}

	if bootDev == "network" {
		if err := exec.SetDomainBootDevice(ctx, domainName, "hd"); err != nil {
			return fmt.Errorf("set domain %s boot device to hd: %w", domainName, err)
		}
	}
	return nil
}

func (d *Deployer) resolveCloudImageBacking(ctx context.Context, osImageRef string) (string, string, error) {
	_, backingPath, backingFormat, err := d.resolveCloudImageBackingImage(ctx, osImageRef)
	return backingPath, backingFormat, err
}

func (d *Deployer) prepareCloudImageBacking(ctx context.Context, storage cloudImageStorage, osImageRef string) (string, string, error) {
	img, backingPath, backingFormat, err := d.resolveCloudImageBackingImage(ctx, osImageRef)
	if err != nil {
		return "", "", err
	}
	if strings.TrimSpace(img.URL) == "" {
		return backingPath, backingFormat, nil
	}
	volumeName := cloudImageBackingVolumeBaseName(img, backingFormat)
	unlock := lockCloudImageBacking(volumeName, backingFormat)
	defer unlock()

	exists, err := storage.VolumeExists(ctx, volumeName, backingFormat)
	if err != nil {
		return "", "", fmt.Errorf("check cloud image backing %s: %w", img.Name, err)
	}
	if exists {
		return backingPath, backingFormat, nil
	}
	if err := d.uploadCloudImageBacking(ctx, storage, img, volumeName, backingFormat); err != nil {
		return "", "", err
	}
	return backingPath, backingFormat, nil
}

func (d *Deployer) resolveCloudImageBackingImage(ctx context.Context, osImageRef string) (osimage.OSImage, string, string, error) {
	if d.OSImages == nil {
		return osimage.OSImage{}, "", "", errors.New("os image service is not configured")
	}
	ref := strings.TrimSpace(osImageRef)
	if ref == "" {
		return osimage.OSImage{}, "", "", errors.New("osImageRef is required for cloudimage deployment")
	}
	img, err := d.OSImages.Get(ctx, ref)
	if err != nil {
		if errors.Is(err, resource.ErrNotFound) {
			return osimage.OSImage{}, "", "", fmt.Errorf("referenced osImageRef not found: %s", ref)
		}
		return osimage.OSImage{}, "", "", err
	}
	if !img.Ready {
		return osimage.OSImage{}, "", "", fmt.Errorf("osImage %s is not ready", ref)
	}
	backingFormat := strings.TrimSpace(string(img.Format))
	if img.Manifest != nil && strings.TrimSpace(string(img.Manifest.Root.Format)) != "" {
		backingFormat = strings.TrimSpace(string(img.Manifest.Root.Format))
	}
	if backingFormat == "" {
		backingFormat = "qcow2"
	}
	if backingFormat != "qcow2" {
		return osimage.OSImage{}, "", "", fmt.Errorf("cloudimage deployment requires qcow2 OS image, got %s", backingFormat)
	}
	if img.Manifest == nil && strings.TrimSpace(img.LocalPath) == "" && strings.TrimSpace(img.URL) == "" {
		return osimage.OSImage{}, "", "", fmt.Errorf("osImage %s has no localPath or url", ref)
	}
	backingPath := filepath.Join(hypervisorImageDir, cloudImageVolumeName(cloudImageBackingVolumeBaseName(img, backingFormat), backingFormat))
	return img, backingPath, backingFormat, nil
}

func lockCloudImageBacking(name string, format string) func() {
	value, _ := cloudImageBackingLocks.LoadOrStore(cloudImageVolumeName(name, format), &sync.Mutex{})
	mu := value.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func cloudImageVolumeName(name string, format string) string {
	if format == "" {
		format = "qcow2"
	}
	suffix := "." + format
	if strings.HasSuffix(name, suffix) {
		return name
	}
	return name + suffix
}

func cloudImageBackingVolumeBaseName(img osimage.OSImage, format string) string {
	name := strings.TrimSpace(img.Name)
	if strings.TrimSpace(img.URL) == "" {
		return name
	}
	suffix := "." + format
	base := strings.TrimSuffix(name, suffix)

	h := sha256.New()
	_, _ = io.WriteString(h, "url="+strings.TrimSpace(img.URL))
	_, _ = io.WriteString(h, "\nchecksum="+normalizeSHA256(img.Checksum))
	_, _ = fmt.Fprintf(h, "\nsize=%d", img.SizeBytes)
	if !img.CreatedAt.IsZero() {
		_, _ = io.WriteString(h, "\ncreated="+img.CreatedAt.UTC().Format(time.RFC3339Nano))
	}
	return fmt.Sprintf("%s-%s", base, hex.EncodeToString(h.Sum(nil))[:12])
}

func (d *Deployer) uploadCloudImageBacking(ctx context.Context, storage cloudImageStorage, img osimage.OSImage, volumeName string, backingFormat string) error {
	rawURL := strings.TrimSpace(img.URL)
	if rawURL == "" {
		return fmt.Errorf("osImage %s has no url", img.Name)
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("invalid cloud image URL for %s", img.Name)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("unsupported cloud image URL scheme for %s: %s", img.Name, parsed.Scheme)
	}
	req, err := gohttp.NewRequestWithContext(ctx, gohttp.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	resp, err := gohttp.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download cloud image %s: %w", img.Name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != gohttp.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return fmt.Errorf("download cloud image %s: status %d", img.Name, resp.StatusCode)
	}
	sizeBytes := img.SizeBytes
	if sizeBytes <= 0 {
		sizeBytes = resp.ContentLength
	}
	if sizeBytes <= 0 {
		return fmt.Errorf("download cloud image %s: content length is required when sizeBytes is unset", img.Name)
	}

	reader := io.Reader(resp.Body)
	var checksum *hashingReader
	if strings.TrimSpace(img.Checksum) != "" {
		checksum = newHashingReader(resp.Body)
		reader = checksum
	}
	if err := storage.CreateVolumeFromReader(ctx, volumeName, sizeBytes, backingFormat, reader); err != nil {
		return fmt.Errorf("sync cloud image %s to hypervisor: %w", img.Name, err)
	}
	if checksum != nil {
		if got, want := checksum.SumHex(), normalizeSHA256(img.Checksum); got != want {
			_ = storage.DeleteVolume(ctx, volumeName)
			return fmt.Errorf("cloud image checksum mismatch for %s: expected %s got %s", img.Name, want, got)
		}
	}
	return nil
}

type hashingReader struct {
	r io.Reader
	h hash.Hash
}

func newHashingReader(r io.Reader) *hashingReader {
	return &hashingReader{r: r, h: sha256.New()}
}

func (r *hashingReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	if n > 0 {
		_, _ = r.h.Write(p[:n])
	}
	return n, err
}

func (r *hashingReader) SumHex() string {
	return hex.EncodeToString(r.h.Sum(nil))
}

func normalizeSHA256(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "sha256:")
	return strings.TrimSpace(value)
}

func (d *Deployer) resolvePXEBaseURL(hv hypervisor.Hypervisor, installType InstallConfigType) (string, error) {
	if base := strings.TrimSpace(d.PXEBaseURL); base != "" {
		return strings.TrimRight(base, "/"), nil
	}
	base, err := resolvePXEBaseURLFromListen(d.ListenAddr, detectPrimaryIP)
	if err != nil && installType == InstallConfigCurtin {
		return "", err
	}
	return base, nil
}

func resolvePXEBaseURLFromListen(listenAddr string, detect primaryIPDetector) (string, error) {
	host, port, err := splitListenAddr(listenAddr)
	if err != nil {
		return "", err
	}
	if port == "" {
		port = "5392"
	}
	if host == "" || isUnspecifiedHost(host) {
		primaryIP, err := detect()
		if err != nil {
			return "", fmt.Errorf("detect primary IP for pxe.http_base_url: %w", err)
		}
		host = primaryIP
	}
	if isLoopbackHost(host) {
		return "", fmt.Errorf("pxe.http_base_url is required when listen_addr %q is loopback-only", listenAddr)
	}
	return "http://" + net.JoinHostPort(host, port) + "/pxe", nil
}

func detectPrimaryIP() (string, error) {
	detected, err := netdetect.Detect()
	if err != nil {
		return "", err
	}
	if detected == nil || strings.TrimSpace(detected.IPAddress) == "" {
		return "", errors.New("primary IPv4 address not found")
	}
	return strings.TrimSpace(detected.IPAddress), nil
}

func splitListenAddr(addr string) (string, string, error) {
	trimmed := strings.TrimSpace(addr)
	if trimmed == "" {
		return "", "5392", nil
	}
	if strings.HasPrefix(trimmed, ":") {
		return "", strings.TrimPrefix(trimmed, ":"), nil
	}
	host, port, err := net.SplitHostPort(trimmed)
	if err == nil {
		return strings.Trim(host, "[]"), port, nil
	}
	if strings.Contains(trimmed, ":") {
		return "", "", fmt.Errorf("invalid listen_addr %q: %w", listenAddrForError(trimmed), err)
	}
	return strings.Trim(trimmed, "[]"), "5392", nil
}

func isUnspecifiedHost(host string) bool {
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsUnspecified()
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(strings.Trim(host, "[]"), "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

func listenAddrForError(addr string) string {
	if addr == "" {
		return "<empty>"
	}
	return addr
}

func (d *Deployer) updatePhaseOnError(ctx context.Context, created *VirtualMachine, action string, deployErr error) {
	if updated, err := d.VMs.UpdateStatus(ctx, created.Name, PhaseError, action, deployErr.Error()); err == nil {
		*created = updated
	}
}

func resolveInstallType(v VirtualMachine) InstallConfigType {
	if v.InstallCfg == nil || strings.TrimSpace(string(v.InstallCfg.Type)) == "" {
		return InstallConfigPreseed
	}
	return v.InstallCfg.Type
}

func BuildLibvirtConfig(hv hypervisor.Hypervisor) libvirt.LibvirtConfig {
	port := hv.Connection.Port
	if port == 0 {
		port = 16509
	}
	return libvirt.LibvirtConfig{
		Host: hv.Connection.Host,
		Port: port,
	}
}

func BuildDomainConfig(v VirtualMachine, domainName, bootDev, pxeBaseURL string, pxeNoCloudFn func(string, InstallConfigType, string) string) libvirt.DomainConfig {
	cfg := libvirt.DomainConfig{
		Name:       domainName,
		VCPU:       v.Resources.CPUCores,
		MemoryMB:   int(v.Resources.MemoryMB),
		DiskPath:   "/var/lib/libvirt/images/" + v.Name + ".qcow2",
		DiskFormat: "qcow2",
		DiskSizeGB: v.Resources.DiskGB,
		BootDev:    bootDev,
	}

	for _, nic := range v.Network {
		cfg.Networks = append(cfg.Networks, libvirt.NetworkConfig{
			Bridge: nic.Bridge,
			MAC:    nic.MAC,
		})
	}

	if opts := v.AdvancedOptions; opts != nil {
		if len(opts.CPUPinning) > 0 {
			cfg.CPUPinning = opts.CPUPinning
		}
		if opts.CPUMode != "" {
			cfg.CPUMode = string(opts.CPUMode)
		}
		if opts.IOThreads > 0 {
			cfg.IOThreads = opts.IOThreads
		}
		if opts.DiskDriver == DiskDriverSCSI {
			cfg.DiskBus = "scsi"
		}
		if opts.NetMultiqueue > 0 {
			cfg.NetQueues = opts.NetMultiqueue
		}
	}

	if pxeBase := strings.TrimSpace(pxeBaseURL); pxeBase != "" && pxeNoCloudFn != nil {
		installType := InstallConfigPreseed
		if v.InstallCfg != nil {
			installType = v.InstallCfg.Type
		}
		mac := vmPrimaryMAC(v)
		if serial := pxeNoCloudFn(pxeBase, installType, mac); serial != "" {
			cfg.SMBIOSSerial = serial
		}
	}

	return cfg
}

func applyInstallStorageOverrides(cfg *libvirt.DomainConfig, installType InstallConfigType) {
	if cfg == nil {
		return
	}
	if installType == InstallConfigCurtin {
		cfg.DiskFormat = "qcow2"
	}
}

func vmPrimaryMAC(v VirtualMachine) string {
	for _, nic := range v.Network {
		if mac := strings.ToLower(strings.TrimSpace(nic.MAC)); mac != "" {
			return mac
		}
	}
	for _, nic := range v.NetworkInterfaces {
		if mac := strings.ToLower(strings.TrimSpace(nic.MAC)); mac != "" {
			return mac
		}
	}
	return ""
}

func IsIgnorableDestroyError(err error) bool {
	if err == nil {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not running") ||
		strings.Contains(msg, "domain is not running") ||
		strings.Contains(msg, "not found") ||
		strings.Contains(msg, "lookup domain")
}
