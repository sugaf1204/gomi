package api

import (
	"fmt"
	gohttp "net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/labstack/echo/v4"
)

// ServeFile serves static files from the configured files directory.
// Paths containing ".." are rejected to prevent path traversal attacks.
func (s *Server) ServeFile(c echo.Context) error {
	if strings.TrimSpace(s.filesDir) == "" {
		return c.JSON(gohttp.StatusNotFound, jsonError("files directory is not configured"))
	}

	raw := strings.TrimPrefix(c.Param("*"), "/")
	rel, err := sanitizeFilePath(raw)
	if err != nil {
		return c.JSON(gohttp.StatusBadRequest, jsonError("invalid file path"))
	}

	full := filepath.Join(s.filesDir, filepath.FromSlash(rel))
	if _, err := os.Stat(full); err != nil {
		if os.IsNotExist(err) {
			return c.JSON(gohttp.StatusNotFound, jsonError("file not found"))
		}
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	return c.File(full)
}

// sanitizeFilePath validates and cleans a relative file path, rejecting
// any traversal attempts (e.g. "..").
func sanitizeFilePath(raw string) (string, error) {
	p := strings.ReplaceAll(strings.TrimSpace(raw), "\\", "/")
	p = strings.TrimPrefix(p, "/")
	p = path.Clean(p)
	if p == "." || strings.HasPrefix(p, "../") || strings.Contains(p, "/../") {
		return "", fmt.Errorf("invalid path")
	}
	return p, nil
}
