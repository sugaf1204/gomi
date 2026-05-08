package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	gohttp "net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/sugaf1204/gomi/internal/infra/httputil"
	"github.com/sugaf1204/gomi/internal/osimage"
	"github.com/sugaf1204/gomi/internal/resource"
)

func (s *Server) CreateOSImage(c echo.Context) error {
	var img osimage.OSImage
	if err := c.Bind(&img); err != nil {
		return c.JSON(gohttp.StatusBadRequest, jsonError("invalid body"))
	}
	created, err := s.osimages.Create(c.Request().Context(), img)
	if err != nil {
		return c.JSON(gohttp.StatusBadRequest, jsonErrorErr(err))
	}
	if created.Source == osimage.SourceURL {
		created = s.downloadURLImage(c, created)
	}
	httputil.CreateAudit(c, s.authStore, created.Name, "create-os-image", "success", "os image created", nil)
	return c.JSON(gohttp.StatusCreated, created)
}

func (s *Server) ListOSImages(c echo.Context) error {
	items, err := s.osimages.List(c.Request().Context())
	if err != nil {
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	return c.JSON(gohttp.StatusOK, itemsResponse[osimage.OSImage]{Items: items})
}

func (s *Server) GetOSImage(c echo.Context) error {
	name := c.Param("name")
	img, err := s.osimages.Get(c.Request().Context(), name)
	if err != nil {
		if errors.Is(err, resource.ErrNotFound) {
			return c.JSON(gohttp.StatusNotFound, jsonError("not found"))
		}
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	return c.JSON(gohttp.StatusOK, img)
}

func (s *Server) UploadOSImage(c echo.Context) error {
	name := c.Param("name")

	img, err := s.osimages.Get(c.Request().Context(), name)
	if err != nil {
		if errors.Is(err, resource.ErrNotFound) {
			return c.JSON(gohttp.StatusNotFound, jsonError("not found"))
		}
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	if img.Source != osimage.SourceUpload {
		return c.JSON(gohttp.StatusBadRequest, jsonError("image source is not 'upload'"))
	}

	file, err := c.FormFile("file")
	if err != nil {
		return c.JSON(gohttp.StatusBadRequest, jsonError("file is required"))
	}

	ext := filepath.Ext(file.Filename)
	if ext == "" {
		// Fall back to format-based extension
		switch img.Format {
		case osimage.FormatISO:
			ext = ".iso"
		case osimage.FormatRAW:
			ext = ".img"
		case osimage.FormatSquashFS:
			ext = ".squashfs"
		default:
			ext = ".qcow2"
		}
	}

	localPath, payloadPath, err := s.imageWritePaths(img, ext)
	if err != nil {
		return c.JSON(gohttp.StatusBadRequest, jsonErrorErr(err))
	}
	if err := os.MkdirAll(filepath.Dir(payloadPath), 0o755); err != nil {
		return c.JSON(gohttp.StatusInternalServerError, jsonError("failed to create storage directory"))
	}

	dst, err := os.Create(payloadPath)
	if err != nil {
		return c.JSON(gohttp.StatusInternalServerError, jsonError("failed to create file"))
	}
	defer dst.Close()

	src, err := file.Open()
	if err != nil {
		return c.JSON(gohttp.StatusInternalServerError, jsonError("failed to open upload"))
	}
	defer src.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return c.JSON(gohttp.StatusInternalServerError, jsonError("failed to save file"))
	}
	if err := s.publishOSImageFile(localPath); err != nil {
		return c.JSON(gohttp.StatusInternalServerError, jsonError("failed to publish image file"))
	}

	updated, err := s.osimages.UpdateStatus(c.Request().Context(), name, true, localPath, "")
	if err != nil {
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	httputil.CreateAudit(c, s.authStore, name, "upload-os-image", "success", "os image uploaded", nil)
	return c.JSON(gohttp.StatusOK, updated)
}

func (s *Server) downloadURLImage(c echo.Context, img osimage.OSImage) osimage.OSImage {
	localPath, err := s.downloadURLImageFile(c.Request().Context(), img)
	if err != nil {
		updated, updateErr := s.osimages.UpdateStatus(c.Request().Context(), img.Name, false, "", err.Error())
		if updateErr == nil {
			return updated
		}
		img.Ready = false
		img.Error = err.Error()
		return img
	}
	updated, err := s.osimages.UpdateStatus(c.Request().Context(), img.Name, true, localPath, "")
	if err != nil {
		img.Ready = false
		img.Error = err.Error()
		return img
	}
	return updated
}

func (s *Server) downloadURLImageFile(ctx context.Context, img osimage.OSImage) (string, error) {
	rawURL := strings.TrimSpace(img.URL)
	if rawURL == "" {
		return "", fmt.Errorf("image URL is required")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid image URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("unsupported image URL scheme: %s", parsed.Scheme)
	}

	localPath, payloadPath, err := s.imageWritePaths(img, imageExtension(img, parsed.Path))
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(payloadPath), 0o755); err != nil {
		return "", fmt.Errorf("create image storage directory: %w", err)
	}
	req, err := gohttp.NewRequestWithContext(ctx, gohttp.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := gohttp.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download image: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != gohttp.StatusOK {
		io.Copy(io.Discard, resp.Body)
		return "", fmt.Errorf("download image: status %d", resp.StatusCode)
	}

	tmpPath := payloadPath + ".download"
	if err := writeImageFileWithChecksum(tmpPath, resp.Body, img.Checksum); err != nil {
		_ = os.Remove(tmpPath)
		return "", err
	}
	if err := os.Rename(tmpPath, payloadPath); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("publish image file: %w", err)
	}
	if err := s.publishOSImageFile(localPath); err != nil {
		return "", err
	}
	return localPath, nil
}

func (s *Server) imageWritePaths(img osimage.OSImage, ext string) (localPath, payloadPath string, err error) {
	storageDir := s.imageStorageDir
	if storageDir == "" {
		storageDir = "data/images"
	}
	if rel, ok, err := manifestRootPath(img); err != nil {
		return "", "", err
	} else if ok {
		localPath = filepath.Join(storageDir, img.Name)
		return localPath, filepath.Join(localPath, filepath.FromSlash(rel)), nil
	}
	localPath = filepath.Join(storageDir, img.Name+ext)
	return localPath, localPath, nil
}

func manifestRootPath(img osimage.OSImage) (string, bool, error) {
	if img.Manifest == nil || strings.TrimSpace(img.Manifest.Root.Path) == "" {
		return "", false, nil
	}
	raw := strings.TrimSpace(img.Manifest.Root.Path)
	if strings.Contains(raw, "\\") || path.IsAbs(raw) {
		return "", false, fmt.Errorf("invalid manifest root path: %s", raw)
	}
	clean := path.Clean(raw)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", false, fmt.Errorf("invalid manifest root path: %s", raw)
	}
	return clean, true, nil
}

func (s *Server) publishOSImageFile(localPath string) error {
	if strings.TrimSpace(s.filesDir) == "" {
		return nil
	}
	st, err := os.Stat(localPath)
	if err != nil {
		return fmt.Errorf("stat image file: %w", err)
	}
	if st.IsDir() {
		return nil
	}
	imagesDir := filepath.Join(s.filesDir, "images")
	if err := os.MkdirAll(imagesDir, 0o755); err != nil {
		return fmt.Errorf("create pxe image directory: %w", err)
	}
	src, err := filepath.Abs(localPath)
	if err != nil {
		return err
	}
	dst := filepath.Join(imagesDir, filepath.Base(localPath))
	dstAbs, err := filepath.Abs(dst)
	if err != nil {
		return err
	}
	if src == dstAbs {
		return nil
	}
	tmp := dst + ".tmp"
	_ = os.Remove(tmp)
	if err := os.Symlink(src, tmp); err != nil {
		return fmt.Errorf("publish image symlink: %w", err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("publish image symlink: %w", err)
	}
	return nil
}

func imageExtension(img osimage.OSImage, rawPath string) string {
	ext := strings.ToLower(filepath.Ext(rawPath))
	switch ext {
	case ".qcow2", ".raw", ".img", ".iso", ".squashfs":
		return ext
	}
	switch img.Format {
	case osimage.FormatISO:
		return ".iso"
	case osimage.FormatRAW:
		return ".raw"
	case osimage.FormatSquashFS:
		return ".squashfs"
	default:
		return ".qcow2"
	}
}

func writeImageFileWithChecksum(path string, r io.Reader, expected string) error {
	dst, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create image file: %w", err)
	}

	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(dst, h), r); err != nil {
		_ = dst.Close()
		return fmt.Errorf("write image file: %w", err)
	}
	if err := dst.Close(); err != nil {
		return fmt.Errorf("close image file: %w", err)
	}
	expected = normalizeSHA256(expected)
	if expected == "" {
		return nil
	}
	actual := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("checksum mismatch: expected %s got %s", expected, actual)
	}
	return nil
}

