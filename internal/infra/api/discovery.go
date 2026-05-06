package api

import (
	gohttp "net/http"

	"github.com/labstack/echo/v4"
)

type discoverRequest struct {
	MAC            string `json:"mac"`
	ClientHostname string `json:"clientHostname"`
	Arch           string `json:"arch"`
	Firmware       string `json:"firmware"`
}

func (s *Server) DiscoverMachine(c echo.Context) error {
	var req discoverRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(gohttp.StatusBadRequest, jsonError("invalid body"))
	}
	if req.MAC == "" {
		return c.JSON(gohttp.StatusBadRequest, jsonError("mac is required"))
	}
	m, err := s.discovery.HandlePXEBoot(c.Request().Context(), req.MAC, req.ClientHostname, req.Arch, req.Firmware)
	if err != nil {
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	return c.JSON(gohttp.StatusOK, m)
}
