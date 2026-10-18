package api

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/sugaf1204/gomi/internal/baremetal"
	"github.com/sugaf1204/gomi/internal/infra/httputil"
)

// UnregisterBareMetalHost retires allocation enrollment, preserving the ordinary
// Machine inventory so it can be redeployed with a different role.
func (s *Server) UnregisterBareMetalHost(c echo.Context) error {
	if s.bareMetal == nil {
		return c.NoContent(http.StatusServiceUnavailable)
	}
	var req struct {
		Revision int64 `json:"revision"`
	}
	if err := c.Bind(&req); err != nil || req.Revision <= 0 {
		return c.JSON(http.StatusBadRequest, jsonError("a positive revision is required"))
	}
	name := c.Param("name")
	if _, err := s.bareMetal.Get(c.Request().Context(), name); errors.Is(err, baremetal.ErrNotFound) {
		return c.NoContent(http.StatusNoContent)
	} else if err != nil {
		return bareMetalError(c, err)
	}
	if err := s.bareMetal.Unregister(c.Request().Context(), name, req.Revision); err != nil {
		return bareMetalError(c, err)
	}
	httputil.CreateAudit(c, s.authStore, name, "unregister-bare-metal-host", "success", "unallocated host enrollment removed; machine retained", nil)
	return c.NoContent(http.StatusNoContent)
}
