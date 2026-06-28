package api

import (
	"context"
	"errors"
	"fmt"
	gohttp "net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/sugaf1204/gomi/internal/infra/httputil"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/osimage"
	"github.com/sugaf1204/gomi/internal/power"
	"github.com/sugaf1204/gomi/internal/resource"
)

type redeployReq struct {
	Confirm  string             `json:"confirm,omitempty"`
	Hostname *string            `json:"hostname,omitempty"`
	MAC      *string            `json:"mac,omitempty"`
	Arch     *string            `json:"arch,omitempty"`
	Firmware *string            `json:"firmware,omitempty"`
	Power    *power.PowerConfig `json:"power,omitempty"`
	OSPreset *struct {
		ImageRef string `json:"imageRef,omitempty"`
	} `json:"osPreset,omitempty"`
	TargetDisk *string `json:"targetDisk,omitempty"`
	Network    *struct {
		Domain string `json:"domain,omitempty"`
	} `json:"network,omitempty"`
	CloudInitRef  *string                `json:"cloudInitRef,omitempty"`
	CloudInitRefs *[]string              `json:"cloudInitRefs,omitempty"`
	SubnetRef     *string                `json:"subnetRef,omitempty"`
	IPAssignment  *string                `json:"ipAssignment,omitempty"`
	IP            *string                `json:"ip,omitempty"`
	Role          *string                `json:"role,omitempty"`
	BridgeName    *string                `json:"bridgeName,omitempty"`
	SSHKeyRefs    *[]string              `json:"sshKeyRefs,omitempty"`
	LoginUser     *machine.LoginUserSpec `json:"loginUser,omitempty"`
}

