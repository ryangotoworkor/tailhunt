//go:build ignore

package pgtest

// pgtest.DSN until the first CI run on GitHub (docs/problems.md, q10):
// check, then create. Two test packages running at once on an empty
// Postgres both pass the check and both CREATE DATABASE.
func dsnBefore() {
	// @learn:create-db-before
	var exists bool
	if err := admin.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)`, name).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if !exists {
		if _, err := admin.ExecContext(ctx, fmt.Sprintf(`CREATE DATABASE %q`, name)); err != nil {
			t.Fatal(err)
		}
	}
	// @learn:end create-db-before
}
