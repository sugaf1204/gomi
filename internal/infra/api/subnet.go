package api

import (
	"errors"
	gohttp "net/http"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/sugaf1204/gomi/internal/infra/httputil"
	"github.com/sugaf1204/gomi/internal/resource"
	"github.com/sugaf1204/gomi/internal/subnet"
)

func (s *Server) CreateSubnet(c echo.Context) error {
	var sub subnet.Subnet
	if err := c.Bind(&sub); err != nil {
		return c.JSON(gohttp.StatusBadRequest, jsonError("invalid body"))
	}
	now := time.Now().UTC()
	sub.CreatedAt = now
	sub.UpdatedAt = now
	if err := subnet.ValidateSubnet(sub); err != nil {
		return c.JSON(gohttp.StatusBadRequest, jsonErrorErr(err))
	}
	if err := s.subnets.Upsert(c.Request().Context(), sub); err != nil {
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	httputil.CreateAudit(c, s.authStore, "", "create-subnet", "success", "subnet created: "+sub.Name, nil)
	return c.JSON(gohttp.StatusCreated, sub)
}

func (s *Server) ListSubnets(c echo.Context) error {
	subs, err := s.subnets.List(c.Request().Context())
	if err != nil {
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	return c.JSON(gohttp.StatusOK, itemsResponse[subnet.Subnet]{Items: subs})
}

func (s *Server) GetSubnet(c echo.Context) error {
	name := c.Param("name")
	sub, err := s.subnets.Get(c.Request().Context(), name)
	if err != nil {
		if errors.Is(err, resource.ErrNotFound) {
			return c.JSON(gohttp.StatusNotFound, jsonError("not found"))
		}
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	return c.JSON(gohttp.StatusOK, sub)
}

func (s *Server) UpdateSubnet(c echo.Context) error {
	name := c.Param("name")
	existing, err := s.subnets.Get(c.Request().Context(), name)
	if err != nil {
		if errors.Is(err, resource.ErrNotFound) {
			return c.JSON(gohttp.StatusNotFound, jsonError("not found"))
		}
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	var sub subnet.Subnet
	if err := c.Bind(&sub); err != nil {
		return c.JSON(gohttp.StatusBadRequest, jsonError("invalid body"))
	}
	sub.Name = name
	sub.CreatedAt = existing.CreatedAt
	sub.UpdatedAt = time.Now().UTC()
	if err := subnet.ValidateSubnet(sub); err != nil {
		return c.JSON(gohttp.StatusBadRequest, jsonErrorErr(err))
	}
	if err := s.subnets.Upsert(c.Request().Context(), sub); err != nil {
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	httputil.CreateAudit(c, s.authStore, "", "update-subnet", "success", "subnet updated: "+name, nil)
	return c.JSON(gohttp.StatusOK, sub)
}

func (s *Server) DeleteSubnet(c echo.Context) error {
	name := c.Param("name")
	if err := s.subnets.Delete(c.Request().Context(), name); err != nil {
		if errors.Is(err, resource.ErrNotFound) {
			return c.JSON(gohttp.StatusNotFound, jsonError("not found"))
		}
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	httputil.CreateAudit(c, s.authStore, "", "delete-subnet", "success", "subnet deleted: "+name, nil)
	return c.NoContent(gohttp.StatusNoContent)
}
