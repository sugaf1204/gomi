package api

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/sugaf1204/gomi/internal/osimage"
)

func windowsImageReady(img osimage.OSImage, localPath string) bool {
	return osimage.WindowsArtifactsReady(img, localPath) == nil
}

// UploadOSImageArtifact adds one explicitly declared Windows boot asset. The
// install WIM is uploaded using the ordinary image endpoint.
func (s *Server) UploadOSImageArtifact(c echo.Context) error {
	img, err := s.osimages.Get(c.Request().Context(), c.Param("name"))
	if err != nil {
		return c.JSON(http.StatusNotFound, jsonError("OS image not found"))
	}
	if img.Manifest == nil || img.Manifest.Windows == nil || img.Source != osimage.SourceUpload {
		return c.JSON(http.StatusBadRequest, jsonError("artifact upload requires an uploaded Windows WIM image"))
	}
	rel := c.FormValue("path")
	allowed := false
	for _, candidate := range img.Manifest.WindowsArtifacts()[1:] {
		if rel == candidate {
			allowed = true
		}
	}
	if !allowed {
		return c.JSON(http.StatusBadRequest, jsonError("path is not a declared Windows boot artifact"))
	}
	directory, _, err := s.imageWritePaths(img, ".wim")
	if err != nil {
		return c.JSON(http.StatusBadRequest, jsonErrorErr(err))
	}
	destination := filepath.Join(directory, filepath.FromSlash(rel))
	if err := ensureArtifactDirectory(directory, filepath.Dir(destination)); err != nil {
		return c.JSON(http.StatusBadRequest, jsonErrorErr(err))
	}
	header, err := c.FormFile("file")
	if err != nil {
		return c.JSON(http.StatusBadRequest, jsonError("file is required"))
	}
	src, err := header.Open()
	if err != nil {
		return err
	}
	defer src.Close()
	file, err := os.CreateTemp(filepath.Dir(destination), ".gomi-artifact-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	written, err := io.Copy(file, src)
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if written == 0 {
		return c.JSON(http.StatusBadRequest, jsonError("artifact cannot be empty"))
	}
	if err := os.Rename(file.Name(), destination); err != nil {
		return err
	}
	updated, err := s.osimages.UpdateStatus(c.Request().Context(), img.Name, windowsImageReady(img, directory), directory, "")
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, osImageResponse(updated))
}

// Do not follow existing symlink directories while writing operator uploads.
func ensureArtifactDirectory(root, directory string) error {
	relative, err := filepath.Rel(root, directory)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("invalid artifact directory")
	}
	current := root
	parts := []string{""}
	if relative != "." {
		parts = append(parts, strings.Split(relative, string(os.PathSeparator))...)
	}
	for _, part := range parts {
		current = filepath.Join(current, part)
		if err := os.MkdirAll(current, 0755); err != nil {
			return err
		}
		st, err := os.Lstat(current)
		if err != nil || !st.IsDir() {
			return fmt.Errorf("artifact directory must not contain symlinks")
		}
	}
	return nil
}
