package pxehttp

import (
	"github.com/labstack/echo/v4"
	gohttp "net/http"
	"os"
	"path/filepath"
	"strings"
)

func (h *Handler) PXEFile(c echo.Context) error {
	root := strings.TrimSpace(h.pxeFilesDir)
	if root == "" {
		root = strings.TrimSpace(h.pxeTFTPRoot)
	}
	if root == "" {
		return c.JSON(gohttp.StatusNotFound, jsonError("pxe file root is not configured"))
	}

	raw := strings.TrimPrefix(c.Param("*"), "/")
	rel, err := sanitizePXEPath(raw)
	if err != nil {
		return c.JSON(gohttp.StatusBadRequest, jsonError("invalid pxe asset path"))
	}
	full := filepath.Join(root, filepath.FromSlash(rel))
	if _, err := os.Stat(full); err != nil {
		if os.IsNotExist(err) {
			return c.JSON(gohttp.StatusNotFound, jsonError("pxe asset not found"))
		}
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	return h.serveProvisionedPXEFile(c, full, "server.file_transfer", "serve PXE file "+rel)
}
