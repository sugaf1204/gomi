package api

import (
	"errors"
	gohttp "net/http"

	"github.com/labstack/echo/v4"

	"github.com/sugaf1204/gomi/internal/hwinfo"
	"github.com/sugaf1204/gomi/internal/resource"
)

func (s *Server) GetHardwareInfo(c echo.Context) error {
	name := c.Param("name")
	info, err := s.hwinfo.Get(c.Request().Context(), name)
	if err != nil {
		if errors.Is(err, resource.ErrNotFound) {
			return c.JSON(gohttp.StatusNotFound, map[string]string{"error": "no hardware info"})
		}
		return c.JSON(gohttp.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(gohttp.StatusOK, info)
}

func (s *Server) ReportHardwareInfo(c echo.Context) error {
	name := c.Param("name")
	var info hwinfo.HardwareInfo
	if err := c.Bind(&info); err != nil {
		return c.JSON(gohttp.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	info.Name = name + "-hwinfo"
	info.MachineName = name
	created, err := s.hwinfo.Upsert(c.Request().Context(), info)
	if err != nil {
		return c.JSON(gohttp.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(gohttp.StatusOK, created)
}
