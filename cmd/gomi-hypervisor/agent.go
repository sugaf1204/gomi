package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Config holds the configuration for the hypervisor agent.
type Config struct {
	ServerURL string
	Token     string
	Interval  time.Duration
	ImageDir  string
}

// OSImage represents an OS image from the GOMI server API.
type OSImage struct {
	Name      string           `json:"name"`
	Format    string           `json:"format"`
	Checksum  string           `json:"checksum,omitempty"`
	SizeBytes int64            `json:"sizeBytes,omitempty"`
	Ready     bool             `json:"ready"`
	Manifest  *OSImageManifest `json:"manifest,omitempty"`
}

type OSImageManifest struct {
	Root OSImageRootArtifact `json:"root"`
}

type OSImageRootArtifact struct {
	Format                string `json:"format"`
	Compression           string `json:"compression,omitempty"`
	Path                  string `json:"path"`
	SHA256                string `json:"sha256,omitempty"`
	UncompressedSizeBytes int64  `json:"uncompressedSizeBytes,omitempty"`
}

// apiClient handles authenticated HTTP requests to the GOMI server.
type apiClient struct {
	serverURL string
	token     string
}

func newAPIClient(serverURL, token string) *apiClient {
	return &apiClient{
		serverURL: strings.TrimRight(serverURL, "/"),
		token:     token,
	}
}

// Run starts the hypervisor agent sync loop.
func Run(ctx context.Context, cfg Config) error {
	client := newAPIClient(cfg.ServerURL, cfg.Token)

	log.Printf("gomi-hypervisor: starting (server=%s, interval=%s, imageDir=%s)", cfg.ServerURL, cfg.Interval, cfg.ImageDir)

	// Run once immediately
	if err := syncOnce(ctx, cfg, client); err != nil {
		log.Printf("gomi-hypervisor: initial sync error: %v", err)
	}

	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("gomi-hypervisor: shutting down")
			return nil
		case <-ticker.C:
			if err := syncOnce(ctx, cfg, client); err != nil {
				log.Printf("gomi-hypervisor: sync error: %v", err)
			}
		}
	}
}

func syncOnce(ctx context.Context, cfg Config, client *apiClient) error {
	images, err := client.fetchImages(ctx)
	if err != nil {
		return fmt.Errorf("fetch images: %w", err)
	}

	// Filter to ready images only
	var ready []OSImage
	for _, img := range images {
		if img.Ready {
			ready = append(ready, img)
		}
	}

	// Ensure image directory exists
	if err := os.MkdirAll(cfg.ImageDir, 0o755); err != nil {
		return fmt.Errorf("create image dir: %w", err)
	}

	// Build expected file set
	expected := make(map[string]OSImage)
	for _, img := range ready {
		filename := syncedImageFilename(img)
		expected[filename] = img

		destPath := filepath.Join(cfg.ImageDir, filename)

		if isArtifactImage(img) {
			if needsArtifactDownload(destPath, artifactSourceChecksum(img)) {
				log.Printf("gomi-hypervisor: downloading artifact %s", filename)
				if err := client.downloadArtifactImage(ctx, img, destPath); err != nil {
					log.Printf("gomi-hypervisor: download artifact %s failed: %v", filename, err)
					continue
				}
				log.Printf("gomi-hypervisor: downloaded artifact %s", filename)
			}
			if err := writeManagedMarker(destPath); err != nil {
				log.Printf("gomi-hypervisor: write managed marker for %s failed: %v", filename, err)
			}
			continue
		}

		// Check if file already exists with correct checksum
		if needsDownload(destPath, img.Checksum) {
			log.Printf("gomi-hypervisor: downloading %s", filename)
			if err := client.downloadImage(ctx, img.Name, destPath); err != nil {
				log.Printf("gomi-hypervisor: download %s failed: %v", filename, err)
				continue
			}
			if img.Checksum != "" {
				if err := verifyChecksum(destPath, img.Checksum); err != nil {
					log.Printf("gomi-hypervisor: checksum mismatch for %s: %v", filename, err)
					os.Remove(destPath)
					continue
				}
			}
			log.Printf("gomi-hypervisor: downloaded %s", filename)
		}
		if err := writeManagedMarker(destPath); err != nil {
			log.Printf("gomi-hypervisor: write managed marker for %s failed: %v", filename, err)
		}
	}

	// Clean up stale files
	removed, err := cleanupStaleFiles(cfg.ImageDir, expected)
	if err != nil {
		log.Printf("gomi-hypervisor: cleanup error: %v", err)
	}
	for _, f := range removed {
		log.Printf("gomi-hypervisor: removed stale file %s", f)
	}

	return nil
}

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

