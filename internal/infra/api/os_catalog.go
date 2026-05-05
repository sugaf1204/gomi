package api

import (
	"context"
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
	for _, entry := range oscatalog.List() {
		items = append(items, s.osCatalogStatus(ctx, entry))
	}
	return c.JSON(gohttp.StatusOK, map[string]any{"items": items})
}

func (s *Server) InstallOSCatalogEntry(c echo.Context) error {
	entry, ok := oscatalog.Get(c.Param("name"))
	if !ok {
		return c.JSON(gohttp.StatusNotFound, map[string]string{"error": "catalog entry not found"})
	}
	if s.bootenvs == nil {
		return c.JSON(gohttp.StatusServiceUnavailable, map[string]string{"error": "boot environment manager is not configured"})
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
		return c.JSON(gohttp.StatusBadRequest, map[string]string{"error": err.Error()})
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
	if err := validateCatalogRawArtifact(entry); err != nil {
		_, _ = s.osimages.UpdateStatus(ctx, img.Name, false, "", err.Error())
		return
	}
	localPath, err := s.downloadCatalogRawArtifact(ctx, entry, img)
	if err != nil {
		_, _ = s.osimages.UpdateStatus(ctx, img.Name, false, "", err.Error())
		return
	}
	if _, err := s.osimages.UpdateStatus(ctx, img.Name, true, localPath, ""); err != nil {
		return
	}
	if s.bootenvs != nil {
		_, _ = s.bootenvs.Ensure(ctx, entry.BootEnvironment)
	}
}

func validateCatalogRawArtifact(entry oscatalog.Entry) error {
	if entry.Format != osimage.FormatRAW {
		return fmt.Errorf("unsupported catalog image format: %s; only raw prebuilt artifacts are supported", entry.Format)
	}
	sourceFormat := entry.SourceFormat
	if sourceFormat == "" {
		sourceFormat = entry.Format
	}
	if sourceFormat != osimage.FormatRAW {
		return fmt.Errorf("unsupported catalog image source format: %s; catalog sources must be raw", sourceFormat)
	}
	if strings.TrimSpace(entry.URL) == "" {
		return fmt.Errorf("catalog raw artifact URL is required")
	}
	compression := strings.TrimSpace(entry.SourceCompression)
	if compression != "" && compression != "zstd" {
		return fmt.Errorf("unsupported catalog image source compression: %s", compression)
	}
	return nil
}

func (s *Server) downloadCatalogRawArtifact(ctx context.Context, entry oscatalog.Entry, img osimage.OSImage) (string, error) {
	rawURL := strings.TrimSpace(entry.URL)
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid catalog image URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("unsupported catalog image URL scheme: %s", parsed.Scheme)
	}

	storageDir := s.imageStorageDir
	if storageDir == "" {
		storageDir = "data/images"
	}
	if err := os.MkdirAll(storageDir, 0o755); err != nil {
		return "", fmt.Errorf("create image storage directory: %w", err)
	}
	localPath := filepath.Join(storageDir, img.Name+".raw")

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

	var reader io.Reader = resp.Body
	var zstdReader *zstd.Decoder
	if strings.TrimSpace(entry.SourceCompression) == "zstd" {
		zstdReader, err = zstd.NewReader(resp.Body)
		if err != nil {
			return "", fmt.Errorf("open zstd image stream: %w", err)
		}
		defer zstdReader.Close()
		reader = zstdReader
	}

	tmpPath := localPath + ".download"
	if err := writeImageFileWithChecksum(tmpPath, reader, img.Checksum); err != nil {
		_ = os.Remove(tmpPath)
		return "", err
	}
	if err := os.Rename(tmpPath, localPath); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("publish image file: %w", err)
	}
	if err := s.publishOSImageFile(localPath); err != nil {
		return "", err
	}
	return localPath, nil
}

func (s *Server) osCatalogStatus(ctx context.Context, entry oscatalog.Entry) osCatalogEntryResponse {
	resp := osCatalogEntryResponse{Entry: entry}
	if s.bootenvs != nil {
		resp.BootEnvironment = s.bootenvs.Status(entry.BootEnvironment)
	}
	img, err := s.osimages.Get(ctx, entry.Name)
	if err == nil {
		resp.OSImageReady = img.Ready
		resp.OSImageError = img.Error
	}
	resp.Installed = resp.OSImageReady && resp.OSImageError == "" && resp.BootEnvironment.Phase == bootenv.PhaseReady
	s.catalogMu.Lock()
	_, resp.Installing = s.catalogInstalls[entry.Name]
	s.catalogMu.Unlock()
	return resp
}

func (s *Server) ListBootEnvironments(c echo.Context) error {
	if s.bootenvs == nil {
		return c.JSON(gohttp.StatusOK, map[string]any{"items": []bootenv.Status{}})
	}
	return c.JSON(gohttp.StatusOK, map[string]any{"items": s.bootenvs.List()})
}

func (s *Server) RebuildBootEnvironment(c echo.Context) error {
	if s.bootenvs == nil {
		return c.JSON(gohttp.StatusServiceUnavailable, map[string]string{"error": "boot environment manager is not configured"})
	}
	return c.JSON(gohttp.StatusAccepted, s.bootenvs.StartRebuild(c.Param("name")))
}

func (s *Server) GetBootEnvironmentLogs(c echo.Context) error {
	if s.bootenvs == nil {
		return c.JSON(gohttp.StatusNotFound, map[string]string{"error": "boot environment manager is not configured"})
	}
	st := s.bootenvs.Status(c.Param("name"))
	if st.LogPath == "" {
		return c.JSON(gohttp.StatusNotFound, map[string]string{"error": "build log not found"})
	}
	if _, err := os.Stat(st.LogPath); err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, resource.ErrNotFound) {
			return c.JSON(gohttp.StatusNotFound, map[string]string{"error": "build log not found"})
		}
		return c.JSON(gohttp.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.File(st.LogPath)
}
