package api

import (
	gohttp "net/http"

	"github.com/labstack/echo/v4"

	"github.com/sugaf1204/gomi/internal/pxe"
)

func (s *Server) ListDHCPLeases(c echo.Context) error {
	if s.leaseStore == nil {
		return c.JSON(gohttp.StatusOK, ListDHCPLeasesResponse{DHCPLeases: []pxe.DHCPLease{}, TotalSize: 0})
	}

	leases, err := s.leaseStore.List(c.Request().Context())
	if err != nil {
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}

	p, err := parsePagination(c, len(leases))
	if err != nil {
		return c.JSON(gohttp.StatusBadRequest, jsonErrorErr(err))
	}
	return c.JSON(gohttp.StatusOK, ListDHCPLeasesResponse{
		DHCPLeases:    paginate(leases, p),
		NextPageToken: p.nextPageToken,
		TotalSize:     p.totalSize,
	})
}
