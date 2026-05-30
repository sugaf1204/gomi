package api

import (
	gohttp "net/http"
	"strings"

	"github.com/labstack/echo/v4"
)

func (s *Server) ListAuditEvents(c echo.Context) error {
	machineName := strings.TrimSpace(c.QueryParam("machine"))
	events, err := s.authStore.ListAuditEvents(c.Request().Context(), machineName, 0)
	if err != nil {
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	p, err := parsePagination(c, len(events))
	if err != nil {
		return c.JSON(gohttp.StatusBadRequest, jsonErrorErr(err))
	}
	return c.JSON(gohttp.StatusOK, ListAuditEventsResponse{
		AuditEvents:   paginate(events, p),
		NextPageToken: p.nextPageToken,
		TotalSize:     p.totalSize,
	})
}
