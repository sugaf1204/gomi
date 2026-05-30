package api

import (
	gohttp "net/http"
	"strings"

	"github.com/labstack/echo/v4"
)

func (s *Server) ListAuditEvents(c echo.Context) error {
	machineName := strings.TrimSpace(c.QueryParam("machine"))
	start, size, err := parsePageRequest(c)
	if err != nil {
		return c.JSON(gohttp.StatusBadRequest, jsonErrorErr(err))
	}
	events, total, err := s.authStore.ListAuditEventsPage(c.Request().Context(), machineName, start, size)
	if err != nil {
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	next := ""
	if start+len(events) < total {
		next = encodePageToken(start + len(events))
	}
	return c.JSON(gohttp.StatusOK, ListAuditEventsResponse{
		AuditEvents:   events,
		NextPageToken: next,
		TotalSize:     total,
	})
}
