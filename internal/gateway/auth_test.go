package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ryangotoworkor/tailhunt/internal/inventory"
	"github.com/ryangotoworkor/tailhunt/internal/pgtest"
)

func testServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	db, err := inventory.Open(pgtest.DSN(t, "tailhunt_test", "tailhunt-test"), 2)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := MigrateAccounts(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	s := &Server{Accounts: db, Secret: []byte("test-secret"), Client: http.DefaultClient}
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	return s, srv
}

func login(t *testing.T, url, user, pw string) (int, string) {
	b, _ := json.Marshal(map[string]string{"username": user, "password": pw})
	resp, err := http.Post(url+"/api/login", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct{ Token string }
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out.Token
}

func get(t *testing.T, url, tok string) int {
	req, _ := http.NewRequest("GET", url, nil)
	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func TestLoginIssuesATokenOnlyForTheRightPassword(t *testing.T) {
	_, srv := testServer(t)
	if code, _ := login(t, srv.URL, "guest", "nope"); code != 401 {
		t.Fatalf("wrong password: %d", code)
	}
	if code, _ := login(t, srv.URL, "nobody", "x"); code != 401 {
		t.Fatalf("unknown user: %d", code)
	}
	code, tok := login(t, srv.URL, "guest", "guest-demo")
	if code != 200 || tok == "" {
		t.Fatalf("login %d", code)
	}
	if got := get(t, srv.URL+"/api/me", tok); got != 200 {
		t.Fatalf("/api/me with token: %d", got)
	}
}

func TestAuthRejectsMissingForgedAndExpiredTokens(t *testing.T) {
	s, srv := testServer(t)
	if got := get(t, srv.URL+"/api/me", ""); got != 401 {
		t.Fatalf("no token: %d", got)
	}
	forged := &Server{Secret: []byte("other-secret")}
	tok, _ := forged.Issue("guest", "oncall", "gold", time.Hour)
	if got := get(t, srv.URL+"/api/me", tok); got != 401 {
		t.Fatalf("forged: %d", got)
	}
	old, _ := s.Issue("guest", "guest", "basic", -time.Minute)
	if got := get(t, srv.URL+"/api/me", old); got != 401 {
		t.Fatalf("expired: %d", got)
	}
}

func TestRoleIsEnforced(t *testing.T) {
	s, _ := testServer(t)
	h := s.authed("oncall", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	for role, want := range map[string]int{"guest": 403, "oncall": 204} {
		tok, _ := s.Issue("x", role, "basic", time.Hour)
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		h(rec, req)
		if rec.Code != want {
			t.Errorf("role %s: %d want %d", role, rec.Code, want)
		}
	}
}
