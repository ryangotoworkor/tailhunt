// Command loadgen runs one open-model load test against the gateway:
// searches at -search-rps (measured) while holds arrive at -hold-rps (the
// background pressure on the inventory pool). It prints a JSON summary of
// the search latencies.
//
//	go run ./cmd/loadgen -search-rps 30 -hold-rps 25 -duration 20s
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/ryangotoworkor/tailhunt/internal/gateway"
	"github.com/ryangotoworkor/tailhunt/internal/loadgen"
)

func main() {
	base := flag.String("url", "http://localhost:8880", "gateway base URL")
	user := flag.String("user", "oncall", "login (needs role oncall to hold on behalf of guests)")
	pass := flag.String("pass", "oncall-demo", "password of the demo login")
	srps := flag.Float64("search-rps", 30, "searches per second (measured)")
	hrps := flag.Float64("hold-rps", 25, "holds per second (background pressure)")
	dur := flag.Duration("duration", 20*time.Second, "how long to send")
	warm := flag.Duration("warmup", 3*time.Second, "holds start this long before searches; not measured")
	label := flag.String("label", "", "free-form label copied into the output")
	flag.Parse()

	tok, err := login(*base, *user, *pass)
	if err != nil {
		fmt.Fprintln(os.Stderr, "login:", err)
		os.Exit(1)
	}
	client := &http.Client{Timeout: 15 * time.Second}
	send := func(ctx context.Context, method, url string, body []byte) int {
		req, _ := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+tok)
		resp, err := client.Do(req)
		if err != nil {
			return 0
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	ctx := context.Background()
	holdsDone := make(chan []loadgen.Sample)
	go func() {
		holdsDone <- loadgen.Run(ctx, *hrps, *dur+*warm, func(ctx context.Context, i int) int {
			return send(ctx, http.MethodPost, *base+"/api/holds", gateway.HoldBody(i))
		})
	}()
	time.Sleep(*warm)
	searches := loadgen.Run(ctx, *srps, *dur, func(ctx context.Context, i int) int {
		return send(ctx, http.MethodGet, gateway.SearchURL(*base, i), nil)
	})
	holds := loadgen.Summarize(<-holdsDone)
	sum := loadgen.Summarize(searches)
	sum.Extra = map[string]any{"label": *label, "search_rps": *srps, "hold_rps": *hrps, "duration_s": dur.Seconds(), "holds": holds}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(sum)
}

func login(base, user, pass string) (string, error) {
	b, _ := json.Marshal(map[string]string{"username": user, "password": pass})
	resp, err := http.Post(base+"/api/login", "application/json", bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var out struct{ Token string }
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || out.Token == "" {
		return "", fmt.Errorf("status %d", resp.StatusCode)
	}
	return out.Token, nil
}
