package api

import (
	"errors"
	gohttp "net/http"

	"github.com/labstack/echo/v4"

	"github.com/sugaf1204/gomi/internal/cloudinit"
	"github.com/sugaf1204/gomi/internal/infra/httputil"
	"github.com/sugaf1204/gomi/internal/resource"
)

func (s *Server) CreateCloudInitTemplate(c echo.Context) error {
	var t cloudinit.CloudInitTemplate
	if err := c.Bind(&t); err != nil {
		return c.JSON(gohttp.StatusBadRequest, jsonError("invalid body"))
	}
	created, err := s.cloudInits.Create(c.Request().Context(), t)
	if err != nil {
		return c.JSON(gohttp.StatusBadRequest, jsonErrorErr(err))
	}
	httputil.CreateAudit(c, s.authStore, created.Name, "create-cloud-init-template", "success", "cloud-init template created", nil)
	return c.JSON(gohttp.StatusCreated, created)
}

func (s *Server) ListCloudInitTemplates(c echo.Context) error {
	items, err := s.cloudInits.List(c.Request().Context())
	if err != nil {
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	return c.JSON(gohttp.StatusOK, itemsResponse[cloudinit.CloudInitTemplate]{Items: items})
}

func (s *Server) GetCloudInitTemplate(c echo.Context) error {
	name := c.Param("name")
	t, err := s.cloudInits.Get(c.Request().Context(), name)
	if err != nil {
		if errors.Is(err, resource.ErrNotFound) {
			return c.JSON(gohttp.StatusNotFound, jsonError("not found"))
		}
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	return c.JSON(gohttp.StatusOK, t)
}

func (s *Server) UpdateCloudInitTemplate(c echo.Context) error {
	name := c.Param("name")
	var t cloudinit.CloudInitTemplate
	if err := c.Bind(&t); err != nil {
		return c.JSON(gohttp.StatusBadRequest, jsonError("invalid body"))
	}
	t.Name = name
	updated, err := s.cloudInits.Update(c.Request().Context(), t)
	if err != nil {
		if errors.Is(err, resource.ErrNotFound) {
			return c.JSON(gohttp.StatusNotFound, jsonError("not found"))
		}
		return c.JSON(gohttp.StatusBadRequest, jsonErrorErr(err))
	}
	httputil.CreateAudit(c, s.authStore, name, "update-cloud-init-template", "success", "cloud-init template updated", nil)
	return c.JSON(gohttp.StatusOK, updated)
}

func (s *Server) DeleteCloudInitTemplate(c echo.Context) error {
	name := c.Param("name")
	if err := s.cloudInits.Delete(c.Request().Context(), name); err != nil {
		if errors.Is(err, resource.ErrNotFound) {
			return c.JSON(gohttp.StatusNotFound, jsonError("not found"))
		}
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	httputil.CreateAudit(c, s.authStore, name, "delete-cloud-init-template", "success", "cloud-init template deleted", nil)
	return c.NoContent(gohttp.StatusNoContent)
}
