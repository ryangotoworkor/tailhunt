package inventory

import (
	"context"
	"log/slog"
	"time"
)

// Sweep expires holds the guest never paid for and puts the rooms back.
// One statement, so a crash halfway cannot expire a hold without
// returning its room.
// @learn:sweeper
func Sweep(ctx context.Context, s *Server) (int64, error) {
	res, err := s.DB.ExecContext(ctx, `
WITH expired AS (
  UPDATE holds SET status = 'expired'
  WHERE status = 'held' AND expires_at < now()
  RETURNING hotel_id, night
), per_night AS (
  SELECT hotel_id, night, count(*) AS n FROM expired GROUP BY hotel_id, night
)
UPDATE room_nights rn SET rooms_left = rn.rooms_left + p.n
FROM per_night p WHERE rn.hotel_id = p.hotel_id AND rn.night = p.night`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// @learn:end sweeper

// RunSweeper calls Sweep every interval until ctx ends.
func RunSweeper(ctx context.Context, s *Server, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if n, err := Sweep(ctx, s); err != nil {
				slog.Warn("sweep failed", "err", err)
			} else if n > 0 {
				slog.Info("sweep", "room_nights_restored", n)
			}
		}
	}
}
