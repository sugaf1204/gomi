package sql

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
	sqlite "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// pgUniqueViolation is the PostgreSQL SQLSTATE for unique_violation.
const pgUniqueViolation = "23505"

// isUniqueViolation reports whether err is a duplicate-key rejection from
// either supported driver. Both are checked by error type rather than message
// text, so a driver changing its wording cannot silently turn a 409 into a 500.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == pgUniqueViolation
	}

	var liteErr *sqlite.Error
	if errors.As(err, &liteErr) {
		// SQLite reports the primary-key conflict as an extended result code;
		// the primary code is SQLITE_CONSTRAINT.
		code := liteErr.Code()
		return code == sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY ||
			code == sqlite3.SQLITE_CONSTRAINT_UNIQUE ||
			code&0xff == sqlite3.SQLITE_CONSTRAINT
	}

	return false
}
