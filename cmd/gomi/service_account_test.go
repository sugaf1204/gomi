package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sugaf1204/gomi/internal/auth"
	infrasql "github.com/sugaf1204/gomi/internal/infra/sql"
)

func TestLocalServiceAccountIsPrivateAndIdempotent(t *testing.T) {
	directory := t.TempDir()
	database := filepath.Join(directory, "gomi.db")
	tokenFile := filepath.Join(directory, "token")
	args := []string{"create", "--db-driver=sqlite", "--db-dsn=" + database, "--name=capi", "--role=operator", "--token-file=" + tokenFile}
	if code := runServiceAccountCommand(args); code != 0 {
		t.Fatalf("create exit code: %d", code)
	}
	token, err := os.ReadFile(tokenFile)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(tokenFile)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatal("token file not private")
	}
	if code := runServiceAccountCommand(args); code != 0 {
		t.Fatalf("retry exit code: %d", code)
	}
	after, err := os.ReadFile(tokenFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(token) {
		t.Fatal("retry rotated token")
	}
	backend, err := infrasql.New("sqlite", database)
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	account, err := backend.Auth().GetServiceAccountByTokenHash(context.Background(), auth.ServiceAccountTokenHash(strings.TrimSpace(string(token))))
	if err != nil || account.Role != auth.RoleOperator || account.Name != "capi" {
		t.Fatal("generated token does not identify operator service account")
	}
	if _, err := ensureServiceAccountFile(context.Background(), backend.Auth(), "capi", auth.RoleAdmin, tokenFile); err == nil {
		t.Fatal("implicit role escalation accepted")
	}
	if err = os.Remove(tokenFile); err != nil {
		t.Fatal(err)
	}
	if code := runServiceAccountCommand(args); code == 0 {
		t.Fatal("missing token file caused implicit rotation")
	}
	unchanged, err := backend.Auth().GetServiceAccount(context.Background(), "capi")
	if err != nil || unchanged.TokenHash != account.TokenHash {
		t.Fatal("existing account was changed")
	}
}
func TestLocalServiceAccountRecoversUncommittedTokenFile(t *testing.T) {
	directory := t.TempDir()
	backend, err := infrasql.New("sqlite", filepath.Join(directory, "gomi.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	if err = backend.Migrate(); err != nil {
		t.Fatal(err)
	}
	token, err := auth.GenerateServiceAccountToken()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "token")
	if err = os.WriteFile(path, []byte(token+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	changed, err := ensureServiceAccountFile(context.Background(), backend.Auth(), "capi", auth.RoleOperator, path)
	if err != nil || !changed {
		t.Fatalf("recover interrupted file creation: %v", err)
	}
	if err = os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err = ensureServiceAccountFile(context.Background(), backend.Auth(), "capi", auth.RoleOperator, path); err == nil {
		t.Fatal("world-readable credential file accepted")
	}
	if err = os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(directory, "link")
	if err = os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err = ensureServiceAccountFile(context.Background(), backend.Auth(), "capi", auth.RoleOperator, link); err == nil {
		t.Fatal("symlink credential accepted")
	}
}
