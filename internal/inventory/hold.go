package inventory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/ryangotoworkor/tailhunt/internal/obs"
	"github.com/ryangotoworkor/tailhunt/internal/platform"
)

// HoldRequest asks to keep one room for one night while the guest pays.
type HoldRequest struct {
	HotelID int    `json:"hotel_id"`
	Night   string `json:"night"` // YYYY-MM-DD
	Guest   string `json:"guest"`
	Tier    string `json:"tier"`
}

// Hold is a placed hold.
type Hold struct {
	ID         int64     `json:"id"`
	HotelID    int       `json:"hotel_id"`
	Night      string    `json:"night"`
	PriceCents int       `json:"price_cents"`
	ExpiresAt  time.Time `json:"expires_at"`
}

// ErrSoldOut means no room is left that night.
var ErrSoldOut = errors.New("sold out")

// ErrNoSuchNight means the hotel or night is not in stock at all.
var ErrNoSuchNight = errors.New("no such hotel night")

func (s *Server) hold(w http.ResponseWriter, r *http.Request) {
	var req HoldRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.HotelID <= 0 || req.Guest == "" {
		platform.Error(w, http.StatusBadRequest, "hotel_id, night and guest are required")
		return
	}
	if _, err := time.Parse("2006-01-02", req.Night); err != nil {
		platform.Error(w, http.StatusBadRequest, "night must be YYYY-MM-DD")
		return
	}
	h, err := s.placeHold(r.Context(), req)
	switch {
	case errors.Is(err, ErrSoldOut):
		platform.Error(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrNoSuchNight):
		platform.Error(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrBusy):
		platform.Error(w, http.StatusTooManyRequests, err.Error())
	case err != nil:
		platform.Error(w, http.StatusBadGateway, err.Error())
	default:
		platform.JSON(w, http.StatusCreated, h)
	}
}

// ErrBusy means the hold could not get a bulkhead slot or a connection in time.
var ErrBusy = errors.New("holds are busy, retry")

// @learn:hold-slot
func (s *Server) placeHold(ctx context.Context, req HoldRequest) (Hold, error) {
	cfg := s.Config()
	if cfg.QuoteOutsideTx {
		return s.holdQuoteFirst(ctx, req, cfg)
	}
	release, err := s.holdSlot(ctx, cfg)
	if err != nil {
		return Hold{}, err
	}
	defer release()
	return s.holdQuoteInsideTx(ctx, req)
}

// holdSlot takes one of the 2 hold slots when Bulkhead is on: a hold
// holds a connection only while it has a slot, so holds can never take
// more than 2 of the 6 connections.
func (s *Server) holdSlot(ctx context.Context, cfg Config) (func(), error) {
	if !cfg.Bulkhead {
		return func() {}, nil
	}
	release, err := s.holdSlots.Acquire(ctx, holdAcquireTimeout)
	if err != nil {
		return nil, ErrBusy
	}
	return release, nil
}

// @learn:end hold-slot

// price calls quote, behind a timeout and a breaker when QuoteGuard is on.
func (s *Server) price(ctx context.Context, req HoldRequest, cfg Config) (int, error) {
	if !cfg.QuoteGuard {
		return s.Quote.Price(ctx, req.HotelID, req.Night, req.Tier)
	}
	ctx, cancel := context.WithTimeout(ctx, quoteTimeout)
	defer cancel()
	var p int
	err := s.breaker.Do(func() (err error) {
		p, err = s.Quote.Price(ctx, req.HotelID, req.Night, req.Tier)
		return err
	})
	return p, err
}

