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
	"path/filepath"
	"strings"

	"github.com/klauspost/compress/zstd"
	"github.com/labstack/echo/v4"

	"github.com/sugaf1204/gomi/internal/bootenv"
	"github.com/sugaf1204/gomi/internal/oscatalog"
	"github.com/sugaf1204/gomi/internal/osimage"
	"github.com/sugaf1204/gomi/internal/resource"
)

var catalogHTTPClient = gohttp.DefaultClient

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

type osCatalogEntryResponse struct {
	Entry           oscatalog.Entry `json:"entry"`
	Installed       bool            `json:"installed"`
	Installing      bool            `json:"installing"`
	OSImageReady    bool            `json:"osImageReady"`
	OSImageError    string          `json:"osImageError,omitempty"`
	BootEnvironment bootenv.Status  `json:"bootEnvironment"`
}

func (s *Server) ListOSCatalog(c echo.Context) error {
	ctx := c.Request().Context()
	items := make([]osCatalogEntryResponse, 0)
	entries, err := oscatalog.ListWithContext(ctx)
	if err != nil {
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	for _, entry := range entries {
		items = append(items, s.osCatalogStatus(ctx, entry))
	}
	return c.JSON(gohttp.StatusOK, itemsResponse[osCatalogEntryResponse]{Items: items})
}

func (s *Server) InstallOSCatalogEntry(c echo.Context) error {
	entry, ok, err := oscatalog.GetWithContext(c.Request().Context(), c.Param("name"))
	if err != nil {
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	if !ok {
		return c.JSON(gohttp.StatusNotFound, jsonError("catalog entry not found"))
	}
	if s.bootenvs == nil {
		return c.JSON(gohttp.StatusServiceUnavailable, jsonError("boot environment manager is not configured"))
	}

	status := s.osCatalogStatus(c.Request().Context(), entry)
	if status.OSImageReady && status.BootEnvironment.Phase == bootenv.PhaseReady {
		return c.JSON(gohttp.StatusOK, status)
	}
	s.catalogMu.Lock()
	if _, running := s.catalogInstalls[entry.Name]; running {
		s.catalogMu.Unlock()
		return c.JSON(gohttp.StatusAccepted, status)
	}
	s.catalogInstalls[entry.Name] = struct{}{}
	s.catalogMu.Unlock()

	img := entry.OSImage()
	if _, err := s.osimages.Create(c.Request().Context(), img); err != nil {
		s.catalogMu.Lock()
		delete(s.catalogInstalls, entry.Name)
		s.catalogMu.Unlock()
		return c.JSON(gohttp.StatusBadRequest, jsonErrorErr(err))
	}

	go s.runCatalogInstall(entry)
	return c.JSON(gohttp.StatusAccepted, s.osCatalogStatus(c.Request().Context(), entry))
}

func (s *Server) runCatalogInstall(entry oscatalog.Entry) {
	defer func() {
		s.catalogMu.Lock()
		delete(s.catalogInstalls, entry.Name)
		s.catalogMu.Unlock()
	}()

	ctx := context.Background()
	img := entry.OSImage()
	if err := validateCatalogArtifact(entry); err != nil {
		_, _ = s.osimages.UpdateStatus(ctx, img.Name, false, "", err.Error())
		return
	}
	localPath, localChecksum, err := s.downloadCatalogArtifact(ctx, entry, img)
	if err != nil {
		_, _ = s.osimages.UpdateStatus(ctx, img.Name, false, "", err.Error())
		return
	}
	if localChecksum != "" {
		img.Checksum = "sha256:" + localChecksum
		if _, err := s.osimages.UpdateChecksum(ctx, img.Name, img.Checksum); err != nil {
			_, _ = s.osimages.UpdateStatus(ctx, img.Name, false, "", err.Error())
			return
		}
	}
	if _, err := s.osimages.UpdateStatus(ctx, img.Name, true, localPath, ""); err != nil {
		return
	}
	if s.bootenvs != nil && catalogEntryNeedsBootEnvironment(entry) {
		_, _ = s.bootenvs.Ensure(ctx, entry.BootEnvironment)
	}
}

func validateCatalogArtifact(entry oscatalog.Entry) error {
	if entry.Format != osimage.FormatQCOW2 && entry.Format != osimage.FormatRAW && entry.Format != osimage.FormatSquashFS {
		return fmt.Errorf("unsupported catalog image format: %s; only qcow2, raw, and squashfs prebuilt artifacts are supported", entry.Format)
	}
	sourceFormat := entry.SourceFormat
	if sourceFormat == "" {
		sourceFormat = entry.Format
	}
	if sourceFormat != entry.Format {
		return fmt.Errorf("unsupported catalog image source format: %s; catalog source must match artifact format %s", sourceFormat, entry.Format)
	}
	if strings.TrimSpace(entry.URL) == "" {
		return fmt.Errorf("catalog artifact URL is required")
	}
	compression := strings.TrimSpace(entry.SourceCompression)
	switch entry.Format {
	case osimage.FormatQCOW2:
		if entry.Variant != osimage.VariantCloud {
			return fmt.Errorf("qcow2 catalog image format is only supported for cloud variants")
		}
		if compression != "" {
			return fmt.Errorf("unsupported qcow2 catalog source compression: %s", compression)
		}
	case osimage.FormatRAW:
		if compression != "" && compression != "zstd" {
			return fmt.Errorf("unsupported catalog image source compression: %s", compression)
		}
	case osimage.FormatSquashFS:
		if compression != "" {
			return fmt.Errorf("unsupported squashfs catalog source compression: %s", compression)
		}
	}
	return nil
}

func (s *Server) downloadCatalogArtifact(ctx context.Context, entry oscatalog.Entry, img osimage.OSImage) (string, string, error) {
	switch entry.Format {
	case osimage.FormatQCOW2:
		return s.downloadCatalogFileArtifact(ctx, entry, img, ".qcow2")
	case osimage.FormatRAW:
		return s.downloadCatalogRawArtifact(ctx, entry, img)
	case osimage.FormatSquashFS:
		return s.downloadCatalogSquashFSArtifact(ctx, entry, img)
	default:
		return "", "", fmt.Errorf("unsupported catalog image format: %s", entry.Format)
	}
}

func (s *Server) downloadCatalogFileArtifact(ctx context.Context, entry oscatalog.Entry, img osimage.OSImage, ext string) (string, string, error) {
	rawURL := strings.TrimSpace(entry.URL)
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", "", fmt.Errorf("invalid catalog image URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", "", fmt.Errorf("unsupported catalog image URL scheme: %s", parsed.Scheme)
	}

	storageDir := s.imageStorageDir
	if storageDir == "" {
		storageDir = "data/images"
	}
	if err := os.MkdirAll(storageDir, 0o755); err != nil {
		return "", "", fmt.Errorf("create image storage directory: %w", err)
	}
	localPath := filepath.Join(storageDir, img.Name+ext)

	req, err := gohttp.NewRequestWithContext(ctx, gohttp.MethodGet, rawURL, nil)
	if err != nil {
		return "", "", err
	}
	resp, err := catalogHTTPClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("download image: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != gohttp.StatusOK {
		io.Copy(io.Discard, resp.Body)
		return "", "", fmt.Errorf("download image: status %d", resp.StatusCode)
	}

	tmpPath := localPath + ".download"
	defer os.Remove(tmpPath)
	if err := writeImageFileWithChecksum(tmpPath, resp.Body, img.Checksum); err != nil {
		return "", "", err
	}
	localChecksum, err := fileSHA256(tmpPath)
	if err != nil {
		return "", "", err
	}
	if err := os.Rename(tmpPath, localPath); err != nil {
		return "", "", fmt.Errorf("publish image file: %w", err)
	}
	if err := s.publishOSImageFile(localPath); err != nil {
		return "", "", err
	}
	return localPath, localChecksum, nil
}

func (s *Server) downloadCatalogRawArtifact(ctx context.Context, entry oscatalog.Entry, img osimage.OSImage) (string, string, error) {
	rawURL := strings.TrimSpace(entry.URL)
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", "", fmt.Errorf("invalid catalog image URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", "", fmt.Errorf("unsupported catalog image URL scheme: %s", parsed.Scheme)
	}

	storageDir := s.imageStorageDir
	if storageDir == "" {
		storageDir = "data/images"
	}
	if err := os.MkdirAll(storageDir, 0o755); err != nil {
		return "", "", fmt.Errorf("create image storage directory: %w", err)
	}
	localPath := filepath.Join(storageDir, img.Name+".raw")

	req, err := gohttp.NewRequestWithContext(ctx, gohttp.MethodGet, rawURL, nil)
	if err != nil {
		return "", "", err
	}
	resp, err := catalogHTTPClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("download image: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != gohttp.StatusOK {
		io.Copy(io.Discard, resp.Body)
		return "", "", fmt.Errorf("download image: status %d", resp.StatusCode)
	}

	tmpPath := localPath + ".download"
	defer os.Remove(tmpPath)
	if strings.TrimSpace(entry.SourceCompression) == "zstd" {
		compressedPath := tmpPath + ".zst"
		defer os.Remove(compressedPath)
		if err := writeImageFileWithChecksum(compressedPath, resp.Body, img.Checksum); err != nil {
			return "", "", err
		}

		compressedFile, err := os.Open(compressedPath)
		if err != nil {
			return "", "", fmt.Errorf("open downloaded zstd image: %w", err)
		}
		defer compressedFile.Close()
		zstdReader, err := zstd.NewReader(compressedFile)
		if err != nil {
			return "", "", fmt.Errorf("open zstd image stream: %w", err)
		}
		defer zstdReader.Close()

		if err := writeImageFileWithChecksum(tmpPath, zstdReader, ""); err != nil {
			return "", "", err
		}
	} else if err := writeImageFileWithChecksum(tmpPath, resp.Body, img.Checksum); err != nil {
		return "", "", err
	}
	localChecksum, err := fileSHA256(tmpPath)
	if err != nil {
		return "", "", err
	}
	if err := os.Rename(tmpPath, localPath); err != nil {
		return "", "", fmt.Errorf("publish image file: %w", err)
	}
	if err := s.publishOSImageFile(localPath); err != nil {
		return "", "", err
	}
	return localPath, localChecksum, nil
}