func (c *apiClient) doRequest(ctx context.Context, method, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	return http.DefaultClient.Do(req)
}

func (c *apiClient) fetchImages(ctx context.Context) ([]OSImage, error) {
	url := c.serverURL + "/api/v1/os-images"
	resp, err := c.doRequest(ctx, http.MethodGet, url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}
	var result struct {
		Items []OSImage `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Items, nil
}

func (c *apiClient) downloadImage(ctx context.Context, name, destPath string) error {
	url := c.serverURL + "/api/v1/os-images/" + name + "/download"
	resp, err := c.doRequest(ctx, http.MethodGet, url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		return fmt.Errorf("download %s: status %d", name, resp.StatusCode)
	}
	return atomicWriteFromReader(destPath, resp.Body)
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
	if compression == "" {
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
	if compression != "zst" && compression != "zstd" {
		return fmt.Errorf("unsupported artifact compression for %s: %s", img.Name, root.Compression)
	}

	compressedPath := destPath + ".download"
	if err := atomicWriteFromReader(compressedPath, resp.Body); err != nil {
		return err
	}
	defer os.Remove(compressedPath)
	if sourceChecksum != "" {
		if err := verifyChecksum(compressedPath, sourceChecksum); err != nil {
			return err
		}
	}
	if err := decompressZstd(ctx, compressedPath, destPath); err != nil {
		return err
	}
	if sourceChecksum != "" {
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

func decompressZstd(ctx context.Context, compressedPath, destPath string) error {
	tmpPath := destPath + ".tmp"
	out, err := os.Create(tmpPath)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "zstd", "-dc", compressedPath)
	cmd.Stdout = out
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	closeErr := out.Close()
	if runErr != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("zstd decompress %s: %w: %s", compressedPath, runErr, strings.TrimSpace(stderr.String()))
	}
	if closeErr != nil {
		os.Remove(tmpPath)
		return closeErr
	}
	return os.Rename(tmpPath, destPath)
}

func atomicWriteFromReader(destPath string, r io.Reader) error {
	tmpPath := destPath + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, destPath)
}

func verifyChecksum(path, expected string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))

	// Support "sha256:<hash>" format.
	want := expected
	if strings.HasPrefix(want, "sha256:") {
		want = strings.TrimPrefix(want, "sha256:")
	}

	if got != want {
		return fmt.Errorf("checksum mismatch: got %s, want %s", got, want)
	}
	return nil
}

func needsDownload(path, expectedChecksum string) bool {
	info, err := os.Stat(path)
	if err != nil || info.Size() == 0 {
		return true
	}
	if expectedChecksum == "" {
		return false
	}
	return verifyChecksum(path, expectedChecksum) != nil
}

const sourceChecksumSuffix = ".source-sha256"
const managedMarkerSuffix = ".gomi-managed"

func needsArtifactDownload(path, sourceChecksum string) bool {
	info, err := os.Stat(path)
	if err != nil || info.Size() == 0 {
		return true
	}
	sourceChecksum = strings.TrimSpace(sourceChecksum)
	if sourceChecksum == "" {
		return false
	}
	recorded, err := os.ReadFile(path + sourceChecksumSuffix)
	if err != nil {
		return true
	}
	return strings.TrimSpace(string(recorded)) != sourceChecksum
}

func writeSourceChecksum(path, sourceChecksum string) error {
	sourceChecksum = strings.TrimSpace(sourceChecksum)
	if sourceChecksum == "" {
		return nil
	}
	return os.WriteFile(path+sourceChecksumSuffix, []byte(sourceChecksum+"\n"), 0o644)
}

func writeManagedMarker(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() || info.Size() == 0 {
		return nil
	}
	return os.WriteFile(path+managedMarkerSuffix, []byte("gomi-hypervisor\n"), 0o644)
}

var managedExtensions = map[string]bool{
	".qcow2": true,
	".raw":   true,
	".iso":   true,
	".img":   true,
}

func isManagedFile(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	return managedExtensions[ext]
}

func cleanupStaleFiles(dir string, expected map[string]OSImage) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var removed []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !isManagedFile(name) {
			continue
		}
		if _, ok := expected[name]; ok {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, name+managedMarkerSuffix)); err != nil {
			continue
		}
		path := filepath.Join(dir, name)
		if err := os.Remove(path); err != nil {
			continue
		}
		_ = os.Remove(path + sourceChecksumSuffix)
		_ = os.Remove(path + managedMarkerSuffix)
		removed = append(removed, name)
	}
	return removed, nil
}