func (s *Server) RedeployMachine(c echo.Context) error {
	name := c.Param("name")
	user, _ := httputil.UserFromContext(c)
	ctx := c.Request().Context()

	var opts *machine.ReinstallOptions
	var req redeployReq
	if c.Request().ContentLength > 0 {
		if err := c.Bind(&req); err != nil {
			return c.JSON(gohttp.StatusBadRequest, jsonError("invalid body"))
		}
	}
	normalizeMachineRedeployRequestRefs(&req)
	if strings.TrimSpace(req.Confirm) != "" && strings.TrimSpace(req.Confirm) != name {
		return c.JSON(gohttp.StatusBadRequest, jsonError("confirm must match machine name"))
	}

	current, err := s.machines.Get(ctx, name)
	if err != nil {
		if errors.Is(err, resource.ErrNotFound) {
			return c.JSON(gohttp.StatusNotFound, jsonError("not found"))
		}
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	powerCycleFallbackIP := current.IP

	if req.Hostname != nil || req.MAC != nil || req.Arch != nil || req.Firmware != nil || req.Power != nil || req.OSPreset != nil || req.TargetDisk != nil || req.Network != nil || req.CloudInitRef != nil || req.CloudInitRefs != nil || req.SubnetRef != nil || req.IPAssignment != nil || req.IP != nil || req.Role != nil || req.BridgeName != nil || req.SSHKeyRefs != nil || req.LoginUser != nil {
		var powerCfg *power.PowerConfig
		if req.Power != nil {
			copyCfg := mergePowerConfigDefaults(current.Power, *req.Power)
			if err := power.FillWoLDefaults(&copyCfg); err != nil {
				return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
			}
			if err := power.ValidatePowerConfig(copyCfg); err != nil {
				return c.JSON(gohttp.StatusBadRequest, jsonErrorErr(err))
			}
			powerCfg = &copyCfg
		}

		nextPreset := current.OSPreset
		if req.OSPreset != nil && strings.TrimSpace(req.OSPreset.ImageRef) != "" {
			nextPreset.ImageRef = strings.TrimSpace(req.OSPreset.ImageRef)
			draft := current
			draft.OSPreset = nextPreset
			if err := s.resolveOSPreset(ctx, &draft); err != nil {
				return c.JSON(gohttp.StatusBadRequest, jsonErrorErr(err))
			}
			nextPreset = draft.OSPreset
		}

		var networkCfg *machine.NetworkConfig
		if req.Network != nil {
			networkCfg = &machine.NetworkConfig{Domain: req.Network.Domain}
		}

		var subnetRef *string
		if req.SubnetRef != nil {
			trimmed := strings.TrimSpace(*req.SubnetRef)
			if trimmed != "" {
				if _, err := s.subnets.Get(ctx, trimmed); err != nil {
					return c.JSON(gohttp.StatusBadRequest, jsonError("referenced subnetRef not found"))
				}
			}
			subnetRef = &trimmed
		}

		var ipAssignment *machine.IPAssignmentMode
		if req.IPAssignment != nil {
			mode := machine.IPAssignmentMode(strings.TrimSpace(*req.IPAssignment))
			if mode != "" && mode != machine.IPAssignmentModeDHCP && mode != machine.IPAssignmentModeStatic {
				return c.JSON(gohttp.StatusBadRequest, jsonError("ipAssignment must be 'dhcp' or 'static'"))
			}
			ipAssignment = &mode
		}

		var firmware *machine.Firmware
		if req.Firmware != nil {
			value := machine.Firmware(strings.TrimSpace(*req.Firmware))
			firmware = &value
		}

		var role *machine.Role
		if req.Role != nil {
			value := machine.Role(strings.TrimSpace(*req.Role))
			if value != machine.RoleDefault && value != machine.RoleHypervisor {
				return c.JSON(gohttp.StatusBadRequest, jsonError("role must be '' or 'hypervisor'"))
			}
			if value == machine.RoleHypervisor && current.Role != machine.RoleHypervisor {
				if _, err := s.hypervisors.Get(ctx, name); err == nil {
					return c.JSON(gohttp.StatusConflict, jsonError(fmt.Sprintf("hypervisor %q already exists", name)))
				} else if !errors.Is(err, resource.ErrNotFound) {
					return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
				}
			}
			role = &value
		}

		var bridgeName *string
		if req.BridgeName != nil || (role != nil && *role == machine.RoleHypervisor) {
			trimmed := ""
			if req.BridgeName != nil {
				trimmed = strings.TrimSpace(*req.BridgeName)
			}
			if role != nil && *role == machine.RoleHypervisor && trimmed == "" {
				trimmed = "br0"
			}
			bridgeName = &trimmed
		}

		opts = &machine.ReinstallOptions{
			Hostname:      req.Hostname,
			MAC:           req.MAC,
			Arch:          req.Arch,
			Firmware:      firmware,
			Power:         powerCfg,
			OSPreset:      &nextPreset,
			TargetDisk:    req.TargetDisk,
			Network:       networkCfg,
			CloudInitRef:  req.CloudInitRef,
			CloudInitRefs: req.CloudInitRefs,
			SubnetRef:     subnetRef,
			IPAssignment:  ipAssignment,
			IP:            req.IP,
			Role:          role,
			BridgeName:    bridgeName,
			SSHKeyRefs:    req.SSHKeyRefs,
			LoginUser:     req.LoginUser,
		}
	}

	m, err := s.machines.Reinstall(ctx, name, user.Username, opts)
	if err != nil {
		if errors.Is(err, resource.ErrNotFound) {
			return c.JSON(gohttp.StatusNotFound, jsonError("not found"))
		}
		httputil.CreateAudit(c, s.authStore, name, "redeploy", "failure", err.Error(), nil)
		return c.JSON(gohttp.StatusBadRequest, jsonErrorErr(err))
	}
	if changed, err := s.attachHypervisorRegistrationToken(ctx, &m); err != nil {
		httputil.CreateAudit(c, s.authStore, name, "redeploy", "failure", err.Error(), nil)
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	} else if changed {
		if err := s.machines.Store().Upsert(ctx, m); err != nil {
			httputil.CreateAudit(c, s.authStore, name, "redeploy", "failure", err.Error(), nil)
			return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
		}
	}
	httputil.CreateAudit(c, s.authStore, name, "redeploy", "success", "redeploy started", nil)
	s.startRedeployPowerCycle(current, m, powerCycleFallbackIP)
	return c.JSON(gohttp.StatusAccepted, machineResponse(m))
}

func normalizeMachineRedeployRequestRefs(req *redeployReq) {
	if req == nil {
		return
	}
	if req.OSPreset != nil {
		req.OSPreset.ImageRef = resourceID("osImages", req.OSPreset.ImageRef)
	}
	if req.CloudInitRef != nil {
		v := resourceID("cloudInitTemplates", *req.CloudInitRef)
		req.CloudInitRef = &v
	}
	if req.CloudInitRefs != nil {
		v := resourceIDs("cloudInitTemplates", *req.CloudInitRefs)
		req.CloudInitRefs = &v
	}
	if req.SubnetRef != nil {
		v := resourceID("subnets", *req.SubnetRef)
		req.SubnetRef = &v
	}
	if req.SSHKeyRefs != nil {
		v := resourceIDs("sshKeys", *req.SSHKeyRefs)
		req.SSHKeyRefs = &v
	}
}

func (s *Server) attachHypervisorRegistrationToken(ctx context.Context, m *machine.Machine) (bool, error) {
	if m == nil || m.Role != machine.RoleHypervisor || m.Provision == nil || s.hypervisors == nil {
		return false, nil
	}
	token, err := s.hypervisors.CreateToken(ctx)
	if err != nil {
		return false, fmt.Errorf("create hypervisor registration token: %w", err)
	}
	if m.Provision.Artifacts == nil {
		m.Provision.Artifacts = map[string]string{}
	}
	m.Provision.Artifacts[machine.ProvisionArtifactHypervisorRegistrationToken] = token.Token
	m.Provision.Artifacts[machine.ProvisionArtifactHypervisorRegistrationTokenExpiresAt] = token.ExpiresAt.Format(time.RFC3339)
	return true, nil
}

func (s *Server) resolveOSPreset(ctx context.Context, m *machine.Machine) error {
	ref := strings.TrimSpace(m.OSPreset.ImageRef)
	if ref == "" {
		return nil
	}
	if s.osimages == nil {
		return fmt.Errorf("os image service not available")
	}
	img, err := s.osimages.Get(ctx, ref)
	if err != nil {
		if errors.Is(err, resource.ErrNotFound) {
			return fmt.Errorf("referenced os image not found: %s", ref)
		}
		return err
	}
	m.OSPreset.Family = machine.OSType(img.OSFamily)
	m.OSPreset.Version = img.OSVersion
	if !osimage.SupportsDeploymentTarget(img, osimage.DeploymentTargetBareMetal) {
		return fmt.Errorf("referenced os image %s does not support bare-metal deployment", ref)
	}
	return nil
}
