package pgtest

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"
)

// Eight callers race to create the same brand-new database, the way the
// test packages do on CI's empty Postgres. Every one of them must succeed.
// @learn:create-db-test
func TestEnsureDBConcurrentCallersAllSucceed(t *testing.T) {
	DSN(t, "postgres", "tailhunt-pgtest") // skips (or fails under TAILHUNT_REQUIRE_PG) without Postgres
	admin, err := sql.Open("pgx", Server())
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	name := fmt.Sprintf("tailhunt_race_%d", time.Now().UnixNano())
	t.Cleanup(func() { _, _ = admin.Exec(fmt.Sprintf(`DROP DATABASE IF EXISTS %q`, name)) })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	start := make(chan struct{})
	errs := make([]error, 8)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs[i] = ensureDB(ctx, admin, name)
		}()
	}
	close(start)
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("caller %d: %v", i, err)
		}
	}
}

// @learn:end create-db-test
