package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	infrasql "github.com/sugaf1204/gomi/internal/infra/sql"
)

func TestRunSetupAdminCreatesFirstAdmin(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "gomi.db")
	passwordPath := filepath.Join(t.TempDir(), "password")
	if err := os.WriteFile(passwordPath, []byte("secret123\n"), 0o600); err != nil {
		t.Fatalf("write password file: %v", err)
	}

	code := runSetupAdmin([]string{
		"--db-driver=sqlite",
		"--db-dsn=" + dbPath,
		"--username=owner",
		"--password-file=" + passwordPath,
	})
	if code != 0 {
		t.Fatalf("expected setup admin to succeed, got exit code %d", code)
	}

	backend, err := infrasql.New("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer backend.Close()
	user, err := backend.Auth().GetUser(context.Background(), "owner")
	if err != nil {
		t.Fatalf("get created user: %v", err)
	}
	if user.Role != "admin" {
		t.Fatalf("expected admin role, got %q", user.Role)
	}
}

func TestRunSetupAdminIgnoreAlreadyConfiguredAcceptsCompletedSetup(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "gomi.db")
	passwordPath := filepath.Join(t.TempDir(), "password")
	if err := os.WriteFile(passwordPath, []byte("secret123\n"), 0o600); err != nil {
		t.Fatalf("write password file: %v", err)
	}

	args := []string{
		"--db-driver=sqlite",
		"--db-dsn=" + dbPath,
		"--username=owner",
		"--password-file=" + passwordPath,
	}
	if code := runSetupAdmin(args); code != 0 {
		t.Fatalf("initial setup failed with exit code %d", code)
	}
	args = append(args, "--ignore-already-configured")
	if code := runSetupAdmin(args); code != 0 {
		t.Fatalf("expected idempotent setup to succeed, got exit code %d", code)
	}
}
