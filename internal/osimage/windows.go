package osimage

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// WindowsImage describes a DISM-applicable image and an operator-built WinPE.
// WinPE must include PowerShell, WMI, and the GOMI startup launcher.
type WindowsImage struct {
	ImageIndex int       `json:"imageIndex"`
	WinPE      WindowsPE `json:"winPE"`
}

type WindowsPE struct {
	Wimboot string `json:"wimboot"`
	BCD     string `json:"bcd"`
	BootSDI string `json:"bootSDI"`
	BootWIM string `json:"bootWIM"`
}

func (m *Manifest) WindowsArtifacts() []string {
	if m == nil || m.Windows == nil {
		return nil
	}
	p := m.Windows.WinPE
	return []string{m.Root.Path, p.Wimboot, p.BCD, p.BootSDI, p.BootWIM}
}

var windowsArtifactPathPattern = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)

func validateWindowsImage(img OSImage) error {
	windows := strings.EqualFold(strings.TrimSpace(img.OSFamily), "windows")
	format := EffectiveImageFormat(img)
	hasWindows := img.Manifest != nil && img.Manifest.Windows != nil
	if !windows && format != FormatWIM && !hasWindows {
		return nil
	}
	if windows && img.OSFamily != strings.TrimSpace(img.OSFamily) {
		return fmt.Errorf("Windows osFamily must not contain surrounding whitespace")
	}
	if !windows || format != FormatWIM || !hasWindows {
		return fmt.Errorf("Windows deployment requires osFamily windows, format wim, and manifest.windows")
	}
	if img.Source == SourceURL {
		return fmt.Errorf("Windows images require upload source so companion WinPE artifacts can be uploaded")
	}
	if img.Format != FormatWIM || img.Manifest.Root.Format != FormatWIM {
		return fmt.Errorf("Windows image and manifest.root format must both be wim")
	}
	if img.Arch != "amd64" && img.Arch != "x86_64" {
		return fmt.Errorf("Windows deployment supports only amd64")
	}
	if img.Manifest.OSFamily != "" && !strings.EqualFold(img.Manifest.OSFamily, "windows") {
		return fmt.Errorf("Windows manifest OS family must match image")
	}
	if img.Manifest.Arch != "" && img.Manifest.Arch != "amd64" && img.Manifest.Arch != "x86_64" {
		return fmt.Errorf("Windows manifest architecture must be amd64")
	}
	if img.Manifest.Root.Compression != "" && img.Manifest.Root.Compression != "none" {
		return fmt.Errorf("Windows WIM must not use external compression")
	}
	if len(img.Manifest.BootModes) != 1 || img.Manifest.BootModes[0] != "uefi" {
		return fmt.Errorf("Windows deployment requires manifest.bootModes [uefi]")
	}
	if img.Manifest.Windows.ImageIndex <= 0 {
		return fmt.Errorf("manifest.windows.imageIndex must be positive")
	}
	seen := map[string]bool{}
	for _, raw := range img.Manifest.WindowsArtifacts() {
		if !windowsArtifactPathPattern.MatchString(raw) || strings.ContainsAny(raw, "\\\r\n\x00") || path.IsAbs(raw) || path.Clean(raw) != raw || raw == "." || raw == ".." || strings.HasPrefix(raw, "../") {
			return fmt.Errorf("Windows artifact must be a clean relative path: %q", raw)
		}
		if seen[raw] {
			return fmt.Errorf("Windows artifact paths must be distinct")
		}
		seen[raw] = true
	}
	return nil
}

// WindowsArtifactsReady verifies the complete operator-built asset set exists.
func WindowsArtifactsReady(img OSImage, directory string) error {
	if img.Manifest == nil || img.Manifest.Windows == nil {
		return nil
	}
	for _, rel := range img.Manifest.WindowsArtifacts() {
		st, err := os.Lstat(filepath.Join(directory, filepath.FromSlash(rel)))
		if err != nil || !st.Mode().IsRegular() || st.Size() == 0 {
			return fmt.Errorf("Windows artifact is missing or not a nonempty regular file: %s", rel)
		}
	}
	return nil
}