func (s *Server) downloadCatalogSquashFSArtifact(ctx context.Context, entry oscatalog.Entry, img osimage.OSImage) (string, string, error) {
	rawURL := strings.TrimSpace(entry.URL)
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", "", fmt.Errorf("invalid catalog image URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", "", fmt.Errorf("unsupported catalog image URL scheme: %s", parsed.Scheme)
	}

	storageDir := s.imageStorageDir
	if storageDir == "" {
		storageDir = "data/images"
	}
	localDir := filepath.Join(storageDir, img.Name)
	if err := os.MkdirAll(localDir, 0o755); err != nil {
		return "", "", fmt.Errorf("create image storage directory: %w", err)
	}
	localPath := filepath.Join(localDir, "rootfs.squashfs")

	req, err := gohttp.NewRequestWithContext(ctx, gohttp.MethodGet, rawURL, nil)
	if err != nil {
		return "", "", err
	}
	resp, err := catalogHTTPClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("download image: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != gohttp.StatusOK {
		io.Copy(io.Discard, resp.Body)
		return "", "", fmt.Errorf("download image: status %d", resp.StatusCode)
	}

	tmpPath := localPath + ".download"
	defer os.Remove(tmpPath)
	if err := writeImageFileWithChecksum(tmpPath, resp.Body, img.Checksum); err != nil {
		return "", "", err
	}
	localChecksum, err := fileSHA256(tmpPath)
	if err != nil {
		return "", "", err
	}
	if err := os.Rename(tmpPath, localPath); err != nil {
		return "", "", fmt.Errorf("publish image file: %w", err)
	}
	return localDir, localChecksum, nil
}

func (s *Server) osCatalogStatus(ctx context.Context, entry oscatalog.Entry) osCatalogEntryResponse {
	resp := osCatalogEntryResponse{Entry: entry}
	needsBootenv := catalogEntryNeedsBootEnvironment(entry)
	if s.bootenvs != nil {
		resp.BootEnvironment = s.bootenvs.Status(entry.BootEnvironment)
	}
	img, err := s.osimages.Get(ctx, entry.Name)
	if err == nil {
		resp.OSImageReady = img.Ready
		resp.OSImageError = img.Error
	}
	resp.Installed = resp.OSImageReady && resp.OSImageError == "" && (!needsBootenv || resp.BootEnvironment.Phase == bootenv.PhaseReady)
	s.catalogMu.Lock()
	_, resp.Installing = s.catalogInstalls[entry.Name]
	s.catalogMu.Unlock()
	return resp
}

func catalogEntryNeedsBootEnvironment(entry oscatalog.Entry) bool {
	return entry.Format == osimage.FormatSquashFS || entry.Variant == osimage.VariantBareMetal
}

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
