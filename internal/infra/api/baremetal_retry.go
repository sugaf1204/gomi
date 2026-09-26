package api

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/sugaf1204/gomi/internal/baremetal"
	"github.com/sugaf1204/gomi/internal/infra/httputil"
	"github.com/sugaf1204/gomi/internal/machine"
)

// RetryBareMetalClaim is an explicit administrator recovery operation. It never
// releases ownership or changes the sealed bootstrap, disk or enrolled identity.
// A failed installer can still hold the only identity copy in RAM. Require
// authenticated disk-restoration evidence before retrying, even after timeout.
func (s *Server) RetryBareMetalClaim(c echo.Context) error {
	if s.bareMetal == nil || s.osimages == nil {
		return c.NoContent(http.StatusServiceUnavailable)
	}
	var req struct {
		AttemptID string `json:"attemptID"`
	}
	if err := c.Bind(&req); err != nil || req.AttemptID == "" {
		return c.JSON(http.StatusBadRequest, jsonError("the failed attemptID is required"))
	}
	ctx := c.Request().Context()
	h, err := s.bareMetal.FindOwner(ctx, c.Param("owner"))
	if err != nil {
		return bareMetalError(c, err)
	}
	m, err := s.machines.Get(ctx, h.Name)
	if err != nil {
		return bareMetalError(c, err)
	}
	if h.AttemptID != req.AttemptID || m.Provision == nil ||
		m.Provision.AttemptID != req.AttemptID ||
		m.SealedBootstrap == nil || m.SealedBootstrap.Owner != h.Owner {
		return bareMetalError(c, baremetal.ErrConflict)
	}
	failed := h.State == baremetal.Failed && !m.Provision.Active && m.Phase == machine.PhaseError
	// image_applied is emitted only after curtin's late commands restore the
	// enrollment identity. An administrator may then retry a broken guest
	// bootstrap without waiting for the full provisioning timeout.
	postInstall := h.State == baremetal.Deploying && m.Provision.Active &&
		m.Phase == machine.PhaseProvisioning && m.Provision.Artifacts["imageApplied"] == "true"
	if (!failed && !postInstall) || m.Provision.Artifacts["imageApplied"] != "true" {
		return bareMetalError(c, baremetal.ErrConflict)
	}
	img, err := s.osimages.Get(ctx, m.OSPreset.ImageRef)
	if err != nil {
		return c.JSON(http.StatusBadRequest, jsonError("OS image not found"))
	}
	if err := baremetal.ValidateImage(m, img); err != nil {
		return c.JSON(http.StatusBadRequest, jsonErrorErr(err))
	}
	original := m
	actor, _ := httputil.UserFromContext(c)
	m, err = machine.PrepareReinstall(m, actor.Username, nil, s.provisionTimeout)
	if err != nil {
		return c.JSON(http.StatusBadRequest, jsonErrorErr(err))
	}
	h, err = s.bareMetal.CommitDeployment(ctx, h, m)
	if err != nil {
		return bareMetalError(c, err)
	}
	httputil.CreateAudit(c, s.authStore, h.Name, "retry-bare-metal-claim", "success", "administrator retried a failed sealed deployment", map[string]string{"owner": h.Owner, "previousAttemptID": req.AttemptID, "attemptID": h.AttemptID})
	s.startRedeployPowerCycle(original, m, original.IP)
	return c.JSON(http.StatusAccepted, h)
}
