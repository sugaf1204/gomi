package api

import (
	gohttp "net/http"

	"github.com/labstack/echo/v4"
)

func (s *Server) ListDHCPLeases(c echo.Context) error {
	if s.leaseStore == nil {
		return c.JSON(gohttp.StatusOK, map[string]any{"items": []any{}})
	}

	leases, err := s.leaseStore.List(c.Request().Context())
	if err != nil {
		return c.JSON(gohttp.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	return c.JSON(gohttp.StatusOK, map[string]any{"items": leases})
}
