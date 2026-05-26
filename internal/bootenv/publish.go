package bootenv

import (
	"encoding/json"
	"os"
	"path/filepath"
)

func (m *Manager) publishCurrent(name, buildID, artifactDir, logPath string) error {
	current := filepath.Join(m.dataDir, "artifacts", name, "current")
	tmp := current + ".tmp"
	_ = os.Remove(tmp)
	if err := os.Symlink(buildID, tmp); err != nil {
		return err
	}
	defer os.Remove(tmp)
	if err := os.Rename(tmp, current); err != nil {
		return err
	}
	return m.writeStatusFile(name, artifactDir, logPath)
}

func (m *Manager) publishPXECompatibilityFiles(artifactDir string) error {
	linuxDir := filepath.Join(m.filesDir, "linux")
	if err := os.MkdirAll(linuxDir, 0o755); err != nil {
		return err
	}
	for _, name := range []string{"boot-kernel", "boot-initrd", "rootfs.squashfs"} {
		dst := filepath.Join(linuxDir, name)
		tmp := dst + ".tmp"
		_ = os.Remove(tmp)
		if err := os.Symlink(filepath.Join(artifactDir, name), tmp); err != nil {
			return err
		}
		defer os.Remove(tmp)
		if err := os.Rename(tmp, dst); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) currentArtifactDir(name string) (string, bool) {
	current := filepath.Join(m.dataDir, "artifacts", name, "current")
	resolved, err := filepath.EvalSymlinks(current)
	if err != nil {
		return "", false
	}
	for _, file := range []string{"boot-kernel", "boot-initrd", "rootfs.squashfs"} {
		if st, err := os.Stat(filepath.Join(resolved, file)); err != nil || st.IsDir() || st.Size() == 0 {
			return "", false
		}
	}
	return resolved, true
}

func (m *Manager) writeStatusFile(name, artifactDir, logPath string) error {
	st := Status{
		Name:        name,
		Phase:       PhaseReady,
		ArtifactDir: artifactDir,
		LogPath:     logPath,
		KernelPath:  filepath.Join(artifactDir, "boot-kernel"),
		InitrdPath:  filepath.Join(artifactDir, "boot-initrd"),
		RootFSPath:  filepath.Join(artifactDir, "rootfs.squashfs"),
		UpdatedAt:   m.now(),
	}
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(artifactDir, "status.json"), append(raw, '\n'), 0o644)
}

func readSavedStatus(path, expectedName string) (Status, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Status{}, false
	}
	var st Status
	if err := json.Unmarshal(raw, &st); err != nil {
		return Status{}, false
	}
	if st.Name != expectedName {
		return Status{}, false
	}
	return st, true
}

func removeAllWritable(path string) {
	_ = filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			_ = os.Chmod(p, 0o755)
		}
		return nil
	})
	_ = os.RemoveAll(path)
}
