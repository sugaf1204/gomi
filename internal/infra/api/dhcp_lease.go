package api

import (
	gohttp "net/http"

	"github.com/labstack/echo/v4"

	"github.com/sugaf1204/gomi/internal/pxe"
)

func (s *Server) ListDHCPLeases(c echo.Context) error {
	if s.leaseStore == nil {
		return c.JSON(gohttp.StatusOK, itemsResponse[any]{Items: []any{}})
	}

	leases, err := s.leaseStore.List(c.Request().Context())
	if err != nil {
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}

	return c.JSON(gohttp.StatusOK, itemsResponse[pxe.DHCPLease]{Items: leases})
}
