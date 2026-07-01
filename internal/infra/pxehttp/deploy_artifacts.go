package pxehttp

import (
	"context"
	"fmt"
	"github.com/labstack/echo/v4"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/osimage"
	gohttp "net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (h *Handler) PXEArtifact(c echo.Context) error {
	if h.osimages == nil {
		return c.JSON(gohttp.StatusInternalServerError, jsonError("os image service not available"))
	}
	name := c.Param("name")
	raw := strings.TrimPrefix(c.Param("*"), "/")
	rel, err := sanitizePXEPath(raw)
	if err != nil {
		return c.JSON(gohttp.StatusBadRequest, jsonError("invalid artifact path"))
	}
	img, err := h.osimages.Get(c.Request().Context(), name)
	if err != nil {
		return c.JSON(gohttp.StatusNotFound, jsonError("os image not found"))
	}
	if !artifactPathAllowed(img.Manifest, rel) {
		return c.JSON(gohttp.StatusNotFound, jsonError("artifact not found"))
	}
	base, err := artifactBaseDir(img)
	if err != nil {
		return c.JSON(gohttp.StatusNotFound, jsonErrorErr(err))
	}
	full, err := safeArtifactFilePath(base, rel)
	if err != nil {
		return c.JSON(gohttp.StatusNotFound, jsonError("artifact not found"))
	}
	if _, err := os.Stat(full); err != nil {
		if os.IsNotExist(err) {
			return c.JSON(gohttp.StatusNotFound, jsonError("artifact not found"))
		}
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	return h.serveProvisionedPXEFile(c, full, "server.artifact_transfer", "serve OS artifact "+rel)
}

func (h *Handler) serveProvisionedPXEFile(c echo.Context, full, name, message string) error {
	token := strings.TrimSpace(c.QueryParam("token"))
	attemptID := strings.TrimSpace(c.QueryParam("attempt_id"))
	if token == "" || attemptID == "" {
		return c.File(full)
	}
	if c.Request().Method == gohttp.MethodHead {
		return c.File(full)
	}
	if strings.TrimSpace(c.Request().Header.Get("Range")) != "" {
		return c.File(full)
	}
	sizeBytes := int64(0)
	if st, err := os.Stat(full); err == nil {
		sizeBytes = st.Size()
	}
	startedAt := time.Now().UTC()
	err := c.File(full)
	finishedAt := time.Now().UTC()
	result := "success"
	if err != nil {
		result = "failure"
	}
	timing := serverTiming(name, message, result, startedAt, finishedAt, sizeBytes)
	h.appendProvisionTimingByToken(c.Request().Context(), token, attemptID, timing)
	return err
}

func (h *Handler) appendProvisionTimingByToken(ctx context.Context, token, attemptID string, timing machine.ProvisionTiming) {
	target, err := h.requireProvisioningMachine(ctx, token)
	if err != nil || validateAttemptParam(target, attemptID) != nil {
		return
	}
	now := time.Now().UTC()
	_ = h.updateProvisionProgress(ctx, target.Name, func(m *machine.Machine) {
		if m.Provision == nil {
			m.Provision = &machine.ProvisionProgress{}
		}
		m.Provision.LastSignalAt = &now
		m.Provision.Timings = appendProvisionTiming(m.Provision.Timings, timing)
	})
}

func (h *Handler) artifactURL(base string, img osimage.OSImage, rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return "", fmt.Errorf("artifact path is empty")
	}
	clean, err := sanitizePXEPath(rel)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s/artifacts/os-images/%s/%s", strings.TrimRight(base, "/"), url.PathEscape(img.Name), clean), nil
}

func imageFileURL(base string, img osimage.OSImage) (string, error) {
	local := strings.TrimSpace(img.LocalPath)
	if local == "" {
		return "", fmt.Errorf("os image %q has no local path", img.Name)
	}
	return fmt.Sprintf("%s/files/images/%s", strings.TrimRight(base, "/"), url.PathEscape(filepath.Base(local))), nil
}

func appendProvisionQuery(rawURL, token, attemptID string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	query := parsed.Query()
	if strings.TrimSpace(token) != "" {
		query.Set("token", strings.TrimSpace(token))
	}
	if strings.TrimSpace(attemptID) != "" {
		query.Set("attempt_id", strings.TrimSpace(attemptID))
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func artifactBaseDir(img osimage.OSImage) (string, error) {
	local := strings.TrimSpace(img.LocalPath)
	if local == "" {
		return "", fmt.Errorf("os image has no local artifact path")
	}
	st, err := os.Stat(local)
	if err != nil {
		return "", fmt.Errorf("os image local artifact path is not available")
	}
	if !st.IsDir() {
		return "", fmt.Errorf("os image local artifact path is not an artifact directory")
	}
	return local, nil
}

func artifactPathAllowed(manifest *osimage.Manifest, rel string) bool {
	if manifest == nil {
		return false
	}
	clean, err := sanitizePXEPath(rel)
	if err != nil {
		return false
	}
	if clean == strings.TrimSpace(manifest.Root.Path) {
		return true
	}
	for _, bundle := range manifest.Bundles {
		if clean == strings.TrimSpace(bundle.Path) {
			return true
		}
	}
	return false
}

func safeArtifactFilePath(base, rel string) (string, error) {
	baseReal, err := filepath.EvalSymlinks(base)
	if err != nil {
		return "", err
	}
	full := filepath.Join(baseReal, filepath.FromSlash(rel))
	fullReal, err := filepath.EvalSymlinks(full)
	if err != nil {
		return "", err
	}
	prefix := baseReal + string(os.PathSeparator)
	if fullReal != baseReal && !strings.HasPrefix(fullReal, prefix) {
		return "", fmt.Errorf("artifact path escapes base directory")
	}
	return fullReal, nil
}
