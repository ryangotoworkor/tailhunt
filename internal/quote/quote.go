// Package quote is the pricing service: the final price for one room night
// for one membership tier. Inventory calls it when a guest holds a room.
package quote

import (
	"net/http"
	"strconv"

	"github.com/ryangotoworkor/tailhunt/internal/platform"
)

// Quote is the price the guest will pay.
type Quote struct {
	HotelID    int    `json:"hotel_id"`
	Night      string `json:"night"`
	Tier       string `json:"tier"`
	PriceCents int    `json:"price_cents"`
}

var discount = map[string]int{"basic": 0, "silver": 5, "gold": 12} // percent

// Price is deterministic: base price from the hotel id and night, minus
// the tier discount.
func Price(hotelID int, night, tier string) int {
	base := 30000 + (hotelID*7919)%42000
	if len(night) == 10 && (night[8]-'0')%7 >= 5 { // a weekend-ish surcharge
		base += 4500
	}
	return base * (100 - discount[tier]) / 100
}

// Server is the quote service.
type Server struct{}

// Handler serves GET /quote?hotel=&night=&tier=.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", platform.Health)
	mux.HandleFunc("GET /quote", s.quote)
	return mux
}

func (s *Server) quote(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	id, err := strconv.Atoi(q.Get("hotel"))
	if err != nil || q.Get("night") == "" {
		platform.Error(w, http.StatusBadRequest, "hotel and night are required")
		return
	}
	tier := q.Get("tier")
	if tier == "" {
		tier = "basic"
	}
	platform.JSON(w, http.StatusOK, Quote{HotelID: id, Night: q.Get("night"), Tier: tier, PriceCents: Price(id, q.Get("night"), tier)})
}
