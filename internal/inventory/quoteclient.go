package inventory

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/ryangotoworkor/tailhunt/internal/platform"
)

// QuoteClient asks the quote service for the final price of a room night.
type QuoteClient struct {
	URL  string
	HTTP *http.Client
}

// Price returns the price in cents for one night for one tier.
func (q *QuoteClient) Price(ctx context.Context, hotelID int, night, tier string) (int, error) {
	var out struct {
		PriceCents int `json:"price_cents"`
	}
	u := fmt.Sprintf("%s/quote?hotel=%d&night=%s&tier=%s", q.URL, hotelID, url.QueryEscape(night), url.QueryEscape(tier))
	if err := platform.GetJSON(ctx, q.HTTP, u, &out); err != nil {
		return 0, fmt.Errorf("quote: %w", err)
	}
	return out.PriceCents, nil
}
