package inventory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"

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
	case err != nil:
		platform.Error(w, http.StatusBadGateway, err.Error())
	default:
		platform.JSON(w, http.StatusCreated, h)
	}
}

func (s *Server) placeHold(ctx context.Context, req HoldRequest) (Hold, error) {
	return s.holdQuoteInsideTx(ctx, req)
}

// holdQuoteInsideTx is the first version. It locks the room-night row,
// asks quote for the price *while the transaction is open*, then writes.
// The reasoning was "price and stock must be consistent". The cost: the
// pooled connection stays checked out for the whole quote round trip, and
// Postgres shows the session as "idle in transaction" while we wait.
// @learn:hold-legacy
func (s *Server) holdQuoteInsideTx(ctx context.Context, req HoldRequest) (Hold, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
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
