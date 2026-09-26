package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/sugaf1204/gomi/internal/baremetal"
	"github.com/sugaf1204/gomi/internal/infra/httputil"
	"github.com/sugaf1204/gomi/internal/machine"
)

func (s *Server) DeployBareMetalClaim(c echo.Context) error {
	if s.bareMetal == nil {
		return c.NoContent(http.StatusServiceUnavailable)
	}
	ctx := c.Request().Context()
	h, err := s.bareMetal.FindOwner(ctx, c.Param("owner"))
	if err != nil {
		return bareMetalError(c, err)
	}
	var req struct {
		OSImageRef string          `json:"osImageRef"`
		Envelope   json.RawMessage `json:"envelope"`
	}
	// Ciphertext can be a few MiB; bound decoding before allocating a payload.
	c.Request().Body = http.MaxBytesReader(c.Response(), c.Request().Body, 6<<20)
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, jsonError("invalid body"))
	}
	if err := baremetal.ValidateEnvelope(req.Envelope, h); err != nil {
		return c.JSON(http.StatusBadRequest, jsonErrorErr(err))
	}
	m, err := s.machines.Get(ctx, h.Name)
	if err != nil {
		return bareMetalError(c, err)
	}
	if h.State != baremetal.Claimed {
		if (h.State == baremetal.Deploying || h.State == baremetal.Ready) && m.SealedBootstrap != nil && m.SealedBootstrap.Owner == h.Owner && bytes.Equal(m.SealedBootstrap.Envelope, req.Envelope) && m.OSPreset.ImageRef == req.OSImageRef {
			return c.JSON(http.StatusAccepted, h)
		}
		return bareMetalError(c, baremetal.ErrConflict)
	}
	if s.osimages == nil {
		return c.NoContent(http.StatusServiceUnavailable)
	}
	img, err := s.osimages.Get(ctx, req.OSImageRef)
	if err != nil {
		return c.JSON(http.StatusBadRequest, jsonError("OS image not found"))
	}
	if err := baremetal.ValidateImage(m, img); err != nil {
		return c.JSON(http.StatusBadRequest, jsonErrorErr(err))
	}
	original := m
	m.TargetDisk = h.TargetDisk
	m.OSPreset = machine.OSPreset{Family: machine.OSType(img.OSFamily), Version: img.OSVersion, ImageRef: img.Name}
	m.CloudInitRef = ""
	m.CloudInitRefs = nil
	m.LastDeployedCloudInitRef = ""
	fingerprint, _ := baremetal.EnrollmentFingerprint(h.PublicKey)
	m.SealedBootstrap = &machine.SealedBootstrap{Owner: h.Owner, KeyFingerprint: fingerprint, Envelope: req.Envelope}
	actor, _ := httputil.UserFromContext(c)
	m, err = machine.PrepareReinstall(m, actor.Username, nil, s.provisionTimeout)
	if err != nil {
		return c.JSON(http.StatusBadRequest, jsonErrorErr(err))
	}
	h, err = s.bareMetal.CommitDeployment(ctx, h, m)
	if err != nil {
		return bareMetalError(c, err)
	}
	httputil.CreateAudit(c, s.authStore, h.Name, "deploy-bare-metal-claim", "success", "sealed deployment committed", map[string]string{"owner": h.Owner, "attemptID": h.AttemptID})
	s.startRedeployPowerCycle(original, m, original.IP)
	return c.JSON(http.StatusAccepted, h)
}

func (s *Server) DeleteBareMetalClaim(c echo.Context) error {
	if s.bareMetal == nil {
		return c.NoContent(http.StatusServiceUnavailable)
	}
	ctx := c.Request().Context()
	h, err := s.bareMetal.FindOwner(ctx, c.Param("owner"))
	if errors.Is(err, baremetal.ErrNotFound) {
		return c.NoContent(http.StatusNoContent)
	}
	if err != nil {
		return bareMetalError(c, err)
	}
	if h.State == baremetal.Releasing {
		return c.JSON(http.StatusAccepted, h)
	}
	if h.State == baremetal.Claimed {
		h, err = s.bareMetal.Transition(ctx, h, baremetal.Releasing, "")
		if err == nil {
			_, err = s.bareMetal.CompleteRelease(ctx, h)
		}
		if err != nil {
			return bareMetalError(c, err)
		}
		return c.NoContent(http.StatusNoContent)
	}
	m, err := s.machines.Get(ctx, h.Name)
	if err != nil {
		return bareMetalError(c, err)
	}
	// Even a failed or timed-out installer may hold the only identity copy in
	// RAM. Require proof of disk restoration before any cleanup power cycle.
	if m.Provision == nil || m.Provision.Artifacts["imageApplied"] != "true" {
		if m.Provision != nil && m.Provision.Active {
			return c.JSON(http.StatusAccepted, h)
		}
		return c.JSON(http.StatusConflict, jsonError("disk identity restoration is unconfirmed; recover the installer before rebooting"))
	}
	// Once curtin restored the identity, cancellation may clean up a failed or
	// stalled guest bootstrap. If cleanup itself failed, do not create an
	// automatic destructive retry loop; only explicit admin recovery may retry.
	if m.SealedBootstrap != nil && m.SealedBootstrap.Cleanup && (h.State == baremetal.Failed || m.Phase == machine.PhaseError) {
		return c.JSON(http.StatusConflict, jsonError("failed physical cleanup requires explicit recovery"))
	}
	original := m
	m.TargetDisk = h.TargetDisk
	fingerprint, _ := baremetal.EnrollmentFingerprint(h.PublicKey)
	m.SealedBootstrap = &machine.SealedBootstrap{Owner: h.Owner, KeyFingerprint: fingerprint, Cleanup: true}
	m.CloudInitRef = ""
	m.CloudInitRefs = nil
	m.LastDeployedCloudInitRef = ""
	actor, _ := httputil.UserFromContext(c)
	m, err = machine.PrepareReinstall(m, actor.Username, nil, s.provisionTimeout)
	if err != nil {
		return c.JSON(http.StatusBadRequest, jsonErrorErr(err))
	}
	h, err = s.bareMetal.CommitDeployment(ctx, h, m)
	if err != nil {
		return bareMetalError(c, err)
	}
	httputil.CreateAudit(c, s.authStore, h.Name, "release-bare-metal-claim", "success", "OS reset started; host remains allocated until completion", map[string]string{"owner": h.Owner})
	s.startRedeployPowerCycle(original, m, original.IP)
	return c.JSON(http.StatusAccepted, h)
}
