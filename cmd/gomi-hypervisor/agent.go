package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

// Config holds the configuration for the hypervisor agent.
type Config struct {
	ServerURL string
	Token     string
	Interval  time.Duration
	ImageDir  string
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
			if err := client.downloadImage(ctx, osImageID(img), destPath); err != nil {
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
