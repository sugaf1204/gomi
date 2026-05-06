package runner

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	apiinventory "github.com/sugaf1204/gomi/api/inventory"
)

const (
	eventImageApplied  = "image_applied"
	maxEventLogTailLen = 32 * 1024
)

type Runner struct {
	ProcRoot string
	SysRoot  string
	DevRoot  string
	TmpDir   string
	Stdout   io.Writer
	Stderr   io.Writer
	Command  CommandRunner
	Client   *http.Client
}

type CommandRunner interface {
	Run(name string, args ...string) error
}

type ExecCommandRunner struct {
	Stdout io.Writer
	Stderr io.Writer
}

func (r ExecCommandRunner) Run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = r.Stdout
	cmd.Stderr = r.Stderr
	return cmd.Run()
}

type RuntimeConfig struct {
	BaseURL *url.URL
	Token   string
	BootMAC net.HardwareAddr
}

type InventoryResponse struct {
	AttemptID       string `json:"attemptId,omitempty"`
	CurtinConfigURL string `json:"curtinConfigUrl"`
	EventsURL       string `json:"eventsUrl,omitempty"`
}

type timingEvent struct {
	Source           string  `json:"source,omitempty"`
	Name             string  `json:"name"`
	EventType        string  `json:"eventType,omitempty"`
	Message          string  `json:"message,omitempty"`
	Result           string  `json:"result,omitempty"`
	StartedAt        string  `json:"startedAt,omitempty"`
	FinishedAt       string  `json:"finishedAt,omitempty"`
	DurationMillis   int64   `json:"durationMs,omitempty"`
	MonotonicSeconds float64 `json:"monotonicSeconds,omitempty"`
}

func (r *Runner) Run() error {
	r.setDefaults()
	var pending []timingEvent
	record := func(name, message string, fn func() error) error {
		started := time.Now().UTC()
		err := fn()
		finished := time.Now().UTC()
		result := "success"
		if err != nil {
			result = "failure"
		}
		pending = append(pending, timingEvent{
			Source:         "runner",
			Name:           name,
			EventType:      "timing",
			Message:        message,
			Result:         result,
			StartedAt:      started.Format(time.RFC3339Nano),
			FinishedAt:     finished.Format(time.RFC3339Nano),
			DurationMillis: finished.Sub(started).Milliseconds(),
		})
		return err
	}
	cfg, err := r.runtimeConfigFromCmdline()
	if err != nil {
		return err
	}
	r.log("GOMI deploy runtime starting")

	var iface string
	if err := record("runner.select_interface", "selecting PXE network interface", func() error {
		var selectErr error
		iface, selectErr = r.selectIface(cfg.BootMAC)
		return selectErr
	}); err != nil {
		return err
	}
	if err := record("runner.link_up", "bringing PXE network interface up", func() error {
		return r.command().Run("ip", "link", "set", "dev", iface, "up")
	}); err != nil {
		return fmt.Errorf("bring up interface %s: %w", iface, err)
	}
	if err := record("runner.dhcp", "waiting for DHCP lease", func() error {
		return r.command().Run("dhcpcd", "-4", "-w", iface)
	}); err != nil {
		return fmt.Errorf("DHCP failed on %s: %w", iface, err)
	}

	var response InventoryResponse
	if err := record("runner.inventory", "posting hardware inventory", func() error {
		var inventoryErr error
		response, inventoryErr = r.postInventory(cfg)
		return inventoryErr
	}); err != nil {
		return err
	}
	r.postTimingEvents(response.EventsURL, pending)
	r.postInitramfsTimingEvents(response.EventsURL)
	curtinCfg := filepath.Join(r.TmpDir, "gomi-curtin.yaml")
	r.postEvent(response.EventsURL, "progress", "fetching curtin config")
	if err := r.timedEvent(response.EventsURL, "runner.fetch_curtin_config", "fetching curtin config", func() error {
		return r.fetchFile(response.CurtinConfigURL, curtinCfg)
	}); err != nil {
		return fmt.Errorf("curtin config fetch failed: %w", err)
	}
	if st, err := os.Stat(curtinCfg); err != nil || st.Size() == 0 {
		return fmt.Errorf("curtin config is empty")
	}
	r.postEvent(response.EventsURL, "progress", "running curtin install")
	if err := r.timedEvent(response.EventsURL, "runner.curtin_install", "running curtin install", func() error {
		return r.command().Run("curtin", "-c", curtinCfg, "install")
	}); err != nil {
		r.postFailureEvent(response.EventsURL, "curtin install failed", err)
		return fmt.Errorf("curtin install failed: %w", err)
	}
	r.postEvent(response.EventsURL, eventImageApplied, "curtin install completed")
	_ = r.timedEvent(response.EventsURL, "runner.sync", "syncing deployment filesystem state", func() error {
		return r.command().Run("sync")
	})
	return r.timedEvent(response.EventsURL, "runner.reboot", "rebooting into target OS", func() error {
		return r.command().Run("reboot", "-f")
	})
}

