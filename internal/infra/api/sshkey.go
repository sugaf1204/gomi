package api

import (
	"errors"
	gohttp "net/http"

	"github.com/labstack/echo/v4"

	"github.com/sugaf1204/gomi/internal/infra/httputil"
	"github.com/sugaf1204/gomi/internal/resource"
	"github.com/sugaf1204/gomi/internal/sshkey"
)

func (s *Server) ListSSHKeys(c echo.Context) error {
	keys, err := s.sshkeys.List(c.Request().Context())
	if err != nil {
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	// Sanitize: never return private key material in list responses.
	for i := range keys {
		keys[i].PrivateKey = ""
	}
	return c.JSON(gohttp.StatusOK, itemsResponse[sshkey.SSHKey]{Items: keys})
}

func (s *Server) GetSSHKey(c echo.Context) error {
	name := c.Param("name")
	k, err := s.sshkeys.Get(c.Request().Context(), name)
	if err != nil {
		if errors.Is(err, resource.ErrNotFound) {
			return c.JSON(gohttp.StatusNotFound, jsonError("not found"))
		}
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	// Sanitize: never return private key material in get responses.
	k.PrivateKey = ""
	return c.JSON(gohttp.StatusOK, k)
}

func (s *Server) CreateSSHKey(c echo.Context) error {
	var k sshkey.SSHKey
	if err := c.Bind(&k); err != nil {
		return c.JSON(gohttp.StatusBadRequest, jsonError("invalid body"))
	}
	created, err := s.sshkeys.Create(c.Request().Context(), k)
	if err != nil {
		return c.JSON(gohttp.StatusBadRequest, jsonErrorErr(err))
	}
	httputil.CreateAudit(c, s.authStore, "", "create-ssh-key", "success", "ssh key created: "+created.Name, nil)
	return c.JSON(gohttp.StatusCreated, created)
}

func (s *Server) DeleteSSHKey(c echo.Context) error {
	name := c.Param("name")
	if err := s.sshkeys.Delete(c.Request().Context(), name); err != nil {
		if errors.Is(err, resource.ErrNotFound) {
			return c.JSON(gohttp.StatusNotFound, jsonError("not found"))
		}
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	httputil.CreateAudit(c, s.authStore, "", "delete-ssh-key", "success", "ssh key deleted: "+name, nil)
	return c.NoContent(gohttp.StatusNoContent)
}
