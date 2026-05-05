package sql

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

// Backend provides SQL-based storage for all GOMI resources.
type Backend struct {
	db      *sql.DB
	dialect Dialect
}

// New creates a new SQL backend with the given driver and DSN.
// Supported drivers: "sqlite", "postgres".
func New(driver, dsn string) (*Backend, error) {
	var dialect Dialect
	var driverName string

	switch driver {
	case "postgres", "postgresql":
		dialect = DialectPostgres
		driverName = "pgx"
	case "sqlite", "sqlite3":
		dialect = DialectSQLite
		driverName = "sqlite"
	default:
		return nil, fmt.Errorf("unsupported db driver: %s", driver)
	}

	db, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, err
	}

	if err := dialect.Init(db); err != nil {
		db.Close()
		return nil, err
	}

	return &Backend{db: db, dialect: dialect}, nil
}

// Migrate runs pending database migrations.
func (b *Backend) Migrate() error {
	return runMigrations(b.db, b.dialect)
}

// Health checks database connectivity.
func (b *Backend) Health(ctx context.Context) error {
	return b.db.PingContext(ctx)
}

// Close closes the database connection.
func (b *Backend) Close() error {
	return b.db.Close()
}

// Store accessors

func (b *Backend) Machines() *MachineStore       { return &MachineStore{b: b} }
func (b *Backend) Subnets() *SubnetStore         { return &SubnetStore{b: b} }
func (b *Backend) Auth() *AuthStore              { return &AuthStore{b: b} }
func (b *Backend) SSHKeys() *SSHKeyStore         { return &SSHKeyStore{b: b} }
func (b *Backend) HWInfo() *HWInfoStore          { return &HWInfoStore{b: b} }
func (b *Backend) Hypervisors() *HypervisorStore { return &HypervisorStore{b: b} }
func (b *Backend) HypervisorTokens() *RegTokenStore {
	return &RegTokenStore{b: b}
}
func (b *Backend) AgentTokens() *AgentTokenStore { return &AgentTokenStore{b: b} }
func (b *Backend) VMs() *VMStore             { return &VMStore{b: b} }
func (b *Backend) CloudInits() *CloudInitStore { return &CloudInitStore{b: b} }
func (b *Backend) OSImages() *OSImageStore     { return &OSImageStore{b: b} }
func (b *Backend) DHCPLeases() *DHCPLeaseStore { return &DHCPLeaseStore{b: b} }

// Query helpers that apply dialect-specific placeholder rebinding.

func (b *Backend) exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return b.db.ExecContext(ctx, b.dialect.Rebind(query), args...)
}

func (b *Backend) queryRow(ctx context.Context, query string, args ...any) *sql.Row {
	return b.db.QueryRowContext(ctx, b.dialect.Rebind(query), args...)
}

func (b *Backend) query(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return b.db.QueryContext(ctx, b.dialect.Rebind(query), args...)
}
