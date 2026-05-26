package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

func isArtifactImage(img OSImage) bool {
	return img.Manifest != nil && strings.TrimSpace(img.Manifest.Root.Path) != ""
}

func syncedImageFilename(img OSImage) string {
	format := strings.TrimSpace(img.Format)
	if isArtifactImage(img) && strings.TrimSpace(img.Manifest.Root.Format) != "" {
		format = strings.TrimSpace(img.Manifest.Root.Format)
	}
	if format == "" {
		format = "qcow2"
	}
	return img.Name + "." + format
}

func artifactSourceChecksum(img OSImage) string {
	if !isArtifactImage(img) {
		return ""
	}
	return strings.TrimSpace(img.Manifest.Root.SHA256)
}

func (c *apiClient) downloadArtifactImage(ctx context.Context, img OSImage, destPath string) error {
	if !isArtifactImage(img) {
		return fmt.Errorf("image %s has no artifact root", img.Name)
	}
	root := img.Manifest.Root
	artifactURL, err := buildArtifactURL(c.serverURL, img.Name, root.Path)
	if err != nil {
		return err
	}
	resp, err := c.doRequest(ctx, http.MethodGet, artifactURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		return fmt.Errorf("download artifact %s: status %d", img.Name, resp.StatusCode)
	}

	compression := strings.ToLower(strings.TrimSpace(root.Compression))
	sourceChecksum := strings.TrimSpace(root.SHA256)
	if compression == "" || !isExternallyCompressedArtifact(root.Path, compression) {
		if err := atomicWriteFromReader(destPath, resp.Body); err != nil {
			return err
		}
		if sourceChecksum != "" {
			if err := verifyChecksum(destPath, sourceChecksum); err != nil {
				os.Remove(destPath)
				return err
			}
			return writeSourceChecksum(destPath, sourceChecksum)
		}
		return nil
	}
	switch compression {
	case "zst", "zstd":
		return c.downloadCompressedArtifactImage(ctx, resp.Body, img, destPath, sourceChecksum, "zstd", decompressZstd)
	case "xz":
		return c.downloadCompressedArtifactImage(ctx, resp.Body, img, destPath, sourceChecksum, "xz", decompressXZ)
	default:
		return fmt.Errorf("unsupported artifact compression for %s: %s", img.Name, root.Compression)
	}
}

func isExternallyCompressedArtifact(path string, compression string) bool {
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(path)))
	switch strings.ToLower(strings.TrimSpace(compression)) {
	case "zst", "zstd":
		return ext == ".zst" || ext == ".zstd"
	case "xz":
		return ext == ".xz"
	default:
		return false
	}
}

func (c *apiClient) downloadCompressedArtifactImage(ctx context.Context, body io.Reader, img OSImage, destPath, sourceChecksum, suffix string, decompress func(context.Context, string, string) error) error {
	compressedPath := destPath + ".download"
	if suffix != "" {
		compressedPath += "." + suffix
	}
	if err := atomicWriteFromReader(compressedPath, body); err != nil {
		return err
	}
	defer os.Remove(compressedPath)
	if err := decompress(ctx, compressedPath, destPath); err != nil {
		return err
	}
	if sourceChecksum != "" {
		if err := verifyChecksum(destPath, sourceChecksum); err != nil {
			os.Remove(destPath)
			return err
		}
		return writeSourceChecksum(destPath, sourceChecksum)
	}
	return nil
}

func buildArtifactURL(serverURL, name, relPath string) (string, error) {
	clean := strings.Trim(strings.ReplaceAll(strings.TrimSpace(relPath), "\\", "/"), "/")
	if clean == "" {
		return "", fmt.Errorf("invalid artifact path: %q", relPath)
	}
	parts := strings.Split(clean, "/")
	for i := range parts {
		if parts[i] == "" || parts[i] == "." || parts[i] == ".." {
			return "", fmt.Errorf("invalid artifact path: %q", relPath)
		}
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.TrimRight(serverURL, "/") + "/pxe/artifacts/os-images/" + url.PathEscape(name) + "/" + strings.Join(parts, "/"), nil
}
