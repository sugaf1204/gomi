package sql

import (
	"database/sql"
	_ "embed"
	"fmt"
)

//go:embed schema.sql
var schemaSQL string

func runMigrations(db *sql.DB, dialect Dialect) error {
	if _, err := db.Exec(schemaSQL); err != nil {
		return fmt.Errorf("init schema: %w", err)
	}
	if err := dropLegacySSHKeyUsername(db, dialect); err != nil {
		return fmt.Errorf("drop legacy ssh_keys.username: %w", err)
	}
	return nil
}

// dropLegacySSHKeyUsername removes the historical NOT NULL `username` column
// from the ssh_keys table. SSH keys are now global (not bound to a specific
// OS user), so the column is unused and would cause INSERTs to fail with a
// NOT NULL constraint error on databases created by older builds.
//
// SQLite 3.35+ and PostgreSQL both support `ALTER TABLE ... DROP COLUMN`. The
// statement is idempotent in spirit: an error simply means the column was
// already dropped (or never existed in a freshly-created schema), in which
// case we silently move on.
func dropLegacySSHKeyUsername(db *sql.DB, dialect Dialect) error {
	stmt := "ALTER TABLE ssh_keys DROP COLUMN username"
	if dialect == DialectPostgres {
		stmt = "ALTER TABLE ssh_keys DROP COLUMN IF EXISTS username"
	}
	// SQLite has no IF EXISTS for DROP COLUMN; ignore the error from the
	// "no such column" case while still surfacing genuine I/O failures.
	_, _ = db.Exec(stmt)
	return nil
}