func (r *Runner) CollectInventory() (apiinventory.HardwareInventory, error) {
	r.setDefaults()
	inv := apiinventory.HardwareInventory{
		Runtime: apiinventory.RuntimeInfo{KernelVersion: r.kernelVersion()},
		Boot: apiinventory.BootInfo{
			FirmwareMode: r.firmwareMode(),
			EFIVars:      exists(filepath.Join(r.SysRoot, "firmware", "efi", "efivars")),
		},
		Disks: r.collectDisks(),
		NICs:  r.collectNICs(),
	}
	return inv, nil
}

func (r *Runner) setDefaults() {
	if r.ProcRoot == "" {
		r.ProcRoot = "/proc"
	}
	if r.SysRoot == "" {
		r.SysRoot = "/sys"
	}
	if r.DevRoot == "" {
		r.DevRoot = "/dev"
	}
	if r.TmpDir == "" {
		r.TmpDir = "/tmp"
	}
	if r.Stdout == nil {
		r.Stdout = os.Stdout
	}
	if r.Stderr == nil {
		r.Stderr = os.Stderr
	}
	if r.Command == nil {
		r.Command = ExecCommandRunner{Stdout: r.Stdout, Stderr: r.Stderr}
	}
	if r.Client == nil {
		r.Client = http.DefaultClient
	}
}

func (r *Runner) command() CommandRunner {
	r.setDefaults()
	return r.Command
}

func (r *Runner) runtimeConfigFromCmdline() (RuntimeConfig, error) {
	params, err := r.cmdline()
	if err != nil {
		return RuntimeConfig{}, err
	}
	values := cmdlineValues(params)
	rawBase := strings.TrimSpace(values["gomi.base"])
	if rawBase == "" {
		return RuntimeConfig{}, fmt.Errorf("gomi.base is required")
	}
	baseURL, err := url.Parse(rawBase)
	if err != nil || baseURL.Scheme == "" || baseURL.Host == "" {
		return RuntimeConfig{}, fmt.Errorf("invalid gomi.base %q", rawBase)
	}
	token := strings.TrimSpace(values["gomi.token"])
	if token == "" {
		return RuntimeConfig{}, fmt.Errorf("gomi.token is required")
	}
	rawMAC := values["gomi.boot_mac"]
	if strings.TrimSpace(rawMAC) == "" {
		rawMAC = values["BOOTIF"]
	}
	bootMAC, err := parseHardwareAddr(rawMAC)
	if err != nil {
		return RuntimeConfig{}, err
	}
	return RuntimeConfig{BaseURL: baseURL, Token: token, BootMAC: bootMAC}, nil
}

func cmdlineValues(params []string) map[string]string {
	values := make(map[string]string, len(params))
	for _, param := range params {
		key, value, ok := strings.Cut(param, "=")
		if ok {
			values[key] = value
		}
	}
	return values
}

func (r *Runner) cmdline() ([]string, error) {
	if params, ok := cmdlineFromProcFS(r.ProcRoot); ok {
		return params, nil
	}
	data, err := os.ReadFile(filepath.Join(r.ProcRoot, "cmdline"))
	if err != nil {
		return nil, err
	}
	return strings.Fields(string(bytes.ReplaceAll(data, []byte{0}, []byte{' '}))), nil
}

