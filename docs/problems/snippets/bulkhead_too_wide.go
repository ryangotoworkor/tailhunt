//go:build ignore

// First version of the bulkhead in placeHold (internal/inventory/hold.go),
// replaced in step 05: the slot was taken before the quote call, so with
// quote outside the transaction a hold kept its slot for the whole 300 ms
// network call. Two slots then meant ~7 holds a second and 72% of holds
// failed with 429.
package snippets

// @learn:bulkhead-too-wide
func (s *Server) placeHold(ctx context.Context, req HoldRequest) (Hold, error) {
	cfg := s.Config()
	if cfg.Bulkhead {
		release, err := s.holdSlots.Acquire(ctx, holdAcquireTimeout)
		if err != nil {
			return Hold{}, ErrBusy
		}
		defer release()
	}
	if cfg.QuoteOutsideTx {
		return s.holdQuoteFirst(ctx, req, cfg) // calls quote while holding the slot
	}
	return s.holdQuoteInsideTx(ctx, req)
}

// @learn:end bulkhead-too-wide