func normalizeSHA256(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "sha256:")
	return strings.TrimSpace(value)
}

func (s *Server) UpdateOSImageStatus(c echo.Context) error {
	name := c.Param("name")

	var body struct {
		Ready     *bool  `json:"ready"`
		LocalPath string `json:"localPath"`
		Error     string `json:"error"`
	}
	if err := c.Bind(&body); err != nil {
		return c.JSON(gohttp.StatusBadRequest, jsonError("invalid body"))
	}

	img, err := s.osimages.Get(c.Request().Context(), name)
	if err != nil {
		if errors.Is(err, resource.ErrNotFound) {
			return c.JSON(gohttp.StatusNotFound, jsonError("not found"))
		}
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}

	ready := img.Ready
	if body.Ready != nil {
		ready = *body.Ready
	}
	localPath := img.LocalPath
	if body.LocalPath != "" {
		localPath = body.LocalPath
	}

	updated, err := s.osimages.UpdateStatus(c.Request().Context(), name, ready, localPath, body.Error)
	if err != nil {
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	httputil.CreateAudit(c, s.authStore, name, "update-os-image-status", "success", "os image status updated", nil)
	return c.JSON(gohttp.StatusOK, updated)
}

func (s *Server) DownloadOSImage(c echo.Context) error {
	name := c.Param("name")
	img, err := s.osimages.Get(c.Request().Context(), name)
	if err != nil {
		if errors.Is(err, resource.ErrNotFound) {
			return c.JSON(gohttp.StatusNotFound, jsonError("not found"))
		}
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	if !img.Ready || img.LocalPath == "" {
		return c.JSON(gohttp.StatusNotFound, jsonError("image not ready or no local file"))
	}
	localPath := img.LocalPath
	if st, err := os.Stat(localPath); err == nil && st.IsDir() && img.Manifest != nil && strings.TrimSpace(img.Manifest.Root.Path) != "" {
		localPath = filepath.Join(localPath, filepath.FromSlash(strings.TrimSpace(img.Manifest.Root.Path)))
	} else if err != nil {
		return c.JSON(gohttp.StatusNotFound, jsonError("image file not found on disk: "+img.LocalPath))
	}
	if _, err := os.Stat(localPath); err != nil {
		return c.JSON(gohttp.StatusNotFound, jsonError("image file not found on disk: "+img.LocalPath))
	}
	return c.File(localPath)
}

func (s *Server) DeleteOSImage(c echo.Context) error {
	name := c.Param("name")
	if err := s.osimages.Delete(c.Request().Context(), name); err != nil {
		if errors.Is(err, resource.ErrNotFound) {
			return c.JSON(gohttp.StatusNotFound, jsonError("not found"))
		}
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	httputil.CreateAudit(c, s.authStore, name, "delete-os-image", "success", "os image deleted", nil)
	return c.NoContent(gohttp.StatusNoContent)
}
