package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
)

func atomicWriteFromReader(destPath string, r io.Reader) error {
	tmpPath := destPath + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return err
	}
	defer os.Remove(tmpPath)
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
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
