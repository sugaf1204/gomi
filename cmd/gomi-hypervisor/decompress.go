package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func decompressZstd(ctx context.Context, compressedPath, destPath string) error {
	return decompressWithCommand(ctx, "zstd", []string{"-dc", compressedPath}, compressedPath, destPath)
}

func decompressXZ(ctx context.Context, compressedPath, destPath string) error {
	return decompressWithCommand(ctx, "xz", []string{"-dc", compressedPath}, compressedPath, destPath)
}

func decompressWithCommand(ctx context.Context, command string, args []string, compressedPath, destPath string) error {
	tmpPath := destPath + ".tmp"
	out, err := os.Create(tmpPath)
	if err != nil {
		return err
	}
	defer os.Remove(tmpPath)
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Stdout = out
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	closeErr := out.Close()
	if runErr != nil {
		return fmt.Errorf("%s decompress %s: %w: %s", command, compressedPath, runErr, strings.TrimSpace(stderr.String()))
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(tmpPath, destPath)
}