func (r *Runner) selectIface(bootMAC net.HardwareAddr) (string, error) {
	nics := r.collectNICs()
	fallback := ""
	for _, nic := range nics {
		if nic.Name == "lo" || !r.isEthernet(nic.Name) {
			continue
		}
		if fallback == "" {
			fallback = nic.Name
		}
		mac, err := net.ParseMAC(strings.TrimSpace(nic.MAC))
		if err == nil && len(bootMAC) > 0 && bytes.Equal(mac, bootMAC) {
			return nic.Name, nil
		}
	}
	if fallback == "" {
		return "", fmt.Errorf("no network interface found")
	}
	return fallback, nil
}

func (r *Runner) isEthernet(name string) bool {
	return strings.TrimSpace(readString(filepath.Join(r.SysRoot, "class", "net", name, "type"))) == "1"
}

func (r *Runner) collectDisks() []apiinventory.DiskInfo {
	entries, err := os.ReadDir(filepath.Join(r.SysRoot, "block"))
	if err != nil {
		return nil
	}
	disks := make([]apiinventory.DiskInfo, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if ignoredBlockDevice(name) {
			continue
		}
		base := filepath.Join(r.SysRoot, "block", name)
		sizeSectors, _ := strconv.ParseInt(strings.TrimSpace(readString(filepath.Join(base, "size"))), 10, 64)
		removable := strings.TrimSpace(readString(filepath.Join(base, "removable"))) == "1"
		rotational := strings.TrimSpace(readString(filepath.Join(base, "queue", "rotational"))) == "1"
		path := filepath.Join(r.DevRoot, name)
		disks = append(disks, apiinventory.DiskInfo{
			Name:       name,
			Path:       path,
			ByID:       r.linksForDevice(filepath.Join(r.DevRoot, "disk", "by-id"), path),
			ByPath:     r.linksForDevice(filepath.Join(r.DevRoot, "disk", "by-path"), path),
			SizeMB:     sizeSectors / 2048,
			Type:       "disk",
			Model:      strings.TrimSpace(readString(filepath.Join(base, "device", "model"))),
			Serial:     strings.TrimSpace(readString(filepath.Join(base, "device", "serial"))),
			WWN:        strings.TrimSpace(readString(filepath.Join(base, "device", "wwid"))),
			Rotational: rotational,
			Removable:  removable,
		})
	}
	sort.Slice(disks, func(i, j int) bool { return disks[i].Name < disks[j].Name })
	return disks
}

func (r *Runner) collectNICs() []apiinventory.NICInfo {
	entries, err := os.ReadDir(filepath.Join(r.SysRoot, "class", "net"))
	if err != nil {
		return nil
	}
	nics := make([]apiinventory.NICInfo, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		nics = append(nics, r.nicFromSysfs(name, readString(filepath.Join(r.SysRoot, "class", "net", name, "address")), readString(filepath.Join(r.SysRoot, "class", "net", name, "operstate"))))
	}
	sort.Slice(nics, func(i, j int) bool { return nics[i].Name < nics[j].Name })
	return nics
}

func (r *Runner) nicFromSysfs(name, mac, state string) apiinventory.NICInfo {
	base := filepath.Join(r.SysRoot, "class", "net", name)
	device, _ := filepath.EvalSymlinks(filepath.Join(base, "device"))
	nic := apiinventory.NICInfo{
		Name:              name,
		MAC:               strings.TrimSpace(mac),
		State:             strings.TrimSpace(state),
		Modalias:          strings.TrimSpace(readString(filepath.Join(device, "modalias"))),
		PCISlot:           filepath.Base(device),
		VendorID:          strings.TrimSpace(readString(filepath.Join(device, "vendor"))),
		DeviceID:          strings.TrimSpace(readString(filepath.Join(device, "device"))),
		SubsystemVendorID: strings.TrimSpace(readString(filepath.Join(device, "subsystem_vendor"))),
		SubsystemDeviceID: strings.TrimSpace(readString(filepath.Join(device, "subsystem_device"))),
	}
	if speed := strings.TrimSpace(readString(filepath.Join(base, "speed"))); speed != "" {
		nic.Speed = speed
	}
	if driverLink, err := filepath.EvalSymlinks(filepath.Join(device, "driver")); err == nil {
		nic.Driver = filepath.Base(driverLink)
	}
	return nic
}

