package main

import (
	"os"
	"path/filepath"
	"strings"
)

var managedExtensions = map[string]bool{
	".qcow2":    true,
	".iso":      true,
	".img":      true,
	".squashfs": true,
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
