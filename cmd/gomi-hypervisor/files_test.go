package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAtomicWriteFromReader(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "test.bin")

	r := strings.NewReader("hello world")
	if err := atomicWriteFromReader(dest, r); err != nil {
		t.Fatalf("atomicWriteFromReader: %v", err)
	}

	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != "hello world" {
		t.Fatalf("unexpected content: %q", data)
	}

	if _, err := os.Stat(dest + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("temp file should not exist after successful write")
	}
}

func TestVerifyChecksum_Valid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.bin")
	content := []byte("test data for checksum")
	os.WriteFile(path, content, 0o644)

	h := sha256.Sum256(content)
	expected := hex.EncodeToString(h[:])

	if err := verifyChecksum(path, expected); err != nil {
		t.Fatalf("verifyChecksum should pass: %v", err)
	}
}

func TestVerifyChecksum_WithPrefix(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.bin")
	content := []byte("test data")
	os.WriteFile(path, content, 0o644)

	h := sha256.Sum256(content)
	expected := "sha256:" + hex.EncodeToString(h[:])

	if err := verifyChecksum(path, expected); err != nil {
		t.Fatalf("verifyChecksum with sha256: prefix should pass: %v", err)
	}
}

func TestVerifyChecksum_Mismatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.bin")
	os.WriteFile(path, []byte("some data"), 0o644)

	if err := verifyChecksum(path, "0000000000000000000000000000000000000000000000000000000000000000"); err == nil {
		t.Fatal("verifyChecksum should fail for wrong checksum")
	}
}

func TestNeedsDownload_NoFile(t *testing.T) {
	if !needsDownload("/nonexistent/path", "") {
		t.Fatal("should need download when file does not exist")
	}
}

func TestNeedsDownload_ExistingNoChecksum(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.qcow2")
	os.WriteFile(path, []byte("data"), 0o644)

	if needsDownload(path, "") {
		t.Fatal("should not need download when file exists and no checksum")
	}
}

func TestNeedsDownload_ChecksumMatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.qcow2")
	content := []byte("exact content")
	os.WriteFile(path, content, 0o644)

	h := sha256.Sum256(content)
	checksum := hex.EncodeToString(h[:])

	if needsDownload(path, checksum) {
		t.Fatal("should not need download when checksum matches")
	}
}

func TestNeedsDownload_ChecksumMismatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.qcow2")
	os.WriteFile(path, []byte("old content"), 0o644)

	if !needsDownload(path, "0000000000000000000000000000000000000000000000000000000000000000") {
		t.Fatal("should need download when checksum mismatches")
	}
}