func (r *Runner) linksForDevice(dir, target string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	targetReal, _ := filepath.EvalSymlinks(target)
	for _, entry := range entries {
		link := filepath.Join(dir, entry.Name())
		resolved, err := filepath.EvalSymlinks(link)
		if err != nil {
			continue
		}
		if resolved == target || resolved == targetReal {
			out = append(out, link)
		}
	}
	sort.Strings(out)
	return out
}

func (r *Runner) kernelVersion() string {
	return strings.TrimSpace(readString(filepath.Join(r.ProcRoot, "sys", "kernel", "osrelease")))
}

func (r *Runner) firmwareMode() string {
	if exists(filepath.Join(r.SysRoot, "firmware", "efi")) {
		return "uefi"
	}
	return "bios"
}

func (r *Runner) log(message string) {
	fmt.Fprintln(r.Stdout, message)
	if console, err := os.OpenFile(filepath.Join(r.DevRoot, "console"), os.O_WRONLY|os.O_APPEND, 0); err == nil {
		_, _ = fmt.Fprintln(console, message)
		_ = console.Close()
	}
	if serial, err := os.OpenFile(filepath.Join(r.DevRoot, "ttyS0"), os.O_WRONLY|os.O_APPEND, 0); err == nil {
		_, _ = fmt.Fprintln(serial, message)
		_ = serial.Close()
	}
}

func (r *Runner) postEvent(eventsURL, typ, message string) {
	r.postEventForm(eventsURL, typ, message, "", "")
}

func (r *Runner) timedEvent(eventsURL, name, message string, fn func() error) error {
	started := time.Now().UTC()
	err := fn()
	finished := time.Now().UTC()
	result := "success"
	if err != nil {
		result = "failure"
	}
	r.postTimingEvents(eventsURL, []timingEvent{{
		Source:         "runner",
		Name:           name,
		EventType:      "timing",
		Message:        message,
		Result:         result,
		StartedAt:      started.Format(time.RFC3339Nano),
		FinishedAt:     finished.Format(time.RFC3339Nano),
		DurationMillis: finished.Sub(started).Milliseconds(),
	}})
	return err
}

func (r *Runner) postTimingEvents(eventsURL string, events []timingEvent) {
	for _, event := range events {
		if strings.TrimSpace(event.Name) == "" {
			continue
		}
		form := url.Values{}
		form.Set("type", "timing")
		form.Set("source", event.Source)
		form.Set("name", event.Name)
		form.Set("message", event.Message)
		form.Set("result", event.Result)
		form.Set("startedAt", event.StartedAt)
		form.Set("finishedAt", event.FinishedAt)
		if event.DurationMillis > 0 {
			form.Set("durationMs", strconv.FormatInt(event.DurationMillis, 10))
		}
		if event.MonotonicSeconds > 0 {
			form.Set("monotonicSeconds", strconv.FormatFloat(event.MonotonicSeconds, 'f', 3, 64))
		}
		r.postForm(eventsURL, form)
	}
}

func (r *Runner) postInitramfsTimingEvents(eventsURL string) {
	for _, path := range []string{"/run/gomi/timing.ndjson", "/tmp/gomi-initramfs-timing.ndjson"} {
		data, err := os.ReadFile(path)
		if err != nil || len(data) == 0 {
			continue
		}
		for _, raw := range strings.Split(string(data), "\n") {
			raw = strings.TrimSpace(raw)
			if raw == "" {
				continue
			}
			var event timingEvent
			if err := json.Unmarshal([]byte(raw), &event); err != nil {
				continue
			}
			if event.Source == "" {
				event.Source = "initramfs"
			}
			if event.EventType == "" {
				event.EventType = "marker"
			}
			r.postTimingEvents(eventsURL, []timingEvent{event})
		}
		return
	}
}

