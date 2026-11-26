// Package pgtest gives tests their own Postgres database (tailhunt_test by
// default) on the compose Postgres, so a test run never touches the data
// the demo stack is serving. TAILHUNT_TEST_PG overrides the server URL.
package pgtest

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// Server is the admin URL of the Postgres server used by tests.
func Server() string {
	if v := os.Getenv("TAILHUNT_TEST_PG"); v != "" {
		return v
	}
	return "postgres://tailhunt:tailhunt@localhost:54389/tailhunt"
}

// DSN returns a DSN for database name on the test server, creating the
// database if it does not exist. It skips the test when Postgres is not
// reachable unless TAILHUNT_REQUIRE_PG=1 (CI sets it, so CI never skips).
func DSN(t testing.TB, name, app string) string {
	t.Helper()
	admin, err := sql.Open("pgx", Server())
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := admin.PingContext(ctx); err != nil {
		if os.Getenv("TAILHUNT_REQUIRE_PG") == "1" {
			t.Fatalf("postgres required but not reachable: %v", err)
		}
		t.Skipf("postgres not reachable (%v); start it with docker-compose -p tailhunt up -d postgres", err)
	}
	if err := ensureDB(ctx, admin, name); err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(Server())
	u.Path = "/" + name
	q := u.Query()
	q.Set("application_name", app)
	u.RawQuery = q.Encode()
	return u.String()
}

// @learn:create-db
// ensureDB creates the database unless it exists. go test ./... runs
// packages in parallel processes, and on a fresh Postgres (CI) two of
// them can both find no database and both create it; the loser gets a
// duplicate-key error. Losing that race is fine as long as the database
// exists afterwards.
func ensureDB(ctx context.Context, admin *sql.DB, name string) error {
	exists := func() (bool, error) {
		var ok bool
		err := admin.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)`, name).Scan(&ok)
		return ok, err
	}
	if ok, err := exists(); err != nil || ok {
		return err
	}
	if _, err := admin.ExecContext(ctx, fmt.Sprintf(`CREATE DATABASE %q`, name)); err != nil {
		if ok, err2 := exists(); err2 == nil && ok {
			return nil // another process created it first
		}
		return err
	}
	return nil
}

// @learn:end create-db
