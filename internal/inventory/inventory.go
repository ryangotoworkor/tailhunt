// Package inventory owns the room stock in Postgres. It answers
// availability searches and places holds. Both share one database/sql
// pool, and that sharing is where the incident in this repo lives.
package inventory

import (
	"database/sql"
	"net/http"
	"strconv"
	"time"

	"github.com/ryangotoworkor/tailhunt/internal/obs"
	"github.com/ryangotoworkor/tailhunt/internal/platform"
)

// Hotel is one row of an availability answer.
type Hotel struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	Stars      int    `json:"stars"`
	PriceCents int    `json:"price_cents"` // cheapest night in the stay, before tier discount
	RoomsLeft  int    `json:"rooms_left"`  // fewest rooms left on any night of the stay
}

// Server is the inventory service.
type Server struct {
	DB      *sql.DB
	Quote   *QuoteClient
	HoldTTL time.Duration // how long an unpaid hold keeps the room (default 2m)
	Diag    *sql.DB       // one-connection pool for pg_stat_activity
	AppName string        // application_name of the main pool (default tailhunt-inventory)
}

// Handler mounts the routes.
func (s *Server) Handler() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", platform.Health)
	mux.HandleFunc("GET /availability", s.availability)
	mux.HandleFunc("POST /holds", s.hold)
	mux.HandleFunc("GET /debug/dbstats", s.dbstats)
	mux.HandleFunc("GET /debug/pg-activity", s.pgActivity)
	return mux
}

const availabilitySQL = `
SELECT h.id, h.name, h.stars, min(rn.base_price_cents), min(rn.rooms_left)
FROM hotels h JOIN room_nights rn ON rn.hotel_id = h.id
WHERE h.city = $1 AND rn.night >= $2::date AND rn.night < $2::date + $3::int AND rn.rooms_left > 0
GROUP BY h.id
HAVING count(*) = $3
ORDER BY h.id
LIMIT 50`

// ParseStay validates the common city / checkin / nights parameters.
func ParseStay(r *http.Request) (city string, checkin time.Time, nights int, msg string) {
	q := r.URL.Query()
	city = q.Get("city")
	checkin, err := time.Parse("2006-01-02", q.Get("checkin"))
	if city == "" || err != nil {
		return "", time.Time{}, 0, "city and checkin (YYYY-MM-DD) are required"
	}
	nights, err = strconv.Atoi(q.Get("nights"))
	if err != nil || nights < 1 || nights > 14 {
		return "", time.Time{}, 0, "nights must be 1..14"
	}
	return city, checkin, nights, ""
}

func (s *Server) availability(w http.ResponseWriter, r *http.Request) {
	city, checkin, nights, msg := ParseStay(r)
	if msg != "" {
		platform.Error(w, http.StatusBadRequest, msg)
		return
	}
	conn, err := s.acquire(r.Context(), "availability")
	if err != nil {
		platform.Error(w, http.StatusServiceUnavailable, "db pool: "+err.Error())
		return
	}
	defer conn.Close()
	ctx, span := obs.Tracer().Start(r.Context(), "db.query availability")
	defer span.End()
	rows, err := conn.QueryContext(ctx, availabilitySQL, city, checkin.Format("2006-01-02"), nights)
	if err != nil {
		platform.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	out := []Hotel{}
	for rows.Next() {
		var h Hotel
		if err := rows.Scan(&h.ID, &h.Name, &h.Stars, &h.PriceCents, &h.RoomsLeft); err != nil {
			platform.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		out = append(out, h)
	}
	if err := rows.Err(); err != nil {
		platform.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	platform.JSON(w, http.StatusOK, map[string]any{"hotels": out})
}
