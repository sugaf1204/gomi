package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/sugaf1204/gomi/internal/baremetal"
	"github.com/sugaf1204/gomi/internal/infra/httputil"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/resource"
)

func (s *Server) RegisterBareMetalHost(c echo.Context) error {
	if s.bareMetal == nil {
		return c.NoContent(http.StatusServiceUnavailable)
	}
	var req struct {
		Pool       string `json:"pool"`
		PublicKey  string `json:"publicKey"`
		TargetDisk string `json:"targetDisk"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, jsonError("invalid body"))
	}
	if _, err := baremetal.EnrollmentFingerprint(req.PublicKey); err != nil {
		return c.JSON(http.StatusBadRequest, jsonErrorErr(err))
	}
	if !machine.IsWholeDiskPath(req.TargetDisk) {
		return c.JSON(http.StatusBadRequest, jsonError("targetDisk must identify the installation disk"))
	}
	name := c.Param("name")
	if err := baremetal.ValidateRegistration(name, req.Pool); err != nil {
		return c.JSON(http.StatusBadRequest, jsonErrorErr(err))
	}
	if existing, e := s.bareMetal.Get(c.Request().Context(), name); e == nil {
		if existing.Pool != req.Pool || existing.PublicKey != req.PublicKey || existing.TargetDisk != req.TargetDisk {
			return bareMetalError(c, baremetal.ErrConflict)
		}
		return c.JSON(http.StatusOK, existing)
	} else if !errors.Is(e, baremetal.ErrNotFound) {
		return bareMetalError(c, e)
	}
	m, err := s.machines.Get(c.Request().Context(), name)
	if errors.Is(err, resource.ErrNotFound) {
		return c.JSON(http.StatusNotFound, jsonError("machine not found"))
	}
	if err != nil {
		return c.JSON(http.StatusInternalServerError, jsonError("cannot read machine"))
	}
	if err := baremetal.ValidateHost(m); err != nil {
		return c.JSON(http.StatusBadRequest, jsonErrorErr(err))
	}
	if m.Provision != nil && m.Provision.Active {
		return c.JSON(http.StatusConflict, jsonError("machine is provisioning"))
	}
	h, err := s.bareMetal.Register(c.Request().Context(), name, req.Pool, req.PublicKey, req.TargetDisk)
	if err != nil {
		return bareMetalError(c, err)
	}
	httputil.CreateAudit(c, s.authStore, name, "register-bare-metal-host", "success", "host enrolled in pool", map[string]string{"pool": req.Pool})
	return c.JSON(http.StatusOK, h)
}
func (s *Server) GetBareMetalHost(c echo.Context) error {
	if s.bareMetal == nil {
		return c.NoContent(http.StatusServiceUnavailable)
	}
	h, err := s.bareMetal.Get(c.Request().Context(), c.Param("name"))
	if err != nil {
		return bareMetalError(c, err)
	}
	return c.JSON(http.StatusOK, h)
}
func (s *Server) GetBareMetalClaim(c echo.Context) error {
	if s.bareMetal == nil {
		return c.NoContent(http.StatusServiceUnavailable)
	}
	h, err := s.bareMetal.FindOwner(c.Request().Context(), c.Param("owner"))
	if err != nil {
		return bareMetalError(c, err)
	}
	if h.State == baremetal.Deploying || h.State == baremetal.Releasing {
		m, e := s.machines.Get(c.Request().Context(), h.Name)
		if e != nil {
			return bareMetalError(c, e)
		}
		if m.Provision == nil || m.Provision.AttemptID != h.AttemptID {
			return bareMetalError(c, baremetal.ErrConflict)
		}
		if !m.Provision.Active && m.Provision.CompletedAt != nil && m.Phase == machine.PhaseReady {
			if h.State == baremetal.Releasing {
				_, e = s.bareMetal.CompleteRelease(c.Request().Context(), h)
				if e != nil {
					return bareMetalError(c, e)
				}
				return c.NoContent(http.StatusNotFound)
			}
			h, e = s.bareMetal.Transition(c.Request().Context(), h, baremetal.Ready, h.AttemptID)
			if e != nil {
				return bareMetalError(c, e)
			}
		} else if m.Phase == machine.PhaseError {
			h, e = s.bareMetal.Transition(c.Request().Context(), h, baremetal.Failed, h.AttemptID)
			if e != nil {
				return bareMetalError(c, e)
			}
		}
	}
	if m, e := s.machines.Get(c.Request().Context(), h.Name); e == nil {
		h.IPAddress = m.IP
	} else {
		return bareMetalError(c, e)
	}
	return c.JSON(http.StatusOK, h)
}
func (s *Server) AcquireBareMetalHost(c echo.Context) error {
	if s.bareMetal == nil {
		return c.NoContent(http.StatusServiceUnavailable)
	}
	var req struct {
		Pool  string `json:"pool"`
		Owner string `json:"owner"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, jsonError("invalid body"))
	}
	if err := baremetal.ValidateAcquire(req.Pool, req.Owner); err != nil {
		return c.JSON(http.StatusBadRequest, jsonErrorErr(err))
	}
	h, err := s.bareMetal.Acquire(c.Request().Context(), req.Pool, req.Owner)
	if err != nil {
		return bareMetalError(c, err)
	}
	httputil.CreateAudit(c, s.authStore, h.Name, "claim-bare-metal-host", "success", "host allocated", map[string]string{"owner": req.Owner})
	return c.JSON(http.StatusOK, h)
}
func bareMetalError(c echo.Context, err error) error {
	switch {
	case errors.Is(err, baremetal.ErrNotFound):
		return c.JSON(http.StatusNotFound, jsonErrorErr(err))
	case errors.Is(err, baremetal.ErrConflict), errors.Is(err, baremetal.ErrCapacity):
		return c.JSON(http.StatusConflict, jsonErrorErr(err))
	default:
		return c.JSON(http.StatusInternalServerError, jsonError("bare-metal storage operation failed"))
	}
}

// Ordinary UI/API writes must not race a CAPI-owned deployment. The claim
// workflow operates under its own owner/revision fence instead of this route.
func (s *Server) protectAllocatedMachine(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if s.bareMetal == nil {
			return next(c)
		}
		name := c.Param("name")
		if name == "" {
			name, _, _ = strings.Cut(c.Param("*"), ":")
		}
		_, err := s.bareMetal.Get(c.Request().Context(), name)
		if errors.Is(err, baremetal.ErrNotFound) {
			return next(c)
		}
		if err != nil {
			return bareMetalError(c, err)
		}
		return c.JSON(http.StatusConflict, jsonError("machine is enrolled in bare-metal management; use its ownership workflow"))
	}
}
