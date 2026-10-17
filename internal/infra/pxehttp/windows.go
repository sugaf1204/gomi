package pxehttp

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/labstack/echo/v4"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/node"
	"github.com/sugaf1204/gomi/internal/osimage"
	"github.com/sugaf1204/gomi/internal/vm"
)

type windowsTarget struct {
	vm                                  *vm.VirtualMachine
	machine                             *machine.Machine
	image                               osimage.OSImage
	attempt                             string
	active, started, applied, completed bool
	deadline                            *time.Time
}

func (t windowsTarget) host() node.Node {
	if t.vm != nil {
		return t.vm
	}
	return t.machine
}

func (h *Handler) windowsTarget(ctx context.Context, token, attempt string) (windowsTarget, error) {
	var t windowsTarget
	if token == "" || attempt == "" {
		return t, echo.NewHTTPError(http.StatusBadRequest, "token and attempt_id are required")
	}
	v, err := h.findVirtualMachineByProvisionToken(ctx, token)
	if err != nil {
		return t, err
	}
	imageRef := ""
	if v != nil && v.PXEInstallType() == "windows" {
		t.vm, t.attempt, t.active = v, v.Provisioning.AttemptID, v.Provisioning.Active
		t.started = v.Provisioning.WindowsInstallStarted
		t.applied, t.completed, t.deadline = v.Provisioning.ImageApplied, v.Provisioning.CompletedAt != nil, v.Provisioning.DeadlineAt
		imageRef = v.OSImageRef
	} else {
		m, err := h.findMachineByProvisionToken(ctx, token)
		if err != nil {
			return t, err
		}
		if m == nil || m.Provision == nil || m.PXEInstallType() != "windows" {
			return t, echo.NewHTTPError(http.StatusNotFound, "Windows provisioning token not found")
		}
		t.machine, t.attempt, t.active = m, m.Provision.AttemptID, m.Provision.Active
		t.started = m.Provision.Artifacts["windowsInstallStarted"] == "true"
		t.applied, t.completed, t.deadline = machineImageApplied(m), m.Provision.CompletedAt != nil, m.Provision.DeadlineAt
		imageRef = m.OSPreset.ImageRef
	}
	if t.attempt != attempt {
		return t, echo.NewHTTPError(http.StatusConflict, "provisioning attempt mismatch")
	}
	if !t.completed && t.deadline != nil && time.Now().After(*t.deadline) {
		return t, echo.NewHTTPError(http.StatusGone, "Windows provisioning attempt expired")
	}
	if h.osimages == nil {
		return t, fmt.Errorf("OS image service unavailable")
	}
	t.image, err = h.osimages.Get(ctx, imageRef)
	if err != nil {
		return t, err
	}
	if t.image.Manifest == nil || t.image.Manifest.Windows == nil || !strings.EqualFold(t.image.OSFamily, "windows") {
		return t, echo.NewHTTPError(http.StatusConflict, "Windows image metadata unavailable")
	}
	return t, nil
}

func windowsAttempt(n node.Node) string {
	switch t := n.(type) {
	case *vm.VirtualMachine:
		return t.Provisioning.AttemptID
	case *machine.Machine:
		if t.Provision != nil {
			return t.Provision.AttemptID
		}
	}
	return ""
}

func (h *Handler) renderWindowsBoot(ctx context.Context, base string, target pxeTarget) (string, error) {
	token, attempt := pxeTargetToken(target), windowsAttempt(target.node)
	t, err := h.windowsTarget(ctx, token, attempt)
	if err != nil {
		return "", err
	}
	if !t.active || t.started || t.applied {
		return renderPXELocalBootScript(base), nil
	}
	pe := t.image.Manifest.Windows.WinPE
	var urls []string
	for _, path := range []string{pe.Wimboot, pe.BCD, pe.BootSDI, pe.BootWIM} {
		u, err := h.artifactURL(base, t.image, path)
		if err != nil {
			return "", err
		}
		urls = append(urls, u)
	}
	startup := appendProvisionQuery(strings.TrimRight(base, "/")+"/windows/startnet.cmd", token, attempt)
	return fmt.Sprintf(`#!ipxe
iseq ${platform} efi || goto unsupported
dhcp
kernel %s
initrd --name BCD %s BCD
initrd --name boot.sdi %s boot.sdi
initrd --name boot.wim %s boot.wim
initrd --name startnet.cmd %s startnet.cmd
boot || goto failed
:unsupported
echo GOMI Windows deployment requires x86_64 UEFI
exit 1
:failed
echo GOMI Windows boot failed
exit 1
`, urls[0], urls[1], urls[2], urls[3], startup), nil
}

func psQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }

func powershellEncoded(command string) string {
	units := utf16.Encode([]rune(command))
	buf := make([]byte, 2*len(units))
	for i, unit := range units {
		binary.LittleEndian.PutUint16(buf[i*2:], unit)
	}
	return base64.StdEncoding.EncodeToString(buf)
}

func (h *Handler) PXEWindowsStartnet(c echo.Context) error {
	token, attempt := c.QueryParam("token"), c.QueryParam("attempt_id")
	t, err := h.windowsTarget(c.Request().Context(), token, attempt)
	if err != nil {
		return err
	}
	if !t.active || t.started || t.applied {
		return echo.NewHTTPError(http.StatusConflict, "Windows installer is inactive or already applied")
	}
	u := appendProvisionQuery(h.resolvePXEBaseURL(c)+"/windows/install.ps1", token, attempt)
	command := "$ErrorActionPreference='Stop'; try { Invoke-WebRequest -UseBasicParsing -Uri " + psQuote(u) + " -OutFile X:\\gomi-install.ps1; & X:\\gomi-install.ps1 } catch { Write-Host 'GOMI bootstrap failed'; exit 1 }"
	body := "@echo off\r\nwpeinit\r\npowershell.exe -NoProfile -ExecutionPolicy Bypass -EncodedCommand " + powershellEncoded(command) + "\r\nif errorlevel 1 echo GOMI deployment stopped. See WinPE console for diagnostics.\r\n"
	c.Response().Header().Set("Cache-Control", "no-store")
	return c.Blob(http.StatusOK, "text/plain; charset=utf-8", []byte(body))
}

func (h *Handler) PXEWindowsInstall(c echo.Context) error {
	token, attempt := c.QueryParam("token"), c.QueryParam("attempt_id")
	t, err := h.windowsTarget(c.Request().Context(), token, attempt)
	if err != nil {
		return err
	}
	if !t.active || t.started || t.applied {
		return echo.NewHTTPError(http.StatusConflict, "Windows installer is inactive or already applied")
	}
	base := h.resolvePXEBaseURL(c)
	wimURL, err := h.artifactURL(base, t.image, t.image.Manifest.Root.Path)
	if err != nil {
		return err
	}
	config := map[string]any{
		"imageURL": wimURL, "imageIndex": t.image.Manifest.Windows.ImageIndex,
		"sha256": t.image.Manifest.Root.SHA256, "mac": t.host().PrimaryMAC(),
		"hostname": t.host().NodeDisplayName(),
		"stateURL": appendProvisionQuery(base+"/windows/state", token, attempt),
	}
	if t.deadline != nil {
		config["deadline"] = t.deadline.UTC().Format(time.RFC3339)
	}
	body, err := json.Marshal(config)
	if err != nil {
		return err
	}
	c.Response().Header().Set("Cache-Control", "no-store")
	return c.Blob(http.StatusOK, "text/plain; charset=utf-8", []byte(renderWindowsInstall(body)))
}
