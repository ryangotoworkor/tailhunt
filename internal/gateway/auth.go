package gateway

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/ryangotoworkor/tailhunt/internal/platform"
)

// Claims is what the gateway puts in a token.
type Claims struct {
	Role string `json:"role"` // oncall | guest
	Tier string `json:"tier"`
	jwt.RegisteredClaims
}

// DevSecret is only for local runs; compose and CI pass JWT_SECRET.
const DevSecret = "dev-only-not-a-real-secret"

// Accounts are demo logins: oncall / oncall-demo (can use the ops console)
// and guest / guest-demo (can only search and hold).
const accountsSchema = `
CREATE TABLE IF NOT EXISTS accounts (
  name    text PRIMARY KEY,
  pw_hash text NOT NULL,
  role    text NOT NULL,
  tier    text NOT NULL
);`

// MigrateAccounts creates the accounts table and the two demo logins.
func MigrateAccounts(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, accountsSchema); err != nil {
		return err
	}
	for _, a := range []struct{ name, pw, role, tier string }{
		{"oncall", "oncall-demo", "oncall", "gold"},
		{"guest", "guest-demo", "guest", "basic"},
	} {
		var n int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM accounts WHERE name = $1`, a.name).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		h, err := bcrypt.GenerateFromPassword([]byte(a.pw), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO accounts VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING`, a.name, string(h), a.role, a.tier); err != nil {
			return err
		}
	}
	return nil
}

// Issue signs a token for user.
func (s *Server) Issue(user, role, tier string, ttl time.Duration) (string, error) {
	c := Claims{Role: role, Tier: tier, RegisteredClaims: jwt.RegisteredClaims{
		Subject: user, IssuedAt: jwt.NewNumericDate(time.Now()), ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl)),
	}}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(s.Secret)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var in struct{ Username, Password string }
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Username == "" {
		platform.Error(w, http.StatusBadRequest, "username and password are required")
		return
	}
	var hash, role, tier string
	err := s.Accounts.QueryRowContext(r.Context(), `SELECT pw_hash, role, tier FROM accounts WHERE name = $1`, in.Username).Scan(&hash, &role, &tier)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && bcrypt.CompareHashAndPassword([]byte(hash), []byte(in.Password)) != nil) {
		platform.Error(w, http.StatusUnauthorized, "wrong username or password")
		return
	} else if err != nil {
		platform.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	tok, err := s.Issue(in.Username, role, tier, 12*time.Hour)
	if err != nil {
		platform.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	platform.JSON(w, http.StatusOK, map[string]string{"token": tok, "user": in.Username, "role": role, "tier": tier})
}

type ctxKey struct{}

// ClaimsFrom returns the caller's claims (set by authed).
func ClaimsFrom(ctx context.Context) *Claims { c, _ := ctx.Value(ctxKey{}).(*Claims); return c }

// authed rejects requests without a valid bearer token; role "" means any role.
// @learn:jwt-check
func (s *Server) authed(role string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok {
			platform.Error(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		c := &Claims{}
		_, err := jwt.ParseWithClaims(raw, c, func(*jwt.Token) (any, error) { return s.Secret, nil }, jwt.WithValidMethods([]string{"HS256"}))
		if err != nil {
			platform.Error(w, http.StatusUnauthorized, "invalid token")
			return
		}
		if role != "" && c.Role != role {
			platform.Error(w, http.StatusForbidden, "needs role "+role)
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, c)))
	}
}

// @learn:end jwt-check