func (r *Runner) postFailureEvent(eventsURL, message string, err error) {
	reason := message
	if err != nil {
		reason = fmt.Sprintf("%s: %v", message, err)
	}
	r.postEventForm(eventsURL, "failed", message, reason, r.curtinLogTail())
}

func (r *Runner) postEventForm(eventsURL, typ, message, reason, logTail string) {
	if strings.TrimSpace(eventsURL) == "" {
		return
	}
	form := url.Values{}
	form.Set("type", typ)
	form.Set("message", message)
	if strings.TrimSpace(reason) != "" {
		form.Set("reason", reason)
	}
	if strings.TrimSpace(logTail) != "" {
		form.Set("logTail", logTail)
	}
	r.postForm(eventsURL, form)
}

func (r *Runner) postForm(eventsURL string, form url.Values) {
	if strings.TrimSpace(eventsURL) == "" {
		return
	}
	resp, err := r.client().PostForm(eventsURL, form)
	if err != nil {
		return
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

func (r *Runner) curtinLogTail() string {
	for _, path := range []string{
		"/tmp/curtin-install.log",
		"/var/log/curtin/install.log",
	} {
		data, err := os.ReadFile(path)
		if err != nil || len(data) == 0 {
			continue
		}
		if len(data) > maxEventLogTailLen {
			data = data[len(data)-maxEventLogTailLen:]
		}
		return string(data)
	}
	return ""
}

func (r *Runner) postInventory(cfg RuntimeConfig) (InventoryResponse, error) {
	inv, err := r.CollectInventory()
	if err != nil {
		return InventoryResponse{}, err
	}
	data, err := json.Marshal(inv)
	if err != nil {
		return InventoryResponse{}, err
	}
	endpoint := endpointURL(cfg.BaseURL, "inventory")
	query := endpoint.Query()
	query.Set("token", cfg.Token)
	endpoint.RawQuery = query.Encode()
	resp, err := r.client().Post(endpoint.String(), "application/json", bytes.NewReader(data))
	if err != nil {
		return InventoryResponse{}, fmt.Errorf("inventory callback failed: %w", err)
	}
	defer resp.Body.Close()
	if err := requireHTTPSuccess(resp); err != nil {
		return InventoryResponse{}, fmt.Errorf("inventory callback failed: %w", err)
	}
	var out InventoryResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return InventoryResponse{}, fmt.Errorf("decode inventory response: %w", err)
	}
	if strings.TrimSpace(out.CurtinConfigURL) == "" {
		return InventoryResponse{}, errors.New("inventory response missing curtinConfigUrl")
	}
	return out, nil
}

func (r *Runner) fetchFile(rawURL, path string) error {
	resp, err := r.client().Get(rawURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := requireHTTPSuccess(resp); err != nil {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(file, resp.Body)
	return err
}

func (r *Runner) client() *http.Client {
	r.setDefaults()
	return r.Client
}

func endpointURL(base *url.URL, path string) *url.URL {
	out := *base
	out.Path = strings.TrimRight(out.Path, "/") + "/" + strings.TrimLeft(path, "/")
	out.RawQuery = ""
	out.Fragment = ""
	return &out
}

func requireHTTPSuccess(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return fmt.Errorf("HTTP %s", resp.Status)
}

func ignoredBlockDevice(name string) bool {
	for _, prefix := range []string{"loop", "ram", "sr", "fd", "dm-", "md", "zram"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func parseHardwareAddr(value string) (net.HardwareAddr, error) {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return nil, nil
	}
	raw = strings.TrimPrefix(raw, "01-")
	raw = strings.TrimPrefix(raw, "01:")
	mac, err := net.ParseMAC(strings.ReplaceAll(raw, "-", ":"))
	if err != nil {
		return nil, fmt.Errorf("invalid boot MAC %q: %w", value, err)
	}
	return mac, nil
}

func readString(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(bytes.TrimSpace(data))
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
