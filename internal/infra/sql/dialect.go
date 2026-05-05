package sql

import (
	"database/sql"
	"fmt"
	"strings"
)

// Dialect represents the SQL dialect (SQLite or PostgreSQL).
type Dialect int

const (
	DialectSQLite   Dialect = iota
	DialectPostgres
)

// Rebind converts ?-style placeholders to $1,$2,... for PostgreSQL.
// SQLite queries are returned unchanged.
func (d Dialect) Rebind(query string) string {
	if d == DialectSQLite {
		return query
	}
	var b strings.Builder
	n := 1
	for _, c := range query {
		if c == '?' {
			fmt.Fprintf(&b, "$%d", n)
			n++
		} else {
			b.WriteRune(c)
		}
	}
	return b.String()
}

// Init applies dialect-specific initialization to the database connection.
func (d Dialect) Init(db *sql.DB) error {
	if d == DialectSQLite {
		db.SetMaxOpenConns(1)
		if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
			return err
		}
		if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
			return err
		}
	}
	return nil
}
