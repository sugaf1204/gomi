package sql

import (
	"database/sql"
	"fmt"
	"runtime"
	"strings"
)

// Dialect represents the SQL dialect (SQLite or PostgreSQL).
type Dialect int

const (
	DialectSQLite Dialect = iota
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

// sqliteIsMemory reports whether the DSN refers to an in-memory SQLite
// database. Each connection to an in-memory database gets its own private
// data, so the pool must be limited to a single connection for it to behave
// like a normal database.
func sqliteIsMemory(dsn string) bool {
	return dsn == "" || strings.Contains(dsn, ":memory:") || strings.Contains(dsn, "mode=memory")
}

// sqliteDSN augments a SQLite DSN with the pragmas gomi relies on so they apply
// to every pooled connection (modernc.org/sqlite evaluates _pragma query
// parameters on each new connection, unlike a one-off PRAGMA exec which only
// touches a single connection). foreign_keys preserves referential integrity;
// busy_timeout makes a connection wait for the WAL writer lock instead of
// failing with SQLITE_BUSY once more than one connection is allowed; WAL lets
// readers run concurrently with each other and the single writer.
func sqliteDSN(dsn string) string {
	pragmas := []string{"_pragma=busy_timeout(5000)", "_pragma=foreign_keys(1)"}
	if !sqliteIsMemory(dsn) {
		pragmas = append(pragmas, "_pragma=journal_mode(WAL)")
	}
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	return dsn + sep + strings.Join(pragmas, "&")
}

// Init applies dialect-specific initialization to the database connection.
// dsn is the (already pragma-augmented) connection string, used to size the
// connection pool.
func (d Dialect) Init(db *sql.DB, dsn string) error {
	if d == DialectSQLite {
		if sqliteIsMemory(dsn) {
			// Each connection to an in-memory DB is a separate database.
			db.SetMaxOpenConns(1)
			return nil
		}
		// File-backed SQLite in WAL mode supports concurrent readers (plus a
		// single writer), so allow a pool. The dashboard's list endpoints are
		// read-only, so this lets concurrent page loads run in parallel instead
		// of serializing through one connection.
		conns := runtime.NumCPU()
		if conns < 4 {
			conns = 4
		}
		db.SetMaxOpenConns(conns)
	}
	return nil
}
