package vm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	gohttp "net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sugaf1204/gomi/internal/osimage"
)

const cloudImageDownloadTimeout = 30 * time.Minute

func (d *Deployer) uploadCloudImageBacking(ctx context.Context, storage cloudImageStorage, img osimage.OSImage, volumeName string, backingFormat string) error {
	rawURL := strings.TrimSpace(img.URL)
	if rawURL == "" {
		return fmt.Errorf("osImage %s has no url", img.Name)
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("invalid cloud image URL for %s", img.Name)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("unsupported cloud image URL scheme for %s: %s", img.Name, parsed.Scheme)
	}
	downloadCtx, cancel := context.WithTimeout(ctx, cloudImageDownloadTimeout)
	defer cancel()

	req, err := gohttp.NewRequestWithContext(downloadCtx, gohttp.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	resp, err := gohttp.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download cloud image %s: %w", img.Name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != gohttp.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return fmt.Errorf("download cloud image %s: status %d", img.Name, resp.StatusCode)
	}
	sizeBytes := img.SizeBytes
	if sizeBytes <= 0 {
		sizeBytes = resp.ContentLength
	}
	if sizeBytes <= 0 {
		return fmt.Errorf("download cloud image %s: content length is required when sizeBytes is unset", img.Name)
	}

	reader := io.Reader(resp.Body)
	var checksum *hashingReader
	if strings.TrimSpace(img.Checksum) != "" {
		checksum = newHashingReader(resp.Body)
		reader = checksum
	}
	if err := storage.CreateVolumeFromReader(downloadCtx, volumeName, sizeBytes, backingFormat, reader); err != nil {
		return fmt.Errorf("sync cloud image %s to hypervisor: %w", img.Name, err)
	}
	if checksum != nil {
		if got, want := checksum.SumHex(), normalizeSHA256(img.Checksum); got != want {
			if err := storage.DeleteVolume(ctx, volumeName); err != nil {
				return fmt.Errorf("cloud image checksum mismatch for %s: expected %s got %s; cleanup failed: %w", img.Name, want, got, err)
			}
			return fmt.Errorf("cloud image checksum mismatch for %s: expected %s got %s", img.Name, want, got)
		}
	}
	return nil
}

type hashingReader struct {
	r io.Reader
	h hash.Hash
}

func newHashingReader(r io.Reader) *hashingReader {
	return &hashingReader{r: r, h: sha256.New()}
}

func (r *hashingReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	if n > 0 {
		_, _ = r.h.Write(p[:n])
	}
	return n, err
}

func (r *hashingReader) SumHex() string {
	return hex.EncodeToString(r.h.Sum(nil))
}

func normalizeSHA256(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "sha256:")
	return strings.ToLower(strings.TrimSpace(value))
}
