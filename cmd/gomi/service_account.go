package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/sugaf1204/gomi/internal/auth"
	"github.com/sugaf1204/gomi/internal/infra/config"
	infrasql "github.com/sugaf1204/gomi/internal/infra/sql"
	"github.com/sugaf1204/gomi/internal/resource"
)

// This local administration command requires OS access to the configured DB.
// Tokens never go to terminal output, process arguments or service logs.
func runServiceAccountCommand(args []string) int {
	if len(args) == 0 || args[0] != "create" {
		fmt.Fprintln(os.Stderr, "usage: gomi service-account create --name NAME --role operator --token-file PATH [--config PATH]")
		return 2
	}
	args = args[1:]
	configPath := configPathFromArgs(args, os.Getenv("GOMI_CONFIG"))
	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot load GOMI configuration")
		return 1
	}
	fs := flag.NewFlagSet("service-account create", flag.ContinueOnError)
	fs.String("config", configPath, "YAML configuration file")
	driver := fs.String("db-driver", cfg.DBDriver, "database driver")
	dsn := fs.String("db-dsn", cfg.DBDsn, "database DSN")
	name := fs.String("name", "", "service account name")
	role := fs.String("role", "operator", "viewer, operator or admin")
	path := fs.String("token-file", "", "private token file (required)")
	if err = fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 || strings.TrimSpace(*name) == "" || *path == "" || (*role != "viewer" && *role != "operator" && *role != "admin") {
		fmt.Fprintln(os.Stderr, "name, token-file and a valid role are required")
		return 2
	}
	backend, err := infrasql.New(*driver, *dsn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot open GOMI database")
		return 1
	}
	defer backend.Close()
	if err = backend.Migrate(); err != nil {
		fmt.Fprintln(os.Stderr, "cannot migrate GOMI database")
		return 1
	}
	changed, err := ensureServiceAccountFile(context.Background(), backend.Auth(), strings.TrimSpace(*name), auth.Role(*role), *path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if changed {
		fmt.Fprintf(os.Stdout, "created service account %s; token saved to private file\n", strings.TrimSpace(*name))
	} else {
		fmt.Fprintf(os.Stdout, "service account %s already configured\n", strings.TrimSpace(*name))
	}
	return 0
}

var serviceTokenPattern = regexp.MustCompile(`^gomi_sa_[a-f0-9]{64}$`)

func ensureServiceAccountFile(ctx context.Context, store *infrasql.AuthStore, name string, role auth.Role, path string) (bool, error) {
	parent, err := os.Stat(filepath.Dir(path))
	if err != nil || !parent.IsDir() || parent.Mode().Perm()&0022 != 0 {
		return false, fmt.Errorf("token-file parent must exist and not be writable by group or others")
	}
	existing, lookupErr := store.GetServiceAccount(ctx, name)
	if lookupErr != nil && !errors.Is(lookupErr, resource.ErrNotFound) {
		return false, fmt.Errorf("cannot read service account")
	}
	info, statErr := os.Lstat(path)
	var token string
	if statErr == nil {
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 128 {
			return false, fmt.Errorf("existing token file must be a private regular file")
		}
		file, e := os.Open(path)
		if e != nil {
			return false, fmt.Errorf("cannot read token file")
		}
		value, e := io.ReadAll(io.LimitReader(file, 129))
		file.Close()
		if e != nil {
			return false, fmt.Errorf("cannot read token file")
		}
		token = strings.TrimSpace(string(value))
		if !serviceTokenPattern.MatchString(token) {
			return false, fmt.Errorf("invalid local service account token file")
		}
	} else if errors.Is(statErr, os.ErrNotExist) {
		if lookupErr == nil {
			return false, fmt.Errorf("account already exists; supply its existing token file instead of implicitly rotating it")
		}
		token, err = auth.GenerateServiceAccountToken()
		if err != nil {
			return false, fmt.Errorf("cannot generate token")
		}
		file, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return false, fmt.Errorf("cannot create private token file")
		}
		_, e = io.WriteString(file, token+"\n")
		if e == nil {
			e = file.Sync()
		}
		closeErr := file.Close()
		if e != nil || closeErr != nil {
			return false, fmt.Errorf("cannot persist token file")
		}
	} else {
		return false, fmt.Errorf("cannot inspect token file")
	}
	hash := auth.ServiceAccountTokenHash(token)
	if lookupErr == nil {
		if existing.Role != role || existing.TokenHash != hash {
			return false, fmt.Errorf("existing account role or token differs; refusing implicit rotation")
		}
		return false, nil
	}
	// If interrupted after file creation but before insertion, the same private
	// file is reused on retry. Name uniqueness protects concurrent CLI invocations.
	account := auth.ServiceAccount{Name: name, Role: role, TokenHash: hash, CreatedAt: time.Now().UTC()}
	if err = store.InsertServiceAccount(ctx, account); err != nil {
		if winner, e := store.GetServiceAccount(ctx, name); e == nil && winner.TokenHash == hash && winner.Role == role {
			return false, nil
		}
		return false, fmt.Errorf("cannot create service account without replacing existing credentials")
	}
	if err = store.CreateAuditEvent(ctx, auth.AuditEvent{ID: fmt.Sprintf("local-service-account-%d", time.Now().UnixNano()), Action: "create-service-account", Actor: "local-admin", Result: "success", Message: "token issued to private local file", Details: map[string]string{"name": name, "role": string(role)}, CreatedAt: time.Now().UTC()}); err != nil {
		return true, fmt.Errorf("account created but audit event could not be recorded")
	}
	return true, nil
}
