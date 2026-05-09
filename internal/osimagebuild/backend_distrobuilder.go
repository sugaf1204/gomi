package osimagebuild

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

type distrobuilderBackend struct {
	runner CommandRunner
}

func newRootFSBuilder(entry BuildEntry, runner CommandRunner) (rootFSBuilder, error) {
	switch normalizeBackend(entry.Backend) {
	case "distrobuilder":
		return distrobuilderBackend{runner: runner}, nil
	default:
		return nil, fmt.Errorf("%s: unsupported backend", entry.Backend)
	}
}

func (b distrobuilderBackend) BuildRootFS(ctx context.Context, req BuildRootFSRequest) (string, error) {
	if err := os.MkdirAll(req.WorkDir, 0o755); err != nil {
		return "", err
	}
	cacheDir := filepath.Join(req.WorkDir, "distrobuilder-cache")
	sourcesDir := filepath.Join(req.WorkDir, "distrobuilder-sources")
	if err := b.runner.Run(
		ctx,
		"distrobuilder",
		"--cache-dir", cacheDir,
		"build-dir",
		"--with-post-files",
		"--sources-dir", sourcesDir,
		req.DefinitionPath,
		req.RootFSDir,
	); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return "", fmt.Errorf("distrobuilder is required on the build host: %w", err)
		}
		return "", fmt.Errorf("build rootfs with distrobuilder: %w", err)
	}
	return req.RootFSDir, nil
}
