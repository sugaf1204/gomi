package api

import (
	"errors"
	gohttp "net/http"
	"os"

	"github.com/labstack/echo/v4"

	"github.com/sugaf1204/gomi/internal/bootenv"
	"github.com/sugaf1204/gomi/internal/resource"
)

func (s *Server) ListBootEnvironments(c echo.Context) error {
	if s.bootenvs == nil {
		return c.JSON(gohttp.StatusOK, itemsResponse[bootenv.Status]{Items: []bootenv.Status{}})
	}
	return c.JSON(gohttp.StatusOK, itemsResponse[bootenv.Status]{Items: s.bootenvs.List()})
}

func (s *Server) RebuildBootEnvironment(c echo.Context) error {
	if s.bootenvs == nil {
		return c.JSON(gohttp.StatusServiceUnavailable, jsonError("boot environment manager is not configured"))
	}
	return c.JSON(gohttp.StatusAccepted, s.bootenvs.StartRebuild(c.Param("name")))
}

func (s *Server) GetBootEnvironmentLogs(c echo.Context) error {
	if s.bootenvs == nil {
		return c.JSON(gohttp.StatusNotFound, jsonError("boot environment manager is not configured"))
	}
	st := s.bootenvs.Status(c.Param("name"))
	if st.LogPath == "" {
		return c.JSON(gohttp.StatusNotFound, jsonError("build log not found"))
	}
	if _, err := os.Stat(st.LogPath); err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, resource.ErrNotFound) {
			return c.JSON(gohttp.StatusNotFound, jsonError("build log not found"))
		}
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	return c.File(st.LogPath)
}
