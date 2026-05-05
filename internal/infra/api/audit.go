package api

import (
	gohttp "net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
)

func (s *Server) ListAuditEvents(c echo.Context) error {
	machineName := strings.TrimSpace(c.QueryParam("machine"))
	limit := 50
	if raw := strings.TrimSpace(c.QueryParam("limit")); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 && v <= 200 {
			limit = v
		}
	}
	events, err := s.authStore.ListAuditEvents(c.Request().Context(), machineName, limit)
	if err != nil {
		return c.JSON(gohttp.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(gohttp.StatusOK, map[string]any{"items": events})
}
