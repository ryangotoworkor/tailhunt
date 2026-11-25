package inventory

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // database/sql driver "pgx"
)

// Open returns a database/sql pool with a hard cap on open connections.
// The cap is the whole story of this repo: when every one of the maxOpen
// connections is busy, the next caller waits inside db.Conn / QueryContext
// until one is returned.
// @learn:pool-open
func Open(dsn string, maxOpen int) (*sql.DB, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(maxOpen)
	db.SetConnMaxIdleTime(5 * time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for {
		if err = db.PingContext(ctx); err == nil {
			return db, nil
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("postgres not reachable: %w", err)
		case <-time.After(300 * time.Millisecond):
		}
	}
}

// @learn:end pool-open

// The schema and seed are idempotent: every service start runs them, so a
// brand-new volume and an old one end up in the same state.
const schema = `
CREATE TABLE IF NOT EXISTS hotels (
  id      int PRIMARY KEY,
  name    text NOT NULL,
  city    text NOT NULL,
  stars   int  NOT NULL,
  partner text NOT NULL
);
CREATE INDEX IF NOT EXISTS hotels_city ON hotels (city);
CREATE TABLE IF NOT EXISTS room_nights (
  hotel_id         int  NOT NULL REFERENCES hotels (id),
  night            date NOT NULL,
  base_price_cents int  NOT NULL,
  rooms_left       int  NOT NULL CHECK (rooms_left >= 0),
  PRIMARY KEY (hotel_id, night)
);
CREATE TABLE IF NOT EXISTS holds (
  id          bigserial PRIMARY KEY,
  hotel_id    int  NOT NULL,
  night       date NOT NULL,
  guest       text NOT NULL,
  price_cents int  NOT NULL,
  status      text NOT NULL DEFAULT 'held',
  created_at  timestamptz NOT NULL DEFAULT now(),
  expires_at  timestamptz NOT NULL
);
CREATE INDEX IF NOT EXISTS holds_open ON holds (expires_at) WHERE status = 'held';
`

// Seed: 5 cities x 40 hotels, 60 nights from 2026-12-01, 400 rooms a night.
const seed = `
INSERT INTO hotels (id, name, city, stars, partner)
SELECT g,
       (ARRAY['Lantern','Harbor','Cedar','Maple','Willow','Summit','Garden','River'])[1 + g % 8] || ' ' ||
       (ARRAY['Inn','House','Lodge','Suites','Hotel'])[1 + (g / 8) % 5] || ' ' || g,
       (ARRAY['hangzhou','chengdu','xian','kyoto','lisbon'])[1 + g % 5],
       2 + g % 4,
       'p' || (g % 7)
FROM generate_series(1, 200) g
ON CONFLICT DO NOTHING;
INSERT INTO room_nights (hotel_id, night, base_price_cents, rooms_left)
SELECT h, DATE '2026-12-01' + d, 30000 + (h * 7919) % 42000, 400
FROM generate_series(1, 200) h, generate_series(0, 59) d
ON CONFLICT DO NOTHING;
`

// Migrate creates the schema and seeds it if needed.
func Migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("schema: %w", err)
	}
	if _, err := db.ExecContext(ctx, seed); err != nil {
		return fmt.Errorf("seed: %w", err)
	}
	return nil
}