// holdQuoteFirst is the root-cause fix: ask quote *before* touching the
// database, then do the stock change in one short transaction. The
// conditional UPDATE replaces SELECT ... FOR UPDATE: it locks, checks and
// decrements in one statement, so the row lock lives for microseconds.
// The price is still consistent: it depends on hotel, night and tier, not
// on how many rooms are left.
// @learn:hold-fixed
func (s *Server) holdQuoteFirst(ctx context.Context, req HoldRequest, cfg Config) (Hold, error) {
	price, err := s.price(ctx, req, cfg) // no connection held during the network call
	if err != nil {
		return Hold{}, err
	}
	release, err := s.holdSlot(ctx, cfg)
	if err != nil {
		return Hold{}, err
	}
	defer release()
	actx := ctx
	if cfg.AcquireTimeout {
		var cancel context.CancelFunc
		actx, cancel = context.WithTimeout(ctx, holdAcquireTimeout)
		defer cancel()
	}
	conn, err := s.acquire(actx, "hold")
	if err != nil {
		return Hold{}, ErrBusy
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	ctx, span := obs.Tracer().Start(ctx, "db.tx hold")
	defer span.End()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return Hold{}, err
	}
	defer tx.Rollback()
	var left int
	err = tx.QueryRowContext(ctx, `UPDATE room_nights SET rooms_left = rooms_left - 1
		WHERE hotel_id = $1 AND night = $2 AND rooms_left > 0 RETURNING rooms_left`, req.HotelID, req.Night).Scan(&left)
	if errors.Is(err, sql.ErrNoRows) {
		var exists bool
		_ = tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM room_nights WHERE hotel_id = $1 AND night = $2)`, req.HotelID, req.Night).Scan(&exists)
		if exists {
			return Hold{}, ErrSoldOut
		}
		return Hold{}, ErrNoSuchNight
	} else if err != nil {
		return Hold{}, err
	}
	h := Hold{HotelID: req.HotelID, Night: req.Night, PriceCents: price}
	err = tx.QueryRowContext(ctx, `INSERT INTO holds (hotel_id, night, guest, price_cents, expires_at)
		VALUES ($1, $2, $3, $4, now() + $5::interval) RETURNING id, expires_at`,
		req.HotelID, req.Night, req.Guest, price, s.holdTTL()).Scan(&h.ID, &h.ExpiresAt)
	if err != nil {
		return Hold{}, err
	}
	return h, tx.Commit()
}

// @learn:end hold-fixed

// holdQuoteInsideTx is the first version. It locks the room-night row,
// asks quote for the price *while the transaction is open*, then writes.
// The reasoning was "price and stock must be consistent". The cost: the
// pooled connection stays checked out for the whole quote round trip, and
// Postgres shows the session as "idle in transaction" while we wait.
// @learn:hold-legacy
func (s *Server) holdQuoteInsideTx(ctx context.Context, req HoldRequest) (Hold, error) {
	conn, err := s.acquire(ctx, "hold")
	if err != nil {
		return Hold{}, err
	}
	defer conn.Close()
	ctx, span := obs.Tracer().Start(ctx, "db.tx hold")
	defer span.End()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return Hold{}, err
	}
	defer tx.Rollback()
	var left int
	err = tx.QueryRowContext(ctx, `SELECT rooms_left FROM room_nights WHERE hotel_id = $1 AND night = $2 FOR UPDATE`, req.HotelID, req.Night).Scan(&left)
	if errors.Is(err, sql.ErrNoRows) {
		return Hold{}, ErrNoSuchNight
	} else if err != nil {
		return Hold{}, err
	}
	if left == 0 {
		return Hold{}, ErrSoldOut
	}
	price, err := s.Quote.Price(ctx, req.HotelID, req.Night, req.Tier) // network call, connection still held
	if err != nil {
		return Hold{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE room_nights SET rooms_left = rooms_left - 1 WHERE hotel_id = $1 AND night = $2`, req.HotelID, req.Night); err != nil {
		return Hold{}, err
	}
	h := Hold{HotelID: req.HotelID, Night: req.Night, PriceCents: price}
	err = tx.QueryRowContext(ctx, `INSERT INTO holds (hotel_id, night, guest, price_cents, expires_at)
		VALUES ($1, $2, $3, $4, now() + $5::interval) RETURNING id, expires_at`,
		req.HotelID, req.Night, req.Guest, price, s.holdTTL()).Scan(&h.ID, &h.ExpiresAt)
	if err != nil {
		return Hold{}, err
	}
	return h, tx.Commit()
}

// @learn:end hold-legacy

func (s *Server) holdTTL() string {
	if s.HoldTTL > 0 {
		return s.HoldTTL.String()
	}
	return "2m"
}
